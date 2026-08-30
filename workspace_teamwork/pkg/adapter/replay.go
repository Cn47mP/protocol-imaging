package adapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrReplayExhausted  = errors.New("replay operations exhausted")
	ErrReplayDiverged   = errors.New("replay diverged from recorded operations")
	ErrChecksumMismatch = errors.New("replay frame checksum mismatch")
	ErrInvalidRecording = errors.New("invalid controller recording document")
)

// ReplayOptions configures the ReplayController behavior.
type ReplayOptions struct {
	StrictMode        bool    // Validate operation kinds, gesture coordinates, and scroll parameters
	Tolerance         float64 // Tolerance for floating point coordinates in strict mode
	ValidateChecksums bool    // Verify SHA-256 hash of loaded PNG frames
}

// DefaultReplayOptions returns default options.
func DefaultReplayOptions() ReplayOptions {
	return ReplayOptions{
		StrictMode:        true,
		Tolerance:         0.5,
		ValidateChecksums: true,
	}
}

// ReplayController plays back recorded controller operations deterministically.
type ReplayController struct {
	mu         sync.Mutex
	recording  ControllerRecording
	rootDir    string
	cursor     int
	opts       ReplayOptions
	frameCache map[string]image.Image
}

// NewReplayController creates a ReplayController from a directory or recording document.
func NewReplayController(recordingDir string, opts ReplayOptions) (*ReplayController, error) {
	jsonPath := filepath.Join(recordingDir, "controller-recording.json")
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("read recording json: %w", err)
	}

	var rec ControllerRecording
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("unmarshal recording json: %w", err)
	}

	if rec.Format != RecordingFormatConst {
		return nil, fmt.Errorf("%w: invalid format %q, expected %q", ErrInvalidRecording, rec.Format, RecordingFormatConst)
	}
	if rec.SchemaVersion != RecordingSchemaVersion {
		return nil, fmt.Errorf("%w: unsupported schema version %d", ErrInvalidRecording, rec.SchemaVersion)
	}

	if opts.Tolerance <= 0 {
		opts.Tolerance = 0.5
	}

	return &ReplayController{
		recording:  rec,
		rootDir:    recordingDir,
		cursor:     0,
		opts:       opts,
		frameCache: make(map[string]image.Image),
	}, nil
}

// NewReplayControllerFromRecording creates a ReplayController directly from a ControllerRecording struct.
func NewReplayControllerFromRecording(rec ControllerRecording, rootDir string, opts ReplayOptions) (*ReplayController, error) {
	if rec.Format != RecordingFormatConst {
		return nil, fmt.Errorf("%w: invalid format %q", ErrInvalidRecording, rec.Format)
	}
	if opts.Tolerance <= 0 {
		opts.Tolerance = 0.5
	}

	return &ReplayController{
		recording:  rec,
		rootDir:    rootDir,
		cursor:     0,
		opts:       opts,
		frameCache: make(map[string]image.Image),
	}, nil
}

// CaptureRaw replays the next capture_raw operation.
func (r *ReplayController) CaptureRaw(ctx context.Context) (Frame, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	select {
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	default:
	}

	if r.cursor >= len(r.recording.Operations) {
		return Frame{}, ErrReplayExhausted
	}

	op := r.recording.Operations[r.cursor]
	r.cursor++

	if op.Kind != OpCaptureRaw {
		return Frame{}, fmt.Errorf("%w: expected %s at sequence %d, got %s", ErrReplayDiverged, op.Kind, op.Sequence, OpCaptureRaw)
	}

	if op.Error != "" {
		return Frame{}, errors.New(op.Error)
	}

	if op.Frame == nil {
		return Frame{}, errors.New("recorded capture_raw operation missing frame data")
	}

	frameRef := op.Frame
	absPath := filepath.Join(r.rootDir, frameRef.Path)

	img, ok := r.frameCache[frameRef.Path]
	if !ok {
		fileBytes, err := os.ReadFile(absPath)
		if err != nil {
			return Frame{}, fmt.Errorf("read recorded frame file %s: %w", absPath, err)
		}

		if r.opts.ValidateChecksums {
			hash := sha256.Sum256(fileBytes)
			hashHex := hex.EncodeToString(hash[:])
			if hashHex != frameRef.SHA256 {
				return Frame{}, fmt.Errorf("%w: file %s has hash %s, expected %s", ErrChecksumMismatch, frameRef.Path, hashHex, frameRef.SHA256)
			}
		}

		decoded, err := png.Decode(bytes.NewReader(fileBytes))
		if err != nil {
			return Frame{}, fmt.Errorf("decode recorded frame png %s: %w", absPath, err)
		}
		img = decoded
		r.frameCache[frameRef.Path] = img
	}

	capturedAt, err := time.Parse(time.RFC3339Nano, frameRef.CapturedAt)
	if err != nil {
		capturedAt = time.Now().UTC()
	}

	return Frame{
		ID:         frameRef.ID,
		CapturedAt: capturedAt,
		Image:      img,
		Size: PixelSize{
			Width:  frameRef.Size.Width,
			Height: frameRef.Size.Height,
		},
	}, nil
}

// Scroll replays the next scroll operation.
func (r *ReplayController) Scroll(ctx context.Context, deltaX, deltaY int32) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if r.cursor >= len(r.recording.Operations) {
		return ErrReplayExhausted
	}

	op := r.recording.Operations[r.cursor]
	r.cursor++

	if op.Kind != OpScroll {
		return fmt.Errorf("%w: expected %s at sequence %d, got %s", ErrReplayDiverged, op.Kind, op.Sequence, OpScroll)
	}

	if r.opts.StrictMode && op.Scroll != nil {
		if deltaX != op.Scroll.DeltaX || deltaY != op.Scroll.DeltaY {
			return fmt.Errorf("%w: scroll delta mismatch at sequence %d: got (%d, %d), expected (%d, %d)",
				ErrReplayDiverged, op.Sequence, deltaX, deltaY, op.Scroll.DeltaX, op.Scroll.DeltaY)
		}
	}

	if op.Error != "" {
		return errors.New(op.Error)
	}

	return nil
}

// MiddleDrag replays the next middle_drag operation.
func (r *ReplayController) MiddleDrag(ctx context.Context, gesture DragGesture) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if r.cursor >= len(r.recording.Operations) {
		return ErrReplayExhausted
	}

	op := r.recording.Operations[r.cursor]
	r.cursor++

	if op.Kind != OpMiddleDrag {
		return fmt.Errorf("%w: expected %s at sequence %d, got %s", ErrReplayDiverged, op.Kind, op.Sequence, OpMiddleDrag)
	}

	if r.opts.StrictMode && op.Drag != nil {
		recG := op.Drag.Gesture
		bxDiff := math.Abs(gesture.Begin.X - recG.Begin.X)
		byDiff := math.Abs(gesture.Begin.Y - recG.Begin.Y)
		exDiff := math.Abs(gesture.End.X - recG.End.X)
		eyDiff := math.Abs(gesture.End.Y - recG.End.Y)

		if bxDiff > r.opts.Tolerance || byDiff > r.opts.Tolerance || exDiff > r.opts.Tolerance || eyDiff > r.opts.Tolerance {
			return fmt.Errorf("%w: gesture coordinate mismatch at sequence %d: got begin=(%.2f,%.2f) end=(%.2f,%.2f), expected begin=(%.2f,%.2f) end=(%.2f,%.2f)",
				ErrReplayDiverged, op.Sequence, gesture.Begin.X, gesture.Begin.Y, gesture.End.X, gesture.End.Y,
				recG.Begin.X, recG.Begin.Y, recG.End.X, recG.End.Y)
		}
	}

	if op.Error != "" {
		return errors.New(op.Error)
	}

	return nil
}

// InputViewport replays the next input_viewport operation.
func (r *ReplayController) InputViewport(ctx context.Context) (PixelSize, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	select {
	case <-ctx.Done():
		return PixelSize{}, ctx.Err()
	default:
	}

	if r.cursor >= len(r.recording.Operations) {
		return PixelSize{}, ErrReplayExhausted
	}

	op := r.recording.Operations[r.cursor]
	r.cursor++

	if op.Kind != OpInputViewport {
		return PixelSize{}, fmt.Errorf("%w: expected %s at sequence %d, got %s", ErrReplayDiverged, op.Kind, op.Sequence, OpInputViewport)
	}

	if op.Error != "" {
		return PixelSize{}, errors.New(op.Error)
	}

	if op.Viewport == nil {
		return PixelSize{Width: 1920, Height: 1080}, nil
	}

	return PixelSize{
		Width:  op.Viewport.Width,
		Height: op.Viewport.Height,
	}, nil
}

// Release replays the next release operation.
func (r *ReplayController) Release(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if r.cursor >= len(r.recording.Operations) {
		return ErrReplayExhausted
	}

	op := r.recording.Operations[r.cursor]
	r.cursor++

	if op.Kind != OpRelease {
		return fmt.Errorf("%w: expected %s at sequence %d, got %s", ErrReplayDiverged, op.Kind, op.Sequence, OpRelease)
	}

	if op.Error != "" {
		return errors.New(op.Error)
	}

	return nil
}

// Remaining returns the count of remaining unplayed operations.
func (r *ReplayController) Remaining() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.recording.Operations) - r.cursor
}

// Cursor returns current operation index.
func (r *ReplayController) Cursor() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cursor
}

// TotalOperations returns total operation count.
func (r *ReplayController) TotalOperations() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.recording.Operations)
}

// Reset resets the playback cursor to the beginning.
func (r *ReplayController) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cursor = 0
}
