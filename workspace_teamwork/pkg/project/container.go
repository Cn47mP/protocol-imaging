package project

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	MaxZipFileCount        = 100_000
	MaxZipEntryBytes       = 512 << 20 // 512MB per file uncompressed
	MaxZipTotalBytes       = 4 << 30   // 4GB total uncompressed volume
	MaxZipCompressionRatio = 250       // Ratio threshold for entries > 1MB
)

var (
	ErrZipBomb         = errors.New("zip bomb detected: decompression limit exceeded")
	ErrInvalidArchive  = errors.New("invalid archive entry")
	ErrSymlinkDetected = errors.New("archive contains symbolic link or non-regular file")
)

// Pack compresses an existing project working directory into a single .pimap ZIP container.
// Internal staging directory (.protocol-imaging) is skipped.
func Pack(projectDir string, archivePath string) error {
	if projectDir == "" || archivePath == "" {
		return errors.New("project directory and archive path are required")
	}
	absProjectDir, err := filepath.Abs(projectDir)
	if err != nil {
		return fmt.Errorf("resolve project directory: %w", err)
	}
	absArchivePath, err := filepath.Abs(archivePath)
	if err != nil {
		return fmt.Errorf("resolve archive path: %w", err)
	}

	tempArchivePath, err := randomTempArchivePath(absArchivePath)
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(tempArchivePath)
	}()

	archiveFile, err := os.OpenFile(tempArchivePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create temporary archive: %w", err)
	}

	zipWriter := zip.NewWriter(archiveFile)

	err = filepath.Walk(absProjectDir, func(currentPath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrSymlinkDetected, currentPath)
		}
		relPath, err := filepath.Rel(absProjectDir, currentPath)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		slashPath := filepath.ToSlash(relPath)
		// Skip internal transaction / staging directories
		if slashPath == ".protocol-imaging" || strings.HasPrefix(slashPath, ".protocol-imaging/") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if err := ValidateArchivePath(slashPath); err != nil {
			return fmt.Errorf("invalid archive entry path %q: %w", slashPath, err)
		}

		if info.IsDir() {
			return nil
		}

		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: %s", ErrSymlinkDetected, currentPath)
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return fmt.Errorf("create zip header for %q: %w", slashPath, err)
		}
		header.Name = slashPath
		header.Method = zip.Deflate

		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("create zip entry %q: %w", slashPath, err)
		}

		srcFile, err := os.Open(currentPath)
		if err != nil {
			return fmt.Errorf("open file %q: %w", currentPath, err)
		}
		defer srcFile.Close()

		if _, err := io.Copy(writer, srcFile); err != nil {
			return fmt.Errorf("write zip entry %q: %w", slashPath, err)
		}
		return nil
	})

	if err != nil {
		_ = zipWriter.Close()
		_ = archiveFile.Close()
		return err
	}

	if err := zipWriter.Close(); err != nil {
		_ = archiveFile.Close()
		return fmt.Errorf("close zip writer: %w", err)
	}
	if err := archiveFile.Sync(); err != nil {
		_ = archiveFile.Close()
		return fmt.Errorf("sync archive file: %w", err)
	}
	if err := archiveFile.Close(); err != nil {
		return fmt.Errorf("close archive file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempArchivePath, absArchivePath); err != nil {
		return fmt.Errorf("atomic rename archive: %w", err)
	}
	return nil
}

// Unpack extracts a .pimap ZIP container into a target directory with path traversal,
// symlink, and zip-bomb security defenses.
func Unpack(archivePath string, targetDir string) error {
	return UnpackWithLimits(archivePath, targetDir, MaxZipEntryBytes, MaxZipTotalBytes, MaxZipCompressionRatio)
}

// UnpackWithLimits allows custom security thresholds for decompression.
func UnpackWithLimits(archivePath string, targetDir string, maxEntryBytes, maxTotalBytes, maxRatio uint64) error {
	if archivePath == "" || targetDir == "" {
		return errors.New("archive path and target directory are required")
	}
	absTargetDir, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("resolve target directory: %w", err)
	}

	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip archive: %w", err)
	}
	defer reader.Close()

	if len(reader.File) > MaxZipFileCount {
		return fmt.Errorf("%w: file count %d exceeds maximum %d", ErrZipBomb, len(reader.File), MaxZipFileCount)
	}

	if err := os.MkdirAll(absTargetDir, 0o700); err != nil {
		return fmt.Errorf("create target directory: %w", err)
	}

	var totalUncompressedBytes uint64

	for _, file := range reader.File {
		if file.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrSymlinkDetected, file.Name)
		}

		slashPath := filepath.ToSlash(file.Name)
		if strings.HasSuffix(slashPath, "/") {
			// Directory entry
			cleanDir := strings.TrimSuffix(slashPath, "/")
			if cleanDir != "" {
				if err := ValidateArchivePath(cleanDir); err != nil {
					return fmt.Errorf("%w: directory path %q: %v", ErrInvalidArchive, cleanDir, err)
				}
				destDirPath := filepath.Join(absTargetDir, filepath.FromSlash(cleanDir))
				if err := os.MkdirAll(destDirPath, 0o700); err != nil {
					return fmt.Errorf("create directory %q: %w", cleanDir, err)
				}
			}
			continue
		}

		if err := ValidateArchivePath(slashPath); err != nil {
			return fmt.Errorf("%w: file path %q: %v", ErrInvalidArchive, slashPath, err)
		}

		if file.UncompressedSize64 > maxEntryBytes {
			return fmt.Errorf("%w: file %q uncompressed size %d exceeds limit %d", ErrZipBomb, slashPath, file.UncompressedSize64, maxEntryBytes)
		}

		totalUncompressedBytes += file.UncompressedSize64
		if totalUncompressedBytes > maxTotalBytes {
			return fmt.Errorf("%w: total uncompressed volume exceeds limit %d", ErrZipBomb, maxTotalBytes)
		}

		// Compression ratio guard for entries > 1MB
		if file.UncompressedSize64 > 1024*1024 && file.CompressedSize64 > 0 {
			ratio := file.UncompressedSize64 / file.CompressedSize64
			if ratio > maxRatio {
				return fmt.Errorf("%w: file %q compression ratio %d exceeds threshold %d", ErrZipBomb, slashPath, ratio, maxRatio)
			}
		}

		destFilePath := filepath.Join(absTargetDir, filepath.FromSlash(slashPath))
		destParent := filepath.Dir(destFilePath)
		if err := os.MkdirAll(destParent, 0o700); err != nil {
			return fmt.Errorf("create parent directory for %q: %w", slashPath, err)
		}

		if err := extractZipFile(file, destFilePath, int64(maxEntryBytes)); err != nil {
			return fmt.Errorf("extract %q: %w", slashPath, err)
		}
	}

	return nil
}

// OpenContainer extracts a .pimap archive to a directory and returns an active Session.
func OpenContainer(archivePath string, tempDir string) (*Store, *Session, error) {
	if tempDir == "" {
		td, err := os.MkdirTemp("", "pimap-*")
		if err != nil {
			return nil, nil, fmt.Errorf("create temp directory: %w", err)
		}
		tempDir = td
	}
	if err := Unpack(archivePath, tempDir); err != nil {
		return nil, nil, err
	}
	store, err := NewStore(tempDir)
	if err != nil {
		return nil, nil, err
	}
	session, err := store.Resume(context.Background())
	if err != nil {
		return nil, nil, err
	}
	return store, session, nil
}

// SaveContainer atomically packs an active Session back to a .pimap archive.
func SaveContainer(session *Session, archivePath string) error {
	if session == nil {
		return errors.New("session is required")
	}
	return Pack(session.Root(), archivePath)
}

func extractZipFile(file *zip.File, destPath string, limit int64) error {
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	destFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer destFile.Close()

	limitedReader := io.LimitReader(rc, limit+1)
	written, err := io.Copy(destFile, limitedReader)
	if err != nil {
		return err
	}
	if written > limit {
		return ErrZipBomb
	}
	return nil
}

func randomTempArchivePath(finalPath string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s.tmp-%s", finalPath, hex.EncodeToString(buf)), nil
}
