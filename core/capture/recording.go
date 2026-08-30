package capture

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
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	ControllerRecordingFormat        = "protocol-imaging-controller-recording"
	ControllerRecordingSchemaVersion = 1
	maxRecordingManifestBytes        = 16 << 20
	maxRecordedFrameBytes            = 512 << 20
	maxRecordingOperations           = 1_000_000
)

var (
	ErrInvalidRecording = errors.New("invalid controller recording")
	ErrReplayDiverged   = errors.New("controller replay diverged from recording")
	ErrReplayExhausted  = errors.New("controller replay is exhausted")
)

type OperationKind string

const (
	OperationCaptureRaw    OperationKind = "capture_raw"
	OperationScroll        OperationKind = "scroll"
	OperationMiddleDrag    OperationKind = "middle_drag"
	OperationInputViewport OperationKind = "input_viewport"
	OperationRelease       OperationKind = "release"
)

func (kind OperationKind) Valid() bool {
	switch kind {
	case OperationCaptureRaw, OperationScroll, OperationMiddleDrag, OperationInputViewport, OperationRelease:
		return true
	default:
		return false
	}
}

type ControllerRecording struct {
	Format        string                        `json:"format"`
	SchemaVersion int                           `json:"schema_version"`
	CreatedAt     time.Time                     `json:"created_at"`
	Operations    []RecordedControllerOperation `json:"operations"`
	Frames        map[string][]byte             `json:"-"`
}

type RecordedControllerOperation struct {
	Sequence int             `json:"sequence"`
	Kind     OperationKind   `json:"kind"`
	Frame    *RecordedFrame  `json:"frame,omitempty"`
	Scroll   *RecordedScroll `json:"scroll,omitempty"`
	Drag     *RecordedDrag   `json:"drag,omitempty"`
	Viewport *PixelSize      `json:"viewport,omitempty"`
	Error    string          `json:"error,omitempty"`
}

type RecordedFrame struct {
	ID         string    `json:"id"`
	CapturedAt time.Time `json:"captured_at"`
	Size       PixelSize `json:"size"`
	Path       string    `json:"path"`
	SHA256     string    `json:"sha256"`
}

type RecordedScroll struct {
	DeltaX int32 `json:"delta_x"`
	DeltaY int32 `json:"delta_y"`
}

type RecordedDrag struct {
	Gesture DragGesture `json:"gesture"`
}

type RecordingController struct {
	source    Controller
	mu        sync.Mutex
	recording ControllerRecording
}

func NewRecordingController(source Controller, createdAt time.Time) (*RecordingController, error) {
	if source == nil {
		return nil, errors.New("recorded controller source is required")
	}
	if createdAt.IsZero() {
		return nil, errors.New("recording created_at is required")
	}
	return &RecordingController{
		source: source,
		recording: ControllerRecording{
			Format:        ControllerRecordingFormat,
			SchemaVersion: ControllerRecordingSchemaVersion,
			CreatedAt:     createdAt.UTC(),
			Frames:        make(map[string][]byte),
		},
	}, nil
}

func (controller *RecordingController) CaptureRaw(ctx context.Context) (Frame, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Frame{}, err
	}

	operation := RecordedControllerOperation{Sequence: len(controller.recording.Operations), Kind: OperationCaptureRaw}
	frame, err := controller.source.CaptureRaw(ctx)
	if err != nil {
		operation.Error = err.Error()
		controller.append(operation)
		return Frame{}, err
	}
	if err := validateLiveFrame(frame); err != nil {
		operation.Error = err.Error()
		controller.append(operation)
		return Frame{}, err
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, frame.Image); err != nil {
		operation.Error = fmt.Sprintf("encode frame PNG: %v", err)
		controller.append(operation)
		return Frame{}, errors.New(operation.Error)
	}
	framePath := fmt.Sprintf("frames/frame-%06d.png", operation.Sequence)
	data := encoded.Bytes()
	digest := sha256.Sum256(data)
	operation.Frame = &RecordedFrame{
		ID:         frame.ID,
		CapturedAt: frame.CapturedAt.UTC(),
		Size:       frame.Size,
		Path:       framePath,
		SHA256:     hex.EncodeToString(digest[:]),
	}
	controller.recording.Frames[framePath] = bytes.Clone(data)
	controller.append(operation)
	return frame, nil
}

func (controller *RecordingController) Scroll(ctx context.Context, deltaX, deltaY int32) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	operation := RecordedControllerOperation{
		Sequence: len(controller.recording.Operations),
		Kind:     OperationScroll,
		Scroll:   &RecordedScroll{DeltaX: deltaX, DeltaY: deltaY},
	}
	err := controller.source.Scroll(ctx, deltaX, deltaY)
	if err != nil {
		operation.Error = err.Error()
	}
	controller.append(operation)
	return err
}

func (controller *RecordingController) MiddleDrag(ctx context.Context, gesture DragGesture) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	operation := RecordedControllerOperation{
		Sequence: len(controller.recording.Operations),
		Kind:     OperationMiddleDrag,
		Drag:     &RecordedDrag{Gesture: gesture},
	}
	err := controller.source.MiddleDrag(ctx, gesture)
	if err != nil {
		operation.Error = err.Error()
	}
	controller.append(operation)
	return err
}

func (controller *RecordingController) InputViewport(ctx context.Context) (PixelSize, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return PixelSize{}, err
	}
	operation := RecordedControllerOperation{Sequence: len(controller.recording.Operations), Kind: OperationInputViewport}
	viewport, err := controller.source.InputViewport(ctx)
	if err != nil {
		operation.Error = err.Error()
		controller.append(operation)
		return PixelSize{}, err
	}
	if err := viewport.Validate(); err != nil {
		operation.Error = err.Error()
		controller.append(operation)
		return PixelSize{}, err
	}
	operation.Viewport = &viewport
	controller.append(operation)
	return viewport, nil
}

func (controller *RecordingController) Release(ctx context.Context) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	operation := RecordedControllerOperation{Sequence: len(controller.recording.Operations), Kind: OperationRelease}
	err := controller.source.Release(ctx)
	if err != nil {
		operation.Error = err.Error()
	}
	controller.append(operation)
	return err
}

func (controller *RecordingController) Snapshot() ControllerRecording {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return cloneControllerRecording(controller.recording)
}

func (controller *RecordingController) append(operation RecordedControllerOperation) {
	controller.recording.Operations = append(controller.recording.Operations, operation)
}

type ReplayController struct {
	mu        sync.Mutex
	recording ControllerRecording
	next      int
}

func NewReplayController(recording ControllerRecording) (*ReplayController, error) {
	if err := recording.Validate(); err != nil {
		return nil, err
	}
	return &ReplayController{recording: cloneControllerRecording(recording)}, nil
}

func (controller *ReplayController) CaptureRaw(ctx context.Context) (Frame, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	operation, err := controller.expect(ctx, OperationCaptureRaw)
	if err != nil {
		return Frame{}, err
	}
	controller.next++
	if operation.Error != "" {
		return Frame{}, ReplayedControllerError{Message: operation.Error}
	}
	data := controller.recording.Frames[operation.Frame.Path]
	imageData, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return Frame{}, fmt.Errorf("%w: decode replay frame: %v", ErrInvalidRecording, err)
	}
	return Frame{
		ID:         operation.Frame.ID,
		CapturedAt: operation.Frame.CapturedAt,
		Image:      imageData,
		Size:       operation.Frame.Size,
	}, nil
}

func (controller *ReplayController) Scroll(ctx context.Context, deltaX, deltaY int32) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	operation, err := controller.expect(ctx, OperationScroll)
	if err != nil {
		return err
	}
	if operation.Scroll.DeltaX != deltaX || operation.Scroll.DeltaY != deltaY {
		return fmt.Errorf("%w: scroll got (%d,%d), want (%d,%d)", ErrReplayDiverged,
			deltaX, deltaY, operation.Scroll.DeltaX, operation.Scroll.DeltaY)
	}
	controller.next++
	return replayedOperationError(operation.Error)
}

func (controller *ReplayController) MiddleDrag(ctx context.Context, gesture DragGesture) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	operation, err := controller.expect(ctx, OperationMiddleDrag)
	if err != nil {
		return err
	}
	if operation.Drag.Gesture != gesture {
		return fmt.Errorf("%w: drag got %+v, want %+v", ErrReplayDiverged, gesture, operation.Drag.Gesture)
	}
	controller.next++
	return replayedOperationError(operation.Error)
}

func (controller *ReplayController) InputViewport(ctx context.Context) (PixelSize, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	operation, err := controller.expect(ctx, OperationInputViewport)
	if err != nil {
		return PixelSize{}, err
	}
	controller.next++
	if operation.Error != "" {
		return PixelSize{}, ReplayedControllerError{Message: operation.Error}
	}
	return *operation.Viewport, nil
}

func (controller *ReplayController) Release(ctx context.Context) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	operation, err := controller.expect(ctx, OperationRelease)
	if err != nil {
		return err
	}
	controller.next++
	return replayedOperationError(operation.Error)
}

func (controller *ReplayController) Remaining() int {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return len(controller.recording.Operations) - controller.next
}

func (controller *ReplayController) expect(ctx context.Context, kind OperationKind) (RecordedControllerOperation, error) {
	if err := ctx.Err(); err != nil {
		return RecordedControllerOperation{}, err
	}
	if controller.next >= len(controller.recording.Operations) {
		return RecordedControllerOperation{}, fmt.Errorf("%w: expected %s", ErrReplayExhausted, kind)
	}
	operation := controller.recording.Operations[controller.next]
	if operation.Kind != kind {
		return RecordedControllerOperation{}, fmt.Errorf("%w: operation %d is %s, caller requested %s",
			ErrReplayDiverged, controller.next, operation.Kind, kind)
	}
	return operation, nil
}

type ReplayedControllerError struct {
	Message string
}

func (err ReplayedControllerError) Error() string {
	return err.Message
}

func replayedOperationError(message string) error {
	if message == "" {
		return nil
	}
	return ReplayedControllerError{Message: message}
}

func (recording ControllerRecording) Validate() error {
	if err := recording.validateMetadata(); err != nil {
		return err
	}
	expectedFrames := make(map[string]RecordedFrame)
	for _, operation := range recording.Operations {
		if operation.Frame != nil {
			expectedFrames[operation.Frame.Path] = *operation.Frame
		}
	}
	if len(recording.Frames) != len(expectedFrames) {
		return fmt.Errorf("%w: frame blob count %d does not match metadata count %d", ErrInvalidRecording, len(recording.Frames), len(expectedFrames))
	}
	for framePath, metadata := range expectedFrames {
		data, exists := recording.Frames[framePath]
		if !exists {
			return fmt.Errorf("%w: missing frame blob %q", ErrInvalidRecording, framePath)
		}
		if len(data) > maxRecordedFrameBytes {
			return fmt.Errorf("%w: frame %q exceeds size limit", ErrInvalidRecording, framePath)
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != metadata.SHA256 {
			return fmt.Errorf("%w: frame %q checksum mismatch", ErrInvalidRecording, framePath)
		}
		config, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("%w: frame %q is not PNG: %v", ErrInvalidRecording, framePath, err)
		}
		if config.Width != metadata.Size.Width || config.Height != metadata.Size.Height {
			return fmt.Errorf("%w: frame %q dimensions do not match metadata", ErrInvalidRecording, framePath)
		}
	}
	return nil
}

func (recording ControllerRecording) validateMetadata() error {
	if recording.Format != ControllerRecordingFormat || recording.SchemaVersion != ControllerRecordingSchemaVersion {
		return fmt.Errorf("%w: unsupported format or schema version", ErrInvalidRecording)
	}
	if recording.CreatedAt.IsZero() {
		return fmt.Errorf("%w: created_at is required", ErrInvalidRecording)
	}
	if len(recording.Operations) > maxRecordingOperations {
		return fmt.Errorf("%w: too many operations", ErrInvalidRecording)
	}
	seenFramePaths := make(map[string]struct{})
	for index, operation := range recording.Operations {
		if operation.Sequence != index || !operation.Kind.Valid() {
			return fmt.Errorf("%w: invalid operation %d sequence or kind", ErrInvalidRecording, index)
		}
		payloadCount := 0
		if operation.Frame != nil {
			payloadCount++
		}
		if operation.Scroll != nil {
			payloadCount++
		}
		if operation.Drag != nil {
			payloadCount++
		}
		if operation.Viewport != nil {
			payloadCount++
		}
		switch operation.Kind {
		case OperationCaptureRaw:
			if operation.Error == "" {
				if operation.Frame == nil || payloadCount != 1 {
					return fmt.Errorf("%w: capture operation %d lacks exactly one frame", ErrInvalidRecording, index)
				}
				if err := operation.Frame.Validate(); err != nil {
					return fmt.Errorf("%w: operation %d: %v", ErrInvalidRecording, index, err)
				}
				if _, duplicate := seenFramePaths[operation.Frame.Path]; duplicate {
					return fmt.Errorf("%w: duplicate frame path %q", ErrInvalidRecording, operation.Frame.Path)
				}
				seenFramePaths[operation.Frame.Path] = struct{}{}
			} else if payloadCount != 0 {
				return fmt.Errorf("%w: failed capture operation %d contains a result", ErrInvalidRecording, index)
			}
		case OperationScroll:
			if operation.Scroll == nil || payloadCount != 1 {
				return fmt.Errorf("%w: scroll operation %d lacks exactly one call payload", ErrInvalidRecording, index)
			}
		case OperationMiddleDrag:
			if operation.Drag == nil || payloadCount != 1 {
				return fmt.Errorf("%w: drag operation %d lacks exactly one call payload", ErrInvalidRecording, index)
			}
			if err := operation.Drag.Gesture.Validate(); err != nil {
				return fmt.Errorf("%w: operation %d drag: %v", ErrInvalidRecording, index, err)
			}
		case OperationInputViewport:
			if operation.Error == "" {
				if operation.Viewport == nil || payloadCount != 1 {
					return fmt.Errorf("%w: viewport operation %d lacks exactly one result", ErrInvalidRecording, index)
				}
				if err := operation.Viewport.Validate(); err != nil {
					return fmt.Errorf("%w: operation %d viewport: %v", ErrInvalidRecording, index, err)
				}
			} else if payloadCount != 0 {
				return fmt.Errorf("%w: failed viewport operation %d contains a result", ErrInvalidRecording, index)
			}
		case OperationRelease:
			if payloadCount != 0 {
				return fmt.Errorf("%w: release operation %d contains a payload", ErrInvalidRecording, index)
			}
		}
	}
	return nil
}

func (frame RecordedFrame) Validate() error {
	if frame.ID == "" {
		return errors.New("frame id is required")
	}
	if frame.CapturedAt.IsZero() {
		return errors.New("frame captured_at is required")
	}
	if err := frame.Size.Validate(); err != nil {
		return err
	}
	if err := validateRecordingPath(frame.Path); err != nil {
		return err
	}
	if !strings.HasPrefix(frame.Path, "frames/") || path.Ext(frame.Path) != ".png" {
		return errors.New("frame path must be a PNG below frames/")
	}
	if len(frame.SHA256) != sha256.Size*2 {
		return errors.New("frame sha256 has the wrong length")
	}
	if _, err := hex.DecodeString(frame.SHA256); err != nil {
		return errors.New("frame sha256 is not hexadecimal")
	}
	return nil
}

func (size PixelSize) Validate() error {
	if size.Width <= 0 || size.Height <= 0 || size.Width > 1_000_000 || size.Height > 1_000_000 {
		return fmt.Errorf("pixel size must be within 1..1000000, got %dx%d", size.Width, size.Height)
	}
	return nil
}

func (gesture DragGesture) Validate() error {
	if err := gesture.Begin.Validate(); err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := gesture.End.Validate(); err != nil {
		return fmt.Errorf("end: %w", err)
	}
	if gesture.Duration <= 0 {
		return errors.New("duration must be positive")
	}
	return nil
}

func validateLiveFrame(frame Frame) error {
	if frame.ID == "" {
		return errors.New("captured frame id is required")
	}
	if frame.CapturedAt.IsZero() {
		return errors.New("captured frame time is required")
	}
	if frame.Image == nil {
		return errors.New("captured frame image is required")
	}
	if err := frame.Size.Validate(); err != nil {
		return err
	}
	bounds := frame.Image.Bounds()
	if bounds.Dx() != frame.Size.Width || bounds.Dy() != frame.Size.Height {
		return fmt.Errorf("captured frame image is %dx%d, metadata says %dx%d", bounds.Dx(), bounds.Dy(), frame.Size.Width, frame.Size.Height)
	}
	return nil
}

func (recording ControllerRecording) SaveDirectory(root string) error {
	if err := recording.Validate(); err != nil {
		return err
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(absolute); err == nil {
		return errors.New("recording directory already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Mkdir(absolute, 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(filepath.Join(absolute, "frames"), 0o700); err != nil {
		return err
	}
	paths := make([]string, 0, len(recording.Frames))
	for framePath := range recording.Frames {
		paths = append(paths, framePath)
	}
	sort.Strings(paths)
	for _, framePath := range paths {
		filename, err := resolveRecordingPath(absolute, framePath)
		if err != nil {
			return err
		}
		if err := writeRecordingFile(filename, recording.Frames[framePath]); err != nil {
			return err
		}
	}
	manifest, err := json.MarshalIndent(recording, "", "  ")
	if err != nil {
		return err
	}
	manifest = append(manifest, '\n')
	return writeRecordingFile(filepath.Join(absolute, "recording.json"), manifest)
}

func LoadControllerRecording(root string) (ControllerRecording, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return ControllerRecording{}, err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return ControllerRecording{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ControllerRecording{}, errors.New("recording root must be a real directory")
	}
	manifestPath := filepath.Join(absolute, "recording.json")
	manifestInfo, err := os.Lstat(manifestPath)
	if err != nil {
		return ControllerRecording{}, err
	}
	if !manifestInfo.Mode().IsRegular() || manifestInfo.Mode()&os.ModeSymlink != 0 || manifestInfo.Size() > maxRecordingManifestBytes {
		return ControllerRecording{}, fmt.Errorf("%w: invalid recording manifest", ErrInvalidRecording)
	}
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return ControllerRecording{}, err
	}
	var recording ControllerRecording
	if err := json.Unmarshal(manifest, &recording); err != nil {
		return ControllerRecording{}, fmt.Errorf("%w: %v", ErrInvalidRecording, err)
	}
	if err := recording.validateMetadata(); err != nil {
		return ControllerRecording{}, err
	}
	recording.Frames = make(map[string][]byte)
	for _, operation := range recording.Operations {
		if operation.Frame == nil {
			continue
		}
		framePath, err := resolveRecordingPath(absolute, operation.Frame.Path)
		if err != nil {
			return ControllerRecording{}, fmt.Errorf("%w: %v", ErrInvalidRecording, err)
		}
		frameInfo, err := os.Lstat(framePath)
		if err != nil {
			return ControllerRecording{}, err
		}
		if !frameInfo.Mode().IsRegular() || frameInfo.Mode()&os.ModeSymlink != 0 || frameInfo.Size() > maxRecordedFrameBytes {
			return ControllerRecording{}, fmt.Errorf("%w: invalid frame file %q", ErrInvalidRecording, operation.Frame.Path)
		}
		data, err := os.ReadFile(framePath)
		if err != nil {
			return ControllerRecording{}, err
		}
		recording.Frames[operation.Frame.Path] = data
	}
	if err := recording.Validate(); err != nil {
		return ControllerRecording{}, err
	}
	return recording, nil
}

func validateRecordingPath(value string) error {
	if value == "" || strings.ContainsRune(value, '\x00') || strings.Contains(value, "\\") || strings.Contains(value, ":") || strings.HasPrefix(value, "/") {
		return errors.New("recording path is empty, absolute, or uses a forbidden separator")
	}
	if path.Clean(value) != value || value == "." {
		return errors.New("recording path is not normalized")
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return errors.New("recording path contains an unsafe segment")
		}
	}
	return nil
}

func resolveRecordingPath(root, relative string) (string, error) {
	if err := validateRecordingPath(relative); err != nil {
		return "", err
	}
	resolved := filepath.Join(root, filepath.FromSlash(relative))
	relativeCheck, err := filepath.Rel(root, resolved)
	if err != nil || relativeCheck == ".." || strings.HasPrefix(relativeCheck, ".."+string(filepath.Separator)) {
		return "", errors.New("recording path escapes root")
	}
	parent := filepath.Dir(resolved)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return "", err
	}
	if !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("recording path traverses an unsafe parent")
	}
	return resolved, nil
}

func writeRecordingFile(filename string, data []byte) error {
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

func cloneControllerRecording(recording ControllerRecording) ControllerRecording {
	data, err := json.Marshal(recording)
	if err != nil {
		panic(err)
	}
	var clone ControllerRecording
	if err := json.Unmarshal(data, &clone); err != nil {
		panic(err)
	}
	clone.Frames = make(map[string][]byte, len(recording.Frames))
	for framePath, frame := range recording.Frames {
		clone.Frames[framePath] = bytes.Clone(frame)
	}
	return clone
}
