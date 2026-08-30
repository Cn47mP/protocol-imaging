package project

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrProjectExists     = errors.New("project working directory already exists")
	ErrProjectNotFound   = errors.New("project working directory does not exist")
	ErrSessionConflict   = errors.New("session state changed since it was opened")
	ErrImmutableConflict = errors.New("immutable project file already exists")
	ErrCorruptSession    = errors.New("project session is corrupt")
)

const maxMetadataBytes = 16 << 20

type Store struct {
	root string
	mu   sync.Mutex

	// installHook is only set by package tests to emulate process loss after a
	// payload rename. Production stores leave it nil.
	installHook func(installed int) error
}

type Session struct {
	store          *Store
	mu             sync.RWMutex
	manifest       Manifest
	capture        CaptureDocument
	boundary       BoundaryDocument
	activePlan     *PlanDocument
	manifestDigest string
}

type TileCommit struct {
	ID   string
	Path string
	PNG  []byte
}

type CheckpointCommit struct {
	Plan              PlanDocument
	Boundary          BoundaryDocument
	Tiles             []TileCommit
	CaptureState      CaptureState
	ActiveCalibration string
	ActiveVersion     string
	UpdatedAt         time.Time
}

func NewStore(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("project root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve project root: %w", err)
	}
	absolute = filepath.Clean(absolute)
	if filepath.Dir(absolute) == absolute {
		return nil, errors.New("filesystem root cannot be used as a project directory")
	}
	return &Store{root: absolute}, nil
}

func (store *Store) Root() string {
	return store.root
}

func (store *Store) Create(ctx context.Context, manifest Manifest, captureDocument CaptureDocument, boundary BoundaryDocument) (*Session, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if manifest.CaptureState != CaptureCreated || manifest.ActivePlan != "" || manifest.ActiveCalibration != "" {
		return nil, errors.New("new project must start in created state without an active plan or calibration")
	}
	if manifest.Geometry.Status != GeometryDiscovering || boundary.Status != GeometryDiscovering {
		return nil, errors.New("new project must start with discovering geometry")
	}
	if boundary.Revision != 0 || len(boundary.Events) != 0 || len(boundary.Rows) != 0 {
		return nil, errors.New("new project boundary must not contain exploration evidence")
	}
	if err := ValidateBundle(manifest, captureDocument); err != nil {
		return nil, err
	}
	if err := boundary.Validate(); err != nil {
		return nil, fmt.Errorf("boundary: %w", err)
	}
	if err := validateManifestBoundary(manifest, boundary); err != nil {
		return nil, err
	}

	if info, err := os.Lstat(store.root); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("project root cannot be a symbolic link")
		}
		return nil, ErrProjectExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect project root: %w", err)
	}
	if err := os.Mkdir(store.root, 0o700); err != nil {
		return nil, fmt.Errorf("create project root: %w", err)
	}
	if err := store.ensureMetadataDirectories(); err != nil {
		return nil, err
	}

	manifestData, err := marshalIndented(manifest)
	if err != nil {
		return nil, fmt.Errorf("encode manifest: %w", err)
	}
	captureData, err := marshalIndented(captureDocument)
	if err != nil {
		return nil, fmt.Errorf("encode capture document: %w", err)
	}
	boundaryData, err := marshalIndented(boundary)
	if err != nil {
		return nil, fmt.Errorf("encode boundary: %w", err)
	}
	writes := []transactionWrite{
		{Target: manifest.Capture, Data: captureData},
		{Target: manifest.Geometry.Observation, Data: boundaryData},
		{Target: "manifest.json", Data: manifestData},
	}
	if err := store.transactLocked(ctx, writes); err != nil {
		return nil, fmt.Errorf("create project transaction: %w", err)
	}
	return newSession(store, manifest, captureDocument, boundary, nil, digestBytes(manifestData)), nil
}

func (store *Store) Resume(ctx context.Context) (*Session, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Lstat(store.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("inspect project root: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("project root must be a real directory")
	}
	if err := store.ensureMetadataDirectories(); err != nil {
		return nil, err
	}
	if err := store.recoverPendingLocked(); err != nil {
		return nil, fmt.Errorf("recover pending transaction: %w", err)
	}

	var manifest Manifest
	if err := store.readJSON("manifest.json", &manifest); err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var captureDocument CaptureDocument
	if err := store.readJSON(manifest.Capture, &captureDocument); err != nil {
		return nil, fmt.Errorf("read capture document: %w", err)
	}
	var boundary BoundaryDocument
	if err := store.readJSON(manifest.Geometry.Observation, &boundary); err != nil {
		return nil, fmt.Errorf("read boundary: %w", err)
	}
	if err := ValidateBundle(manifest, captureDocument); err != nil {
		return nil, err
	}
	if err := boundary.Validate(); err != nil {
		return nil, fmt.Errorf("boundary: %w", err)
	}
	if err := validateManifestBoundary(manifest, boundary); err != nil {
		return nil, err
	}

	var activePlan *PlanDocument
	if manifest.ActivePlan != "" {
		var planDocument PlanDocument
		if err := store.readJSON(manifest.ActivePlan, &planDocument); err != nil {
			return nil, fmt.Errorf("read active plan: %w", err)
		}
		if PlanArchivePath(planDocument.ID) != manifest.ActivePlan {
			return nil, fmt.Errorf("%w: active plan path does not match plan id", ErrCorruptSession)
		}
		if err := ValidateCheckpoint(planDocument, boundary); err != nil {
			return nil, fmt.Errorf("active checkpoint: %w", err)
		}
		activePlan = &planDocument
	} else if boundary.Revision != 0 {
		return nil, fmt.Errorf("%w: boundary evidence exists without an active plan", ErrCorruptSession)
	}
	manifestDigest, err := store.digestFile("manifest.json")
	if err != nil {
		return nil, fmt.Errorf("hash manifest: %w", err)
	}
	return newSession(store, manifest, captureDocument, boundary, activePlan, manifestDigest), nil
}

func (session *Session) Root() string {
	return session.store.root
}

func (session *Session) Manifest() Manifest {
	session.mu.RLock()
	defer session.mu.RUnlock()
	return cloneDocument(session.manifest)
}

func (session *Session) CaptureDocument() CaptureDocument {
	session.mu.RLock()
	defer session.mu.RUnlock()
	return cloneDocument(session.capture)
}

func (session *Session) Boundary() BoundaryDocument {
	session.mu.RLock()
	defer session.mu.RUnlock()
	return cloneDocument(session.boundary)
}

func (session *Session) ActivePlan() *PlanDocument {
	session.mu.RLock()
	defer session.mu.RUnlock()
	if session.activePlan == nil {
		return nil
	}
	clone := cloneDocument(*session.activePlan)
	return &clone
}

func (session *Session) UpdateCaptureState(ctx context.Context, state CaptureState, activeCalibration string, updatedAt time.Time) error {
	session.mu.Lock()
	defer session.mu.Unlock()

	if !validCaptureTransition(session.manifest.CaptureState, state) {
		return fmt.Errorf("invalid capture state transition %q -> %q", session.manifest.CaptureState, state)
	}
	next := cloneDocument(session.manifest)
	next.CaptureState = state
	next.ActiveCalibration = activeCalibration
	next.UpdatedAt = updatedAt
	if err := ValidateBundle(next, session.capture); err != nil {
		return err
	}
	data, err := marshalIndented(next)
	if err != nil {
		return err
	}
	if err := session.store.transact(ctx, []transactionWrite{{
		Target:         "manifest.json",
		Data:           data,
		Replace:        true,
		ExpectedSHA256: session.manifestDigest,
	}}); err != nil {
		return err
	}
	session.manifest = next
	session.manifestDigest = digestBytes(data)
	return nil
}

func (session *Session) CommitCheckpoint(ctx context.Context, commit CheckpointCommit) error {
	session.mu.Lock()
	defer session.mu.Unlock()

	if !validCaptureTransition(session.manifest.CaptureState, commit.CaptureState) {
		return fmt.Errorf("invalid capture state transition %q -> %q", session.manifest.CaptureState, commit.CaptureState)
	}
	if err := ValidateCheckpoint(commit.Plan, commit.Boundary); err != nil {
		return err
	}
	if commit.Plan.Supersedes != session.manifest.ActivePlan {
		return fmt.Errorf("%w: plan supersedes %q, active plan is %q", ErrSessionConflict, commit.Plan.Supersedes, session.manifest.ActivePlan)
	}
	if commit.UpdatedAt.Before(session.manifest.UpdatedAt) {
		return errors.New("checkpoint updated_at cannot precede current manifest")
	}
	if commit.Plan.CreatedAt.After(commit.UpdatedAt) {
		return errors.New("plan created_at cannot be after checkpoint updated_at")
	}
	planPath := PlanArchivePath(commit.Plan.ID)
	if err := session.validateTiles(commit.Tiles, commit.Plan); err != nil {
		return err
	}

	next := cloneDocument(session.manifest)
	next.UpdatedAt = commit.UpdatedAt
	next.Geometry.Status = commit.Boundary.Status
	next.Geometry.CoordinateCompatibility = commit.Boundary.CoordinateCompatibility
	next.CaptureState = commit.CaptureState
	next.ActiveCalibration = commit.ActiveCalibration
	next.ActivePlan = planPath
	if commit.ActiveVersion != "" {
		next.ActiveVersion = commit.ActiveVersion
	}
	if err := ValidateBundle(next, session.capture); err != nil {
		return err
	}

	planData, err := marshalIndented(commit.Plan)
	if err != nil {
		return fmt.Errorf("encode plan: %w", err)
	}
	boundaryData, err := marshalIndented(commit.Boundary)
	if err != nil {
		return fmt.Errorf("encode boundary: %w", err)
	}
	manifestData, err := marshalIndented(next)
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	writes := make([]transactionWrite, 0, len(commit.Tiles)+3)
	for _, tile := range commit.Tiles {
		writes = append(writes, transactionWrite{Target: tile.Path, Data: tile.PNG})
	}
	writes = append(writes,
		transactionWrite{Target: planPath, Data: planData},
		transactionWrite{Target: session.manifest.Geometry.Observation, Data: boundaryData, Replace: true},
		transactionWrite{
			Target:         "manifest.json",
			Data:           manifestData,
			Replace:        true,
			ExpectedSHA256: session.manifestDigest,
		},
	)
	if err := session.store.transact(ctx, writes); err != nil {
		return err
	}
	session.manifest = next
	session.manifestDigest = digestBytes(manifestData)
	session.boundary = cloneDocument(commit.Boundary)
	planClone := cloneDocument(commit.Plan)
	session.activePlan = &planClone
	return nil
}

func (session *Session) validateTiles(tiles []TileCommit, planDocument PlanDocument) error {
	frontierTiles := make(map[string]struct{}, len(planDocument.Frontier.Tiles))
	for _, tile := range planDocument.Frontier.Tiles {
		frontierTiles[tile.ID] = struct{}{}
	}
	layers := make(map[string]struct{}, len(session.manifest.Layers))
	for _, layer := range session.manifest.Layers {
		layers[layer.ID] = struct{}{}
	}
	seenPaths := make(map[string]struct{}, len(tiles))
	seenIDs := make(map[string]struct{}, len(tiles))
	totalBytes := int64(0)
	for index, tile := range tiles {
		if err := validateStableID("tile.id", tile.ID); err != nil {
			return fmt.Errorf("tiles[%d]: %w", index, err)
		}
		if _, exists := frontierTiles[tile.ID]; !exists {
			return fmt.Errorf("tiles[%d] id %q is not present in the plan frontier", index, tile.ID)
		}
		if _, duplicate := seenIDs[tile.ID]; duplicate {
			return fmt.Errorf("duplicate tile id %q in checkpoint", tile.ID)
		}
		seenIDs[tile.ID] = struct{}{}
		if err := ValidateArchivePath(tile.Path); err != nil {
			return fmt.Errorf("tiles[%d].path: %w", index, err)
		}
		segments := strings.Split(tile.Path, "/")
		if len(segments) < 4 || segments[0] != "layers" || segments[2] != "tiles" || path.Ext(tile.Path) != ".png" {
			return fmt.Errorf("tiles[%d].path must be layers/<layer>/tiles/<name>.png", index)
		}
		if _, exists := layers[segments[1]]; !exists {
			return fmt.Errorf("tiles[%d] references unknown layer %q", index, segments[1])
		}
		if _, duplicate := seenPaths[tile.Path]; duplicate {
			return fmt.Errorf("duplicate tile path %q in checkpoint", tile.Path)
		}
		seenPaths[tile.Path] = struct{}{}
		config, err := png.DecodeConfig(bytes.NewReader(tile.PNG))
		if err != nil {
			return fmt.Errorf("tiles[%d] is not a valid PNG: %w", index, err)
		}
		if config.Width != session.capture.Environment.RawFrameSize.Width || config.Height != session.capture.Environment.RawFrameSize.Height {
			return fmt.Errorf("tiles[%d] dimensions %dx%d do not match raw frame %dx%d", index, config.Width, config.Height,
				session.capture.Environment.RawFrameSize.Width, session.capture.Environment.RawFrameSize.Height)
		}
		totalBytes += int64(len(tile.PNG))
		if totalBytes > session.capture.Limits.MaxDiskBytes {
			return errors.New("checkpoint tile bytes exceed session disk safety limit")
		}
	}
	return nil
}

func PlanArchivePath(planID string) string {
	return "plans/" + planID + ".json"
}

func newSession(store *Store, manifest Manifest, captureDocument CaptureDocument, boundary BoundaryDocument, activePlan *PlanDocument, manifestDigest string) *Session {
	session := &Session{
		store:          store,
		manifest:       cloneDocument(manifest),
		capture:        cloneDocument(captureDocument),
		boundary:       cloneDocument(boundary),
		manifestDigest: manifestDigest,
	}
	if activePlan != nil {
		clone := cloneDocument(*activePlan)
		session.activePlan = &clone
	}
	return session
}

func validateManifestBoundary(manifest Manifest, boundary BoundaryDocument) error {
	if manifest.Geometry.Status != boundary.Status {
		return fmt.Errorf("%w: manifest geometry status %q differs from boundary %q", ErrCorruptSession, manifest.Geometry.Status, boundary.Status)
	}
	if manifest.Geometry.CoordinateCompatibility != boundary.CoordinateCompatibility {
		return fmt.Errorf("%w: coordinate compatibility differs between manifest and boundary", ErrCorruptSession)
	}
	return nil
}

func validCaptureTransition(from, to CaptureState) bool {
	if from == to {
		return true
	}
	if to == CaptureCancelled || to == CaptureFailedRecoverable || to == CaptureFailedCorrupt {
		return from != CaptureComplete && from != CaptureFailedCorrupt
	}
	switch from {
	case CaptureCreated:
		return to == CaptureHoming
	case CaptureHoming:
		return to == CaptureCalibrating
	case CaptureCalibrating:
		return to == CaptureCapturing
	case CaptureCapturing:
		return to == CaptureRepairing || to == CaptureProcessing
	case CaptureRepairing:
		return to == CaptureProcessing
	case CaptureProcessing:
		return to == CaptureComplete
	case CaptureCancelled, CaptureFailedRecoverable:
		return to == CaptureHoming || to == CaptureCalibrating || to == CaptureCapturing || to == CaptureRepairing || to == CaptureProcessing
	default:
		return false
	}
}

func marshalIndented(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func cloneDocument[T any](value T) T {
	data, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("clone project document: %v", err))
	}
	var clone T
	if err := json.Unmarshal(data, &clone); err != nil {
		panic(fmt.Sprintf("clone project document: %v", err))
	}
	return clone
}

func (store *Store) readJSON(relative string, destination any) error {
	filename, err := store.resolveExistingFile(relative)
	if err != nil {
		return err
	}
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() > maxMetadataBytes {
		return fmt.Errorf("metadata file %q exceeds %d bytes", relative, maxMetadataBytes)
	}
	decoder := json.NewDecoder(&limitedReader{reader: file, remaining: maxMetadataBytes + 1})
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("metadata contains trailing JSON values")
		}
		return fmt.Errorf("read trailing metadata: %w", err)
	}
	return nil
}

type limitedReader struct {
	reader    *os.File
	remaining int64
}

func (reader *limitedReader) Read(buffer []byte) (int, error) {
	if reader.remaining <= 0 {
		return 0, errors.New("metadata read limit exceeded")
	}
	if int64(len(buffer)) > reader.remaining {
		buffer = buffer[:reader.remaining]
	}
	count, err := reader.reader.Read(buffer)
	reader.remaining -= int64(count)
	return count, err
}

func (store *Store) digestFile(relative string) (string, error) {
	filename, err := store.resolveExistingFile(relative)
	if err != nil {
		return "", err
	}
	matchedDigest, exists, err := digestExistingFile(filename)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", os.ErrNotExist
	}
	return matchedDigest, nil
}
