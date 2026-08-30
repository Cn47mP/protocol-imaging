package project

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const transactionSchemaVersion = 1

type transactionWrite struct {
	Target         string
	Data           []byte
	Replace        bool
	ExpectedSHA256 string
}

type transactionJournal struct {
	SchemaVersion int                `json:"schema_version"`
	ID            string             `json:"id"`
	Entries       []transactionEntry `json:"entries"`
}

type transactionEntry struct {
	Target  string `json:"target"`
	Payload string `json:"payload"`
	Backup  string `json:"backup"`
	SHA256  string `json:"sha256"`
	Replace bool   `json:"replace"`
}

func (store *Store) transact(ctx context.Context, writes []transactionWrite) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.transactLocked(ctx, writes)
}

func (store *Store) transactLocked(ctx context.Context, writes []transactionWrite) error {
	if len(writes) == 0 {
		return errors.New("transaction requires at least one file")
	}
	if err := store.ensureMetadataDirectories(); err != nil {
		return err
	}
	if err := store.recoverPendingLocked(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	id, err := randomTransactionID()
	if err != nil {
		return err
	}
	transactionDirectory := ".protocol-imaging/transactions/" + id
	if _, err := store.ensureDirectory(transactionDirectory); err != nil {
		return err
	}
	cleanupBeforeJournal := true
	defer func() {
		if cleanupBeforeJournal {
			_ = store.removeTransactionDirectory(transactionDirectory)
		}
	}()

	journal := transactionJournal{SchemaVersion: transactionSchemaVersion, ID: id}
	seenTargets := make(map[string]struct{}, len(writes))
	for index, write := range writes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := validateTransactionTarget(write.Target); err != nil {
			return fmt.Errorf("write %d target: %w", index, err)
		}
		if _, duplicate := seenTargets[write.Target]; duplicate {
			return fmt.Errorf("transaction contains duplicate target %q", write.Target)
		}
		seenTargets[write.Target] = struct{}{}
		if err := store.preflightTarget(write.Target, write.Replace, write.ExpectedSHA256); err != nil {
			return err
		}
		if _, err := store.ensureDirectory(filepath.ToSlash(filepath.Dir(write.Target))); err != nil && filepath.Dir(write.Target) != "." {
			return err
		}

		payload := fmt.Sprintf("%s/payload-%03d", transactionDirectory, index)
		backup := fmt.Sprintf("%s/backup-%03d", transactionDirectory, index)
		payloadPath, err := store.resolvePath(payload)
		if err != nil {
			return err
		}
		if err := writeFileExclusive(payloadPath, write.Data); err != nil {
			return fmt.Errorf("stage %q: %w", write.Target, err)
		}
		digest := sha256.Sum256(write.Data)
		journal.Entries = append(journal.Entries, transactionEntry{
			Target:  write.Target,
			Payload: payload,
			Backup:  backup,
			SHA256:  hex.EncodeToString(digest[:]),
			Replace: write.Replace,
		})
	}

	journalData, err := marshalIndented(journal)
	if err != nil {
		return err
	}
	pendingPath, err := store.resolvePath(".protocol-imaging/pending.json")
	if err != nil {
		return err
	}
	pendingTemporary := pendingPath + ".tmp-" + id
	if err := writeFileExclusive(pendingTemporary, journalData); err != nil {
		return fmt.Errorf("stage transaction journal: %w", err)
	}
	if err := os.Rename(pendingTemporary, pendingPath); err != nil {
		_ = os.Remove(pendingTemporary)
		return fmt.Errorf("publish transaction journal: %w", err)
	}
	cleanupBeforeJournal = false

	for index, entry := range journal.Entries {
		if err := store.installEntry(entry); err != nil {
			return fmt.Errorf("install transaction entry %d: %w", index, err)
		}
		if store.installHook != nil {
			if err := store.installHook(index + 1); err != nil {
				return err
			}
		}
	}
	if err := store.finishTransaction(journal); err != nil {
		return err
	}
	return nil
}

func (store *Store) recoverPendingLocked() error {
	pendingPath, err := store.resolvePath(".protocol-imaging/pending.json")
	if err != nil {
		return err
	}
	info, err := os.Lstat(pendingPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maxMetadataBytes {
		return fmt.Errorf("%w: invalid pending transaction journal", ErrCorruptSession)
	}
	data, err := os.ReadFile(pendingPath)
	if err != nil {
		return err
	}
	var journal transactionJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		return fmt.Errorf("%w: decode pending transaction: %v", ErrCorruptSession, err)
	}
	if err := validateTransactionJournal(journal); err != nil {
		return fmt.Errorf("%w: %v", ErrCorruptSession, err)
	}
	for index, entry := range journal.Entries {
		if err := store.installEntry(entry); err != nil {
			return fmt.Errorf("%w: recover entry %d: %v", ErrCorruptSession, index, err)
		}
	}
	return store.finishTransaction(journal)
}

func (store *Store) installEntry(entry transactionEntry) error {
	parent := filepath.ToSlash(filepath.Dir(entry.Target))
	if parent != "." {
		if _, err := store.ensureDirectory(parent); err != nil {
			return err
		}
	}
	target, err := store.resolvePath(entry.Target)
	if err != nil {
		return err
	}
	payload, err := store.resolvePath(entry.Payload)
	if err != nil {
		return err
	}
	backup, err := store.resolvePath(entry.Backup)
	if err != nil {
		return err
	}

	targetMatches, targetExists, err := fileMatchesDigest(target, entry.SHA256)
	if err != nil {
		return err
	}
	if targetMatches {
		return nil
	}
	payloadMatches, payloadExists, err := fileMatchesDigest(payload, entry.SHA256)
	if err != nil {
		return err
	}
	if !payloadExists || !payloadMatches {
		return errors.New("staged payload is missing or has the wrong digest")
	}
	backupExists, err := regularFileExists(backup)
	if err != nil {
		return err
	}
	if targetExists {
		if !entry.Replace {
			return ErrImmutableConflict
		}
		if backupExists {
			return errors.New("target and backup both exist with an unexpected target digest")
		}
		if err := os.Rename(target, backup); err != nil {
			return fmt.Errorf("preserve previous target: %w", err)
		}
	}
	if err := os.Rename(payload, target); err != nil {
		return fmt.Errorf("install staged payload: %w", err)
	}
	matched, exists, err := fileMatchesDigest(target, entry.SHA256)
	if err != nil {
		return err
	}
	if !exists || !matched {
		return errors.New("installed payload failed digest verification")
	}
	return syncParent(target)
}

func (store *Store) finishTransaction(journal transactionJournal) error {
	pendingPath, err := store.resolvePath(".protocol-imaging/pending.json")
	if err != nil {
		return err
	}
	if err := os.Remove(pendingPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove completed transaction journal: %w", err)
	}
	transactionDirectory := ".protocol-imaging/transactions/" + journal.ID
	if err := store.removeTransactionDirectory(transactionDirectory); err != nil {
		return fmt.Errorf("remove completed transaction staging: %w", err)
	}
	return nil
}

func (store *Store) ensureMetadataDirectories() error {
	info, err := os.Lstat(store.root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("project root must be a real directory")
	}
	if _, err := store.ensureDirectory(".protocol-imaging"); err != nil {
		return err
	}
	_, err = store.ensureDirectory(".protocol-imaging/transactions")
	return err
}

func (store *Store) ensureDirectory(relative string) (string, error) {
	if relative == "." || relative == "" {
		return store.root, nil
	}
	if err := ValidateArchivePath(relative); err != nil {
		return "", err
	}
	current := store.root
	for _, segment := range strings.Split(relative, "/") {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o700); err != nil {
				return "", err
			}
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("path component %q is not a real directory", current)
		}
	}
	return current, nil
}

func (store *Store) resolvePath(relative string) (string, error) {
	if err := ValidateArchivePath(relative); err != nil {
		return "", err
	}
	resolved := filepath.Join(store.root, filepath.FromSlash(relative))
	relativeCheck, err := filepath.Rel(store.root, resolved)
	if err != nil || relativeCheck == ".." || strings.HasPrefix(relativeCheck, ".."+string(filepath.Separator)) {
		return "", errors.New("resolved path escapes project root")
	}
	return resolved, nil
}

func (store *Store) resolveExistingFile(relative string) (string, error) {
	resolved, err := store.resolvePath(relative)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(resolved)
	parentRelative, err := filepath.Rel(store.root, parent)
	if err != nil {
		return "", err
	}
	if parentRelative != "." {
		current := store.root
		for _, segment := range strings.Split(filepath.ToSlash(parentRelative), "/") {
			current = filepath.Join(current, segment)
			info, err := os.Lstat(current)
			if err != nil {
				return "", err
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return "", errors.New("project path traverses a symbolic link or non-directory")
			}
		}
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("project metadata path is not a regular file")
	}
	return resolved, nil
}

func (store *Store) preflightTarget(relative string, replace bool, expectedSHA256 string) error {
	resolved, err := store.resolvePath(relative)
	if err != nil {
		return err
	}
	info, err := os.Lstat(resolved)
	if errors.Is(err, os.ErrNotExist) {
		if expectedSHA256 != "" {
			return fmt.Errorf("%w: expected target %q is missing", ErrSessionConflict, relative)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("target %q is not a regular file", relative)
	}
	if expectedSHA256 != "" {
		actual, exists, err := digestExistingFile(resolved)
		if err != nil {
			return err
		}
		if !exists || actual != expectedSHA256 {
			return fmt.Errorf("%w: target %q no longer matches the opened session", ErrSessionConflict, relative)
		}
	}
	if !replace {
		return fmt.Errorf("%w: %s", ErrImmutableConflict, relative)
	}
	return nil
}

func (store *Store) removeTransactionDirectory(relative string) error {
	expectedPrefix := ".protocol-imaging/transactions/"
	if !strings.HasPrefix(relative, expectedPrefix) || strings.Contains(strings.TrimPrefix(relative, expectedPrefix), "/") {
		return errors.New("refusing to remove an invalid transaction directory")
	}
	resolved, err := store.resolvePath(relative)
	if err != nil {
		return err
	}
	return os.RemoveAll(resolved)
}

func validateTransactionTarget(target string) error {
	if err := ValidateArchivePath(target); err != nil {
		return err
	}
	if target == ".protocol-imaging" || strings.HasPrefix(target, ".protocol-imaging/") {
		return errors.New("transaction target uses reserved session metadata path")
	}
	return nil
}

func validateTransactionJournal(journal transactionJournal) error {
	if journal.SchemaVersion != transactionSchemaVersion || journal.ID == "" || len(journal.Entries) == 0 {
		return errors.New("invalid transaction journal header")
	}
	if len(journal.ID) != 32 {
		return errors.New("invalid transaction id")
	}
	decodedID, err := hex.DecodeString(journal.ID)
	if err != nil || hex.EncodeToString(decodedID) != journal.ID {
		return errors.New("invalid transaction id")
	}
	base := ".protocol-imaging/transactions/" + journal.ID + "/"
	seen := make(map[string]struct{}, len(journal.Entries))
	for index, entry := range journal.Entries {
		if err := validateTransactionTarget(entry.Target); err != nil {
			return fmt.Errorf("entry %d target: %w", index, err)
		}
		if _, duplicate := seen[entry.Target]; duplicate {
			return fmt.Errorf("entry %d duplicates target %q", index, entry.Target)
		}
		seen[entry.Target] = struct{}{}
		if entry.Payload != fmt.Sprintf("%spayload-%03d", base, index) || entry.Backup != fmt.Sprintf("%sbackup-%03d", base, index) {
			return fmt.Errorf("entry %d has unexpected staging paths", index)
		}
		if len(entry.SHA256) != sha256.Size*2 {
			return fmt.Errorf("entry %d has invalid digest", index)
		}
		if _, err := hex.DecodeString(entry.SHA256); err != nil {
			return fmt.Errorf("entry %d has invalid digest: %w", index, err)
		}
	}
	return nil
}

func randomTransactionID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate transaction id: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

func writeFileExclusive(filename string, data []byte) error {
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(filename)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	remove = false
	return nil
}

func fileMatchesDigest(filename, expected string) (matched bool, exists bool, err error) {
	digest, exists, err := digestExistingFile(filename)
	if err != nil || !exists {
		return false, exists, err
	}
	return digest == expected, true, nil
}

func digestExistingFile(filename string) (digest string, exists bool, err error) {
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", true, errors.New("transaction path is not a regular file")
	}
	file, err := os.Open(filename)
	if err != nil {
		return "", true, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", true, err
	}
	return hex.EncodeToString(hash.Sum(nil)), true, nil
}

func regularFileExists(filename string) (bool, error) {
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return true, errors.New("transaction backup is not a regular file")
	}
	return true, nil
}

func syncParent(filename string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(filepath.Dir(filename))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
