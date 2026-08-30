package adapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	RecordingFormatConst   = "protocol-imaging-controller-recording"
	RecordingSchemaVersion = 1
)

type OperationKind string

const (
	OpCaptureRaw    OperationKind = "capture_raw"
	OpScroll        OperationKind = "scroll"
	OpMiddleDrag    OperationKind = "middle_drag"
	OpInputViewport OperationKind = "input_viewport"
	OpRelease       OperationKind = "release"
)

// RecordedPixelSize represents a pixel dimension matching #/$defs/pixelSize.
type RecordedPixelSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// RecordedPoint represents a 2D float point matching #/$defs/point.
type RecordedPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// RecordedGesture matches the drag gesture object.
type RecordedGesture struct {
	Begin      RecordedPoint `json:"begin"`
	End        RecordedPoint `json:"end"`
	DurationNS int64         `json:"duration_ns"`
}

// RecordedDrag matches the drag wrapper.
type RecordedDrag struct {
	Gesture RecordedGesture `json:"gesture"`
}

// RecordedScroll matches the scroll wrapper.
type RecordedScroll struct {
	DeltaX int32 `json:"delta_x"`
	DeltaY int32 `json:"delta_y"`
}

// RecordedFrameRef matches the frame reference object in controller-recording.schema.json.
type RecordedFrameRef struct {
	ID         string            `json:"id"`
	CapturedAt string            `json:"captured_at"`
	Size       RecordedPixelSize `json:"size"`
	Path       string            `json:"path"`
	SHA256     string            `json:"sha256"`
}

// RecordedOperation represents an individual operation item strictly conforming to schema.
type RecordedOperation struct {
	Sequence int                `json:"sequence"`
	Kind     OperationKind      `json:"kind"`
	Frame    *RecordedFrameRef  `json:"frame,omitempty"`
	Scroll   *RecordedScroll    `json:"scroll,omitempty"`
	Drag     *RecordedDrag      `json:"drag,omitempty"`
	Viewport *RecordedPixelSize `json:"viewport,omitempty"`
	Error    string             `json:"error,omitempty"`
}

// ControllerRecording represents the root document conforming to Draft 2020-12 schema.
type ControllerRecording struct {
	Format        string              `json:"format"`
	SchemaVersion int                 `json:"schema_version"`
	CreatedAt     string              `json:"created_at"`
	Operations    []RecordedOperation `json:"operations"`
}

// RecordingController intercepts and records all controller operations to disk.
type RecordingController struct {
	mu         sync.Mutex
	underlying Controller
	outputDir  string
	recording  ControllerRecording
	frameSeq   int
	closed     bool
}

// NewRecordingController initializes a recording controller wrapping underlying.
func NewRecordingController(underlying Controller, outputDir string) (*RecordingController, error) {
	if underlying == nil {
		return nil, errors.New("underlying controller is required")
	}
	if outputDir == "" {
		return nil, errors.New("output directory is required")
	}

	framesDir := filepath.Join(outputDir, "frames")
	if err := os.MkdirAll(framesDir, 0755); err != nil {
		return nil, fmt.Errorf("create recording frames directory: %w", err)
	}

	rc := &RecordingController{
		underlying: underlying,
		outputDir:  outputDir,
		recording: ControllerRecording{
			Format:        RecordingFormatConst,
			SchemaVersion: RecordingSchemaVersion,
			CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
			Operations:    make([]RecordedOperation, 0),
		},
	}

	return rc, nil
}

// CaptureRaw records a capture_raw operation and saves the frame PNG with SHA256 digest.
func (r *RecordingController) CaptureRaw(ctx context.Context) (Frame, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	frame, err := r.underlying.CaptureRaw(ctx)
	seq := len(r.recording.Operations)

	if err != nil {
		r.recording.Operations = append(r.recording.Operations, RecordedOperation{
			Sequence: seq,
			Kind:     OpCaptureRaw,
			Error:    err.Error(),
		})
		return Frame{}, err
	}

	// Encode image to PNG in memory
	var buf bytes.Buffer
	if err := png.Encode(&buf, frame.Image); err != nil {
		encodeErr := fmt.Errorf("encode frame to png: %w", err)
		r.recording.Operations = append(r.recording.Operations, RecordedOperation{
			Sequence: seq,
			Kind:     OpCaptureRaw,
			Error:    encodeErr.Error(),
		})
		return frame, encodeErr
	}

	pngBytes := buf.Bytes()
	hash := sha256.Sum256(pngBytes)
	hashHex := hex.EncodeToString(hash[:])

	relPath := fmt.Sprintf("frames/frame-%06d.png", r.frameSeq)
	absPath := filepath.Join(r.outputDir, relPath)

	if err := os.WriteFile(absPath, pngBytes, 0644); err != nil {
		writeErr := fmt.Errorf("write frame png: %w", err)
		r.recording.Operations = append(r.recording.Operations, RecordedOperation{
			Sequence: seq,
			Kind:     OpCaptureRaw,
			Error:    writeErr.Error(),
		})
		return frame, writeErr
	}

	frameID := frame.ID
	if frameID == "" {
		frameID = fmt.Sprintf("frame-%06d", r.frameSeq)
	}

	capturedAtStr := frame.CapturedAt.UTC().Format(time.RFC3339Nano)
	if frame.CapturedAt.IsZero() {
		capturedAtStr = time.Now().UTC().Format(time.RFC3339Nano)
	}

	r.recording.Operations = append(r.recording.Operations, RecordedOperation{
		Sequence: seq,
		Kind:     OpCaptureRaw,
		Frame: &RecordedFrameRef{
			ID:         frameID,
			CapturedAt: capturedAtStr,
			Size: RecordedPixelSize{
				Width:  frame.Size.Width,
				Height: frame.Size.Height,
			},
			Path:   relPath,
			SHA256: hashHex,
		},
	})
	r.frameSeq++

	return frame, nil
}

// Scroll records a scroll operation.
func (r *RecordingController) Scroll(ctx context.Context, deltaX, deltaY int32) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	err := r.underlying.Scroll(ctx, deltaX, deltaY)
	seq := len(r.recording.Operations)

	op := RecordedOperation{
		Sequence: seq,
		Kind:     OpScroll,
		Scroll: &RecordedScroll{
			DeltaX: deltaX,
			DeltaY: deltaY,
		},
	}
	if err != nil {
		op.Error = err.Error()
	}

	r.recording.Operations = append(r.recording.Operations, op)
	return err
}

// MiddleDrag records a middle_drag operation.
func (r *RecordingController) MiddleDrag(ctx context.Context, gesture DragGesture) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	err := r.underlying.MiddleDrag(ctx, gesture)
	seq := len(r.recording.Operations)

	durationNS := gesture.Duration.Nanoseconds()
	if durationNS <= 0 {
		durationNS = 1
	}

	op := RecordedOperation{
		Sequence: seq,
		Kind:     OpMiddleDrag,
		Drag: &RecordedDrag{
			Gesture: RecordedGesture{
				Begin:      RecordedPoint{X: gesture.Begin.X, Y: gesture.Begin.Y},
				End:        RecordedPoint{X: gesture.End.X, Y: gesture.End.Y},
				DurationNS: durationNS,
			},
		},
	}
	if err != nil {
		op.Error = err.Error()
	}

	r.recording.Operations = append(r.recording.Operations, op)
	return err
}

// InputViewport records an input_viewport operation.
func (r *RecordingController) InputViewport(ctx context.Context) (PixelSize, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	vp, err := r.underlying.InputViewport(ctx)
	seq := len(r.recording.Operations)

	op := RecordedOperation{
		Sequence: seq,
		Kind:     OpInputViewport,
		Viewport: &RecordedPixelSize{
			Width:  vp.Width,
			Height: vp.Height,
		},
	}
	if err != nil {
		op.Error = err.Error()
	}

	r.recording.Operations = append(r.recording.Operations, op)
	return vp, err
}

// Release records a release operation.
func (r *RecordingController) Release(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	err := r.underlying.Release(ctx)
	seq := len(r.recording.Operations)

	op := RecordedOperation{
		Sequence: seq,
		Kind:     OpRelease,
	}
	if err != nil {
		op.Error = err.Error()
	}

	r.recording.Operations = append(r.recording.Operations, op)
	return err
}

// Flush writes the controller-recording.json file atomically to disk.
func (r *RecordingController) Flush() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := json.MarshalIndent(r.recording, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal controller recording: %w", err)
	}

	targetPath := filepath.Join(r.outputDir, "controller-recording.json")
	tmpPath := targetPath + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write temp recording json: %w", err)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("atomic rename recording json: %w", err)
	}

	return nil
}

// Close flushes and finalizes the recording session.
func (r *RecordingController) Close() error {
	return r.Flush()
}

// Snapshot returns a copy of the in-memory recording document.
func (r *RecordingController) Snapshot() ControllerRecording {
	r.mu.Lock()
	defer r.mu.Unlock()

	copied := r.recording
	copied.Operations = make([]RecordedOperation, len(r.recording.Operations))
	copy(copied.Operations, r.recording.Operations)
	return copied
}
