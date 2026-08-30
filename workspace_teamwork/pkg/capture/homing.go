package capture

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
)

type HomingConfig struct {
	MaxAttempts        int           `json:"max_attempts"`
	DragDistance       float64       `json:"drag_distance"`
	DragDuration       time.Duration `json:"drag_duration_ns"`
	SettlingDelay      time.Duration `json:"settling_delay_ns"`
	ClampTolerance     float64       `json:"clamp_tolerance"`
	ClampConfirmations int           `json:"clamp_confirmations"`
	MinimumConfidence  float64       `json:"minimum_confidence"`
}

func DefaultHomingConfig() HomingConfig {
	return HomingConfig{
		MaxAttempts:        15,
		DragDistance:       300,
		DragDuration:       250 * time.Millisecond,
		SettlingDelay:      50 * time.Millisecond,
		ClampTolerance:     0.5,
		ClampConfirmations: 2,
		MinimumConfidence:  0.7,
	}
}

type HomingResult struct {
	AnchorFrame   Frame
	ConfirmedLeft bool
	ConfirmedTop  bool
	TotalDrags    int
	Events        []MotionObservation
}

type HomingExecutor struct {
	observer *MotionObserver
	config   HomingConfig
}

func NewHomingExecutor(observer *MotionObserver, config HomingConfig) (*HomingExecutor, error) {
	if observer == nil {
		return nil, errors.New("motion observer is required")
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = 15
	}
	if config.DragDistance <= 0 {
		config.DragDistance = 300
	}
	if config.ClampConfirmations <= 0 {
		config.ClampConfirmations = 2
	}
	if config.MinimumConfidence <= 0 {
		config.MinimumConfidence = 0.7
	}
	return &HomingExecutor{
		observer: observer,
		config:   config,
	}, nil
}

func (h *HomingExecutor) Execute(ctx context.Context, ctrl Controller) (*HomingResult, error) {
	if ctrl == nil {
		return nil, errors.New("controller is required")
	}
	viewport, err := ctrl.InputViewport(ctx)
	if err != nil {
		return nil, fmt.Errorf("homing get viewport: %w", err)
	}
	if viewport.Width <= 0 || viewport.Height <= 0 {
		return nil, errors.New("invalid viewport dimensions")
	}

	cx := float64(viewport.Width) / 2
	cy := float64(viewport.Height) / 2

	result := &HomingResult{}

	// 1. Capture baseline frame
	currentFrame, err := ctrl.CaptureRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("homing capture baseline: %w", err)
	}

	// 2. Horizontal left homing: drag towards right to move view left (or drag left depending on inverted coords)
	// Convention: MiddleDrag from (cx, cy) to (cx + DragDistance, cy) moves content right / camera left.
	// We want to reach the leftmost boundary.
	leftClamps := 0
	attempts := 0
	for leftClamps < h.config.ClampConfirmations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attempts++
		if attempts > h.config.MaxAttempts {
			return nil, fmt.Errorf("%w: failed to confirm left boundary within %d attempts", ErrHomingFailed, h.config.MaxAttempts)
		}

		gesture := DragGesture{
			Begin:    geometry.Point{X: cx, Y: cy},
			End:      geometry.Point{X: cx + h.config.DragDistance, Y: cy},
			Duration: h.config.DragDuration,
		}
		if err := ctrl.MiddleDrag(ctx, gesture); err != nil {
			return nil, fmt.Errorf("homing left drag: %w", err)
		}
		if h.config.SettlingDelay > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(h.config.SettlingDelay):
			}
		}
		result.TotalDrags++

		nextFrame, err := ctrl.CaptureRaw(ctx)
		if err != nil {
			return nil, fmt.Errorf("homing capture after drag: %w", err)
		}

		intent := MoveIntent{
			Direction: DirectionLeft,
			Distance:  h.config.DragDistance,
			Purpose:   PurposeConfirmEdge,
		}
		evidenceID := fmt.Sprintf("homing-left-%03d", attempts)
		obs, err := h.observer.Classify(currentFrame, nextFrame, intent, evidenceID)
		if err != nil {
			return nil, fmt.Errorf("homing classify left drag: %w", err)
		}
		result.Events = append(result.Events, obs)

		if obs.Kind == MotionClamped {
			leftClamps++
		} else {
			leftClamps = 0
		}
		currentFrame = nextFrame
	}
	result.ConfirmedLeft = true

	// 3. Vertical top homing: drag down to move view up / reach top boundary
	topClamps := 0
	attempts = 0
	for topClamps < h.config.ClampConfirmations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attempts++
		if attempts > h.config.MaxAttempts {
			return nil, fmt.Errorf("%w: failed to confirm top boundary within %d attempts", ErrHomingFailed, h.config.MaxAttempts)
		}

		gesture := DragGesture{
			Begin:    geometry.Point{X: cx, Y: cy},
			End:      geometry.Point{X: cx, Y: cy + h.config.DragDistance},
			Duration: h.config.DragDuration,
		}
		if err := ctrl.MiddleDrag(ctx, gesture); err != nil {
			return nil, fmt.Errorf("homing top drag: %w", err)
		}
		if h.config.SettlingDelay > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(h.config.SettlingDelay):
			}
		}
		result.TotalDrags++

		nextFrame, err := ctrl.CaptureRaw(ctx)
		if err != nil {
			return nil, fmt.Errorf("homing capture after drag: %w", err)
		}

		intent := MoveIntent{
			Direction: DirectionUp,
			Distance:  h.config.DragDistance,
			Purpose:   PurposeConfirmEdge,
		}
		evidenceID := fmt.Sprintf("homing-top-%03d", attempts)
		obs, err := h.observer.Classify(currentFrame, nextFrame, intent, evidenceID)
		if err != nil {
			return nil, fmt.Errorf("homing classify top drag: %w", err)
		}
		result.Events = append(result.Events, obs)

		if obs.Kind == MotionClamped {
			topClamps++
		} else {
			topClamps = 0
		}
		currentFrame = nextFrame
	}
	result.ConfirmedTop = true

	// 4. Capture origin anchor frame
	anchorFrame, err := ctrl.CaptureRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("homing capture anchor: %w", err)
	}
	result.AnchorFrame = anchorFrame

	return result, nil
}
