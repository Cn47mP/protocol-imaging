package capture

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
)

type CalibratorConfig struct {
	ProbeDistance     float64       `json:"probe_distance"`
	ProbeDuration     time.Duration `json:"probe_duration_ns"`
	SettlingDelay     time.Duration `json:"settling_delay_ns"`
	ClampTolerance    float64       `json:"clamp_tolerance"`
	MaxAnchorDrift    float64       `json:"max_anchor_drift"`
	MinimumConfidence float64       `json:"minimum_confidence"`
	MaxCouplingRatio  float64       `json:"max_coupling_ratio"`
}

func DefaultCalibratorConfig() CalibratorConfig {
	return CalibratorConfig{
		ProbeDistance:     100,
		ProbeDuration:     200 * time.Millisecond,
		SettlingDelay:     50 * time.Millisecond,
		ClampTolerance:    0.5,
		MaxAnchorDrift:    2.0,
		MinimumConfidence: 0.7,
		MaxCouplingRatio:  0.1,
	}
}

type Calibrator struct {
	observer *MotionObserver
	config   CalibratorConfig
}

func NewCalibrator(observer *MotionObserver, config CalibratorConfig) (*Calibrator, error) {
	if observer == nil {
		return nil, errors.New("motion observer is required")
	}
	if config.ProbeDistance <= 0 {
		config.ProbeDistance = 100
	}
	if config.MaxAnchorDrift <= 0 {
		config.MaxAnchorDrift = 2.0
	}
	if config.MinimumConfidence <= 0 {
		config.MinimumConfidence = 0.7
	}
	if config.MaxCouplingRatio <= 0 {
		config.MaxCouplingRatio = 0.1
	}
	return &Calibrator{
		observer: observer,
		config:   config,
	}, nil
}

func (c *Calibrator) Calibrate(ctx context.Context, ctrl Controller, anchor Frame) (*CalibrationRecord, error) {
	if ctrl == nil {
		return nil, errors.New("controller is required")
	}
	viewport, err := ctrl.InputViewport(ctx)
	if err != nil {
		return nil, fmt.Errorf("calibrator get viewport: %w", err)
	}
	cx := float64(viewport.Width) / 2
	cy := float64(viewport.Height) / 2

	var actions []CalibrationAction
	var totalConfidence float64

	// 1. Horizontal calibration probe
	hGesture := DragGesture{
		Begin:    geometry.Point{X: cx, Y: cy},
		End:      geometry.Point{X: cx - c.config.ProbeDistance, Y: cy},
		Duration: c.config.ProbeDuration,
	}
	if err := ctrl.MiddleDrag(ctx, hGesture); err != nil {
		return nil, fmt.Errorf("horizontal calibration probe drag: %w", err)
	}
	if c.config.SettlingDelay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.config.SettlingDelay):
		}
	}
	hAfterFrame, err := ctrl.CaptureRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("capture after horizontal probe: %w", err)
	}

	hIntent := MoveIntent{
		Direction: DirectionRight,
		Distance:  c.config.ProbeDistance,
		Purpose:   PurposeTraverse,
	}
	hObs, err := c.observer.Classify(anchor, hAfterFrame, hIntent, "calib-h-01")
	if err != nil {
		return nil, fmt.Errorf("classify horizontal probe: %w", err)
	}
	if hObs.Kind == MotionUncertain || hObs.Confidence < c.config.MinimumConfidence {
		return nil, fmt.Errorf("%w: horizontal probe had low confidence or uncertain motion (%g)", ErrCalibrationFailed, hObs.Confidence)
	}

	rawDX := math.Abs(hObs.Delta.X)
	rawDY := math.Abs(hObs.Delta.Y)
	if rawDX <= c.config.ClampTolerance {
		return nil, fmt.Errorf("%w: horizontal probe measured insufficient delta %g", ErrCalibrationFailed, rawDX)
	}
	if rawDX > 0 && (rawDY/rawDX) > c.config.MaxCouplingRatio {
		return nil, fmt.Errorf("%w: horizontal probe cross-coupling %g exceeds limit %g", ErrCalibrationFailed, rawDY/rawDX, c.config.MaxCouplingRatio)
	}

	actions = append(actions, CalibrationAction{
		Purpose:          "horizontal_probe",
		InputDelta:       geometry.Vector{X: c.config.ProbeDistance, Y: 0},
		MeasuredRawDelta: hObs.Delta,
		EvidenceIDs:      []string{"calib-h-01"},
	})
	totalConfidence += hObs.Confidence

	// Restore from horizontal probe
	hRestoreGesture := DragGesture{
		Begin:    geometry.Point{X: cx - c.config.ProbeDistance, Y: cy},
		End:      geometry.Point{X: cx, Y: cy},
		Duration: c.config.ProbeDuration,
	}
	if err := ctrl.MiddleDrag(ctx, hRestoreGesture); err != nil {
		return nil, fmt.Errorf("restore horizontal probe drag: %w", err)
	}
	if c.config.SettlingDelay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.config.SettlingDelay):
		}
	}

	// Verify anchor restore
	hRestoreFrame, err := ctrl.CaptureRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("capture after horizontal restore: %w", err)
	}
	hRestoreObs, err := c.observer.Classify(anchor, hRestoreFrame, MoveIntent{Direction: DirectionLeft, Distance: c.config.ProbeDistance, Purpose: PurposeConfirmEdge}, "calib-h-restore")
	if err == nil && hRestoreObs.Delta.Length() > c.config.MaxAnchorDrift {
		return nil, fmt.Errorf("%w: drift %g after horizontal restore exceeds tolerance %g", ErrDisplacedAnchor, hRestoreObs.Delta.Length(), c.config.MaxAnchorDrift)
	}

	// 2. Vertical calibration probe
	vGesture := DragGesture{
		Begin:    geometry.Point{X: cx, Y: cy},
		End:      geometry.Point{X: cx, Y: cy - c.config.ProbeDistance},
		Duration: c.config.ProbeDuration,
	}
	if err := ctrl.MiddleDrag(ctx, vGesture); err != nil {
		return nil, fmt.Errorf("vertical calibration probe drag: %w", err)
	}
	if c.config.SettlingDelay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.config.SettlingDelay):
		}
	}
	vAfterFrame, err := ctrl.CaptureRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("capture after vertical probe: %w", err)
	}

	vIntent := MoveIntent{
		Direction: DirectionDown,
		Distance:  c.config.ProbeDistance,
		Purpose:   PurposeDescend,
	}
	vObs, err := c.observer.Classify(anchor, vAfterFrame, vIntent, "calib-v-01")
	if err != nil {
		return nil, fmt.Errorf("classify vertical probe: %w", err)
	}
	if vObs.Kind == MotionUncertain || vObs.Confidence < c.config.MinimumConfidence {
		return nil, fmt.Errorf("%w: vertical probe had low confidence or uncertain motion (%g)", ErrCalibrationFailed, vObs.Confidence)
	}

	vRawDX := math.Abs(vObs.Delta.X)
	vRawDY := math.Abs(vObs.Delta.Y)
	if vRawDY <= c.config.ClampTolerance {
		return nil, fmt.Errorf("%w: vertical probe measured insufficient delta %g", ErrCalibrationFailed, vRawDY)
	}
	if vRawDY > 0 && (vRawDX/vRawDY) > c.config.MaxCouplingRatio {
		return nil, fmt.Errorf("%w: vertical probe cross-coupling %g exceeds limit %g", ErrCalibrationFailed, vRawDX/vRawDY, c.config.MaxCouplingRatio)
	}

	actions = append(actions, CalibrationAction{
		Purpose:          "vertical_probe",
		InputDelta:       geometry.Vector{X: 0, Y: c.config.ProbeDistance},
		MeasuredRawDelta: vObs.Delta,
		EvidenceIDs:      []string{"calib-v-01"},
	})
	totalConfidence += vObs.Confidence

	// Restore from vertical probe
	vRestoreGesture := DragGesture{
		Begin:    geometry.Point{X: cx, Y: cy - c.config.ProbeDistance},
		End:      geometry.Point{X: cx, Y: cy},
		Duration: c.config.ProbeDuration,
	}
	if err := ctrl.MiddleDrag(ctx, vRestoreGesture); err != nil {
		return nil, fmt.Errorf("restore vertical probe drag: %w", err)
	}
	if c.config.SettlingDelay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.config.SettlingDelay):
		}
	}

	// 3. Compute scale factors and matrices
	sx := rawDX / c.config.ProbeDistance
	sy := vRawDY / c.config.ProbeDistance
	if sx <= 0 || sy <= 0 || math.IsNaN(sx) || math.IsNaN(sy) {
		return nil, fmt.Errorf("%w: invalid scale factors sx=%g, sy=%g", ErrCalibrationFailed, sx, sy)
	}

	inputToRaw := geometry.Affine2D{
		A:  sx,
		B:  0,
		C:  0,
		D:  sy,
		TX: 0,
		TY: 0,
	}
	rawToSession := geometry.IdentityAffine2D()
	effectiveViewport := geometry.Rect{
		X:      0,
		Y:      0,
		Width:  float64(anchor.Size.Width),
		Height: float64(anchor.Size.Height),
	}

	record := &CalibrationRecord{
		ID:                fmt.Sprintf("calib-%s", time.Now().UTC().Format("20060102-150405")),
		CreatedAt:         time.Now().UTC(),
		Actions:           actions,
		HorizontalMotion:  hObs.Delta,
		VerticalMotion:    vObs.Delta,
		EffectiveViewport: effectiveViewport,
		InputToRaw:        inputToRaw,
		RawToSession:      rawToSession,
		Confidence:        totalConfidence / float64(len(actions)),
	}

	return record, nil
}
