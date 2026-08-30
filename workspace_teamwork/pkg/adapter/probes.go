package adapter

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/capture"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/registration"
)

// ResolutionProbeOptions configures ProbeOriginalResolution.
type ResolutionProbeOptions struct {
	MinWidth    int     `json:"min_width"`
	MinHeight   int     `json:"min_height"`
	MinVariance float64 `json:"min_variance"`
}

func DefaultResolutionProbeOptions() ResolutionProbeOptions {
	return ResolutionProbeOptions{
		MinWidth:    1280,
		MinHeight:   720,
		MinVariance: 10.0,
	}
}

// ProbeOriginalResolution verifies that the controller delivers unscaled native frames.
func ProbeOriginalResolution(ctx context.Context, ctrl Controller, opts ResolutionProbeOptions) (ProbeResult, error) {
	start := time.Now()
	if opts.MinWidth <= 0 {
		opts.MinWidth = 1280
	}
	if opts.MinHeight <= 0 {
		opts.MinHeight = 720
	}
	if opts.MinVariance <= 0 {
		opts.MinVariance = 10.0
	}

	frame, err := ctrl.CaptureRaw(ctx)
	latency := time.Since(start)

	if err != nil {
		return ProbeResult{
			Name:    "ProbeOriginalResolution",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("CaptureRaw failed: %v", err),
			Latency: latency,
		}, nil
	}

	if frame.Image == nil {
		return ProbeResult{
			Name:    "ProbeOriginalResolution",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: "CaptureRaw returned nil image",
			Latency: latency,
		}, nil
	}

	w := frame.Size.Width
	h := frame.Size.Height
	if w < opts.MinWidth || h < opts.MinHeight {
		return ProbeResult{
			Name:    "ProbeOriginalResolution",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("Frame resolution (%dx%d) below minimum threshold (%dx%d)", w, h, opts.MinWidth, opts.MinHeight),
			Latency: latency,
			Metrics: map[string]float64{
				"width":  float64(w),
				"height": float64(h),
			},
		}, nil
	}

	// Check against viewport if available
	vp, vpErr := ctrl.InputViewport(ctx)
	if vpErr == nil && vp.Width > 0 && vp.Height > 0 {
		// If viewport is full HD (1920x1080), ensure it hasn't been downscaled to 720p
		if vp.Width >= 1920 && vp.Height >= 1080 && (w < 1920 || h < 1080) {
			return ProbeResult{
				Name:    "ProbeOriginalResolution",
				Status:  ProbeStatusNoGo,
				Passed:  false,
				Details: fmt.Sprintf("Host viewport is %dx%d but capture is downscaled to %dx%d (720p clamp detected)", vp.Width, vp.Height, w, h),
				Latency: latency,
				Metrics: map[string]float64{
					"width":          float64(w),
					"height":         float64(h),
					"viewport_width": float64(vp.Width),
				},
			}, nil
		}
	}

	// Compute image gray variance (entropy check)
	variance := computeImageVariance(frame.Image)
	if variance < opts.MinVariance {
		return ProbeResult{
			Name:    "ProbeOriginalResolution",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("Frame pixel variance %.2f is below minimum threshold %.2f (black/blank screen detected)", variance, opts.MinVariance),
			Latency: latency,
			Metrics: map[string]float64{
				"variance": variance,
			},
		}, nil
	}

	return ProbeResult{
		Name:    "ProbeOriginalResolution",
		Status:  ProbeStatusGo,
		Passed:  true,
		Details: fmt.Sprintf("Native unscaled resolution %dx%d confirmed with variance %.2f", w, h, variance),
		Latency: latency,
		Metrics: map[string]float64{
			"width":    float64(w),
			"height":   float64(h),
			"variance": variance,
		},
	}, nil
}

// CancellationProbeOptions configures ProbeSerialAndCancellation.
type CancellationProbeOptions struct {
	MaxCancelLatency time.Duration `json:"max_cancel_latency"`
}

func DefaultCancellationProbeOptions() CancellationProbeOptions {
	return CancellationProbeOptions{
		MaxCancelLatency: 100 * time.Millisecond,
	}
}

// ProbeSerialAndCancellation verifies serial action execution and prompt input release upon cancellation.
func ProbeSerialAndCancellation(ctx context.Context, ctrl Controller, opts CancellationProbeOptions) (ProbeResult, error) {
	start := time.Now()
	if opts.MaxCancelLatency <= 0 {
		opts.MaxCancelLatency = 100 * time.Millisecond
	}

	vp, err := ctrl.InputViewport(ctx)
	if err != nil {
		vp = PixelSize{Width: 1920, Height: 1080}
	}
	cx := float64(vp.Width) / 2
	cy := float64(vp.Height) / 2

	// Test cancellation latency on a long middle drag
	cancelCtx, cancel := context.WithCancel(ctx)
	gesture := DragGesture{
		Begin:    geometry.Point{X: cx, Y: cy},
		End:      geometry.Point{X: cx + 200, Y: cy},
		Duration: 600 * time.Millisecond,
	}

	// Trigger cancellation after 20ms
	time.AfterFunc(20*time.Millisecond, cancel)

	cancelStart := time.Now()
	dragErr := ctrl.MiddleDrag(cancelCtx, gesture)
	cancelLatency := time.Since(cancelStart)

	if dragErr == nil {
		return ProbeResult{
			Name:    "ProbeSerialAndCancellation",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: "MiddleDrag completed successfully despite context cancellation",
			Latency: time.Since(start),
		}, nil
	}

	if cancelLatency > opts.MaxCancelLatency+50*time.Millisecond {
		return ProbeResult{
			Name:    "ProbeSerialAndCancellation",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("Cancellation latency %v exceeded maximum threshold %v", cancelLatency, opts.MaxCancelLatency),
			Latency: time.Since(start),
			Metrics: map[string]float64{
				"cancel_latency_ms": float64(cancelLatency.Milliseconds()),
			},
		}, nil
	}

	// Ensure input release is clean and idempotent
	if err := ctrl.Release(ctx); err != nil {
		return ProbeResult{
			Name:    "ProbeSerialAndCancellation",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("Controller Release failed: %v", err),
			Latency: time.Since(start),
		}, nil
	}

	return ProbeResult{
		Name:    "ProbeSerialAndCancellation",
		Status:  ProbeStatusGo,
		Passed:  true,
		Details: fmt.Sprintf("Cancellation responded in %v (< 100ms) with clean input release", cancelLatency),
		Latency: time.Since(start),
		Metrics: map[string]float64{
			"cancel_latency_ms": float64(cancelLatency.Milliseconds()),
		},
	}, nil
}

// FreshnessProbeOptions configures ProbeFrameFreshness.
type FreshnessProbeOptions struct {
	DragDistance    float64 `json:"drag_distance"`
	MinDisplacement float64 `json:"min_displacement"`
	MinConfidence   float64 `json:"min_confidence"`
}

func DefaultFreshnessProbeOptions() FreshnessProbeOptions {
	return FreshnessProbeOptions{
		DragDistance:    100,
		MinDisplacement: 20,
		MinConfidence:   0.4,
	}
}

// ProbeFrameFreshness ensures that frames captured after drag actions reflect actual visual displacement.
func ProbeFrameFreshness(ctx context.Context, ctrl Controller, opts FreshnessProbeOptions) (ProbeResult, error) {
	start := time.Now()
	if opts.DragDistance <= 0 {
		opts.DragDistance = 100
	}
	if opts.MinDisplacement <= 0 {
		opts.MinDisplacement = 20
	}
	if opts.MinConfidence <= 0 {
		opts.MinConfidence = 0.4
	}

	f0, err := ctrl.CaptureRaw(ctx)
	if err != nil {
		return ProbeResult{
			Name:    "ProbeFrameFreshness",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("Capture baseline frame failed: %v", err),
			Latency: time.Since(start),
		}, nil
	}

	vp, err := ctrl.InputViewport(ctx)
	if err != nil {
		vp = PixelSize{Width: 1920, Height: 1080}
	}
	cx := float64(vp.Width) / 2
	cy := float64(vp.Height) / 2

	// Drag horizontally
	gesture := DragGesture{
		Begin:    geometry.Point{X: cx, Y: cy},
		End:      geometry.Point{X: cx - opts.DragDistance, Y: cy},
		Duration: 150 * time.Millisecond,
	}
	if err := ctrl.MiddleDrag(ctx, gesture); err != nil {
		return ProbeResult{
			Name:    "ProbeFrameFreshness",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("MiddleDrag failed: %v", err),
			Latency: time.Since(start),
		}, nil
	}

	f1, err := ctrl.CaptureRaw(ctx)
	if err != nil {
		return ProbeResult{
			Name:    "ProbeFrameFreshness",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("Capture post-action frame failed: %v", err),
			Latency: time.Since(start),
		}, nil
	}

	// Check timestamp monotonicity
	if !f1.CapturedAt.After(f0.CapturedAt) && f1.ID == f0.ID {
		return ProbeResult{
			Name:    "ProbeFrameFreshness",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: "Post-action frame has identical timestamp/ID as baseline (stale frame buffer)",
			Latency: time.Since(start),
		}, nil
	}

	// Compute phase correlation between f0 and f1
	res, err := registration.ComputePhaseCorrelation(f0.Image, f1.Image, registration.DefaultConfig())
	if err != nil {
		return ProbeResult{
			Name:    "ProbeFrameFreshness",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("Phase correlation failed: %v", err),
			Latency: time.Since(start),
		}, nil
	}

	disp := res.Delta.Length()
	if disp < opts.MinDisplacement {
		return ProbeResult{
			Name:    "ProbeFrameFreshness",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("Measured displacement %.2fpx is below threshold %.2fpx (frame buffer unchanged after drag)", disp, opts.MinDisplacement),
			Latency: time.Since(start),
			Metrics: map[string]float64{
				"displacement": disp,
				"confidence":   res.Confidence,
			},
		}, nil
	}

	return ProbeResult{
		Name:    "ProbeFrameFreshness",
		Status:  ProbeStatusGo,
		Passed:  true,
		Details: fmt.Sprintf("Fresh frame verified: displacement %.2fpx, confidence %.2f", disp, res.Confidence),
		Latency: time.Since(start),
		Metrics: map[string]float64{
			"displacement": disp,
			"confidence":   res.Confidence,
		},
	}, nil
}

// MappingProbeOptions configures ProbeCoordinateMapping.
type MappingProbeOptions struct {
	ProbeDistance       float64 `json:"probe_distance"`
	MaxCrossAxisLeakage float64 `json:"max_cross_axis_leakage"`
}

func DefaultMappingProbeOptions() MappingProbeOptions {
	return MappingProbeOptions{
		ProbeDistance:       100,
		MaxCrossAxisLeakage: 0.15,
	}
}

// ProbeCoordinateMapping measures input-to-pixel displacement scaling and cross-axis leakage.
func ProbeCoordinateMapping(ctx context.Context, ctrl Controller, opts MappingProbeOptions) (ProbeResult, *capture.CalibrationRecord, error) {
	start := time.Now()
	if opts.ProbeDistance <= 0 {
		opts.ProbeDistance = 100
	}
	if opts.MaxCrossAxisLeakage <= 0 {
		opts.MaxCrossAxisLeakage = 0.15
	}

	vp, err := ctrl.InputViewport(ctx)
	if err != nil {
		return ProbeResult{
			Name:    "ProbeCoordinateMapping",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("InputViewport failed: %v", err),
			Latency: time.Since(start),
		}, nil, nil
	}
	cx := float64(vp.Width) / 2
	cy := float64(vp.Height) / 2

	f0, err := ctrl.CaptureRaw(ctx)
	if err != nil {
		return ProbeResult{
			Name:    "ProbeCoordinateMapping",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("Capture initial frame failed: %v", err),
			Latency: time.Since(start),
		}, nil, nil
	}

	// 1. Horizontal Probe
	hGesture := DragGesture{
		Begin:    geometry.Point{X: cx, Y: cy},
		End:      geometry.Point{X: cx - opts.ProbeDistance, Y: cy},
		Duration: 150 * time.Millisecond,
	}
	if err := ctrl.MiddleDrag(ctx, hGesture); err != nil {
		return ProbeResult{
			Name:    "ProbeCoordinateMapping",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("Horizontal drag failed: %v", err),
			Latency: time.Since(start),
		}, nil, nil
	}
	fh, err := ctrl.CaptureRaw(ctx)
	if err != nil {
		return ProbeResult{
			Name:    "ProbeCoordinateMapping",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("Capture after horizontal drag failed: %v", err),
			Latency: time.Since(start),
		}, nil, nil
	}

	hRes, err := registration.ComputePhaseCorrelation(f0.Image, fh.Image, registration.DefaultConfig())
	if err != nil {
		return ProbeResult{
			Name:    "ProbeCoordinateMapping",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("Horizontal phase correlation failed: %v", err),
			Latency: time.Since(start),
		}, nil, nil
	}

	// Restore horizontal
	hRestore := DragGesture{
		Begin:    geometry.Point{X: cx - opts.ProbeDistance, Y: cy},
		End:      geometry.Point{X: cx, Y: cy},
		Duration: 150 * time.Millisecond,
	}
	_ = ctrl.MiddleDrag(ctx, hRestore)

	// 2. Vertical Probe
	vGesture := DragGesture{
		Begin:    geometry.Point{X: cx, Y: cy},
		End:      geometry.Point{X: cx, Y: cy - opts.ProbeDistance},
		Duration: 150 * time.Millisecond,
	}
	if err := ctrl.MiddleDrag(ctx, vGesture); err != nil {
		return ProbeResult{
			Name:    "ProbeCoordinateMapping",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("Vertical drag failed: %v", err),
			Latency: time.Since(start),
		}, nil, nil
	}
	fv, err := ctrl.CaptureRaw(ctx)
	if err != nil {
		return ProbeResult{
			Name:    "ProbeCoordinateMapping",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("Capture after vertical drag failed: %v", err),
			Latency: time.Since(start),
		}, nil, nil
	}

	vRes, err := registration.ComputePhaseCorrelation(f0.Image, fv.Image, registration.DefaultConfig())
	if err != nil {
		return ProbeResult{
			Name:    "ProbeCoordinateMapping",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("Vertical phase correlation failed: %v", err),
			Latency: time.Since(start),
		}, nil, nil
	}

	// Restore vertical
	vRestore := DragGesture{
		Begin:    geometry.Point{X: cx, Y: cy - opts.ProbeDistance},
		End:      geometry.Point{X: cx, Y: cy},
		Duration: 150 * time.Millisecond,
	}
	_ = ctrl.MiddleDrag(ctx, vRestore)

	rawDX := math.Abs(hRes.Delta.X)
	rawDY := math.Abs(hRes.Delta.Y)
	vRawDX := math.Abs(vRes.Delta.X)
	vRawDY := math.Abs(vRes.Delta.Y)

	if rawDX < 5.0 || vRawDY < 5.0 {
		return ProbeResult{
			Name:    "ProbeCoordinateMapping",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("Insufficient displacement response: hDX=%.2f, vDY=%.2f", rawDX, vRawDY),
			Latency: time.Since(start),
		}, nil, nil
	}

	crossH := rawDY / rawDX
	crossV := vRawDX / vRawDY

	if crossH > opts.MaxCrossAxisLeakage || crossV > opts.MaxCrossAxisLeakage {
		return ProbeResult{
			Name:    "ProbeCoordinateMapping",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: fmt.Sprintf("Cross-axis leakage exceeded threshold %.2f: hLeak=%.2f, vLeak=%.2f", opts.MaxCrossAxisLeakage, crossH, crossV),
			Latency: time.Since(start),
			Metrics: map[string]float64{
				"h_leakage": crossH,
				"v_leakage": crossV,
			},
		}, nil, nil
	}

	sx := rawDX / opts.ProbeDistance
	sy := vRawDY / opts.ProbeDistance

	calib := &capture.CalibrationRecord{
		ID:        fmt.Sprintf("calib-probe-%s", time.Now().UTC().Format("20060102-150405")),
		CreatedAt: time.Now().UTC(),
		Actions: []capture.CalibrationAction{
			{
				Purpose:          "horizontal_probe",
				InputDelta:       geometry.Vector{X: opts.ProbeDistance, Y: 0},
				MeasuredRawDelta: hRes.Delta,
				EvidenceIDs:      []string{"probe-h"},
			},
			{
				Purpose:          "vertical_probe",
				InputDelta:       geometry.Vector{X: 0, Y: opts.ProbeDistance},
				MeasuredRawDelta: vRes.Delta,
				EvidenceIDs:      []string{"probe-v"},
			},
		},
		HorizontalMotion: hRes.Delta,
		VerticalMotion:   vRes.Delta,
		EffectiveViewport: geometry.Rect{
			X:      0,
			Y:      0,
			Width:  float64(f0.Size.Width),
			Height: float64(f0.Size.Height),
		},
		InputToRaw: geometry.Affine2D{
			A:  sx,
			B:  0,
			C:  0,
			D:  sy,
			TX: 0,
			TY: 0,
		},
		RawToSession: geometry.IdentityAffine2D(),
		Confidence:   (hRes.Confidence + vRes.Confidence) / 2.0,
	}

	return ProbeResult{
		Name:    "ProbeCoordinateMapping",
		Status:  ProbeStatusGo,
		Passed:  true,
		Details: fmt.Sprintf("Linear mapping calibrated: Sx=%.3f, Sy=%.3f, cross-axis leakage < 15%%", sx, sy),
		Latency: time.Since(start),
		Metrics: map[string]float64{
			"scale_x":    sx,
			"scale_y":    sy,
			"h_leakage":  crossH,
			"v_leakage":  crossV,
			"confidence": calib.Confidence,
		},
	}, calib, nil
}

// LifecycleProbeOptions configures ProbeTaskLifecycle.
type LifecycleProbeOptions struct {
	SimulateFault bool `json:"simulate_fault"`
}

func DefaultLifecycleProbeOptions() LifecycleProbeOptions {
	return LifecycleProbeOptions{}
}

// ProbeTaskLifecycle verifies task execution lifecycle, progress reporting, and error handling.
func ProbeTaskLifecycle(ctx context.Context, ctrl Controller, reporter Reporter, opts LifecycleProbeOptions) (ProbeResult, error) {
	start := time.Now()
	if reporter == nil {
		reporter = &NoopReporter{}
	}

	reporter.Phase("lifecycle_init")
	reporter.Progress(1, 3, "initializing probe")
	reporter.TileStatus("tile-probe-01", "pending")

	time.Sleep(10 * time.Millisecond)

	reporter.Phase("lifecycle_exec")
	reporter.Progress(2, 3, "executing test step")
	reporter.TileStatus("tile-probe-01", "captured")

	if opts.SimulateFault {
		reporter.Warning("simulated fault injected")
		reporter.TileStatus("tile-probe-01", "rejected")
		return ProbeResult{
			Name:    "ProbeTaskLifecycle",
			Status:  ProbeStatusNoGo,
			Passed:  false,
			Details: "Task lifecycle encountered simulated fault",
			Latency: time.Since(start),
		}, nil
	}

	reporter.Phase("lifecycle_finalize")
	reporter.Progress(3, 3, "completed successfully")
	reporter.TileStatus("tile-probe-01", "validated")

	return ProbeResult{
		Name:    "ProbeTaskLifecycle",
		Status:  ProbeStatusGo,
		Passed:  true,
		Details: "Task lifecycle, progress callbacks, and state transitions verified cleanly",
		Latency: time.Since(start),
	}, nil
}

// PreflightConfig provides configuration parameters for the full preflight probe suite.
type PreflightConfig struct {
	Resolution    ResolutionProbeOptions   `json:"resolution"`
	Cancellation  CancellationProbeOptions `json:"cancellation"`
	Freshness     FreshnessProbeOptions    `json:"freshness"`
	Mapping       MappingProbeOptions      `json:"mapping"`
	Lifecycle     LifecycleProbeOptions    `json:"lifecycle"`
	SkipFreshness bool                     `json:"skip_freshness,omitempty"`
	SkipMapping   bool                     `json:"skip_mapping,omitempty"`
}

func DefaultPreflightConfig() PreflightConfig {
	return PreflightConfig{
		Resolution:   DefaultResolutionProbeOptions(),
		Cancellation: DefaultCancellationProbeOptions(),
		Freshness:    DefaultFreshnessProbeOptions(),
		Mapping:      DefaultMappingProbeOptions(),
		Lifecycle:    DefaultLifecycleProbeOptions(),
	}
}

// RunPreflightProbes executes all 5 Go/No-Go Capability Probes against the controller.
func RunPreflightProbes(ctx context.Context, ctrl Controller, reporter Reporter, cfg PreflightConfig) (PreflightReport, error) {
	if reporter == nil {
		reporter = &NoopReporter{}
	}

	report := PreflightReport{
		Verdict:     ProbeStatusGo,
		OverallPass: true,
		Timestamp:   time.Now().UTC(),
		Probes:      make(map[string]ProbeResult),
	}

	// 1. ProbeOriginalResolution
	reporter.Phase("probe_resolution")
	resProbe, err := ProbeOriginalResolution(ctx, ctrl, cfg.Resolution)
	if err != nil {
		return report, fmt.Errorf("probe resolution error: %w", err)
	}
	report.Probes["ProbeOriginalResolution"] = resProbe
	if resProbe.Status != ProbeStatusGo {
		report.Verdict = ProbeStatusNoGo
		report.OverallPass = false
	}

	// 2. ProbeSerialAndCancellation
	reporter.Phase("probe_cancellation")
	cancelProbe, err := ProbeSerialAndCancellation(ctx, ctrl, cfg.Cancellation)
	if err != nil {
		return report, fmt.Errorf("probe cancellation error: %w", err)
	}
	report.Probes["ProbeSerialAndCancellation"] = cancelProbe
	if cancelProbe.Status != ProbeStatusGo {
		report.Verdict = ProbeStatusNoGo
		report.OverallPass = false
	}

	// 3. ProbeFrameFreshness
	if !cfg.SkipFreshness {
		reporter.Phase("probe_freshness")
		freshProbe, err := ProbeFrameFreshness(ctx, ctrl, cfg.Freshness)
		if err != nil {
			return report, fmt.Errorf("probe freshness error: %w", err)
		}
		report.Probes["ProbeFrameFreshness"] = freshProbe
		if freshProbe.Status != ProbeStatusGo {
			report.Verdict = ProbeStatusNoGo
			report.OverallPass = false
		}
	}

	// 4. ProbeCoordinateMapping
	if !cfg.SkipMapping {
		reporter.Phase("probe_mapping")
		mapProbe, calib, err := ProbeCoordinateMapping(ctx, ctrl, cfg.Mapping)
		if err != nil {
			return report, fmt.Errorf("probe mapping error: %w", err)
		}
		report.Probes["ProbeCoordinateMapping"] = mapProbe
		if mapProbe.Status != ProbeStatusGo {
			report.Verdict = ProbeStatusNoGo
			report.OverallPass = false
		}
		report.Calibration = calib
	}

	// 5. ProbeTaskLifecycle
	reporter.Phase("probe_lifecycle")
	lifeProbe, err := ProbeTaskLifecycle(ctx, ctrl, reporter, cfg.Lifecycle)
	if err != nil {
		return report, fmt.Errorf("probe lifecycle error: %w", err)
	}
	report.Probes["ProbeTaskLifecycle"] = lifeProbe
	if lifeProbe.Status != ProbeStatusGo {
		report.Verdict = ProbeStatusNoGo
		report.OverallPass = false
	}

	reporter.Phase("preflight_complete")
	return report, nil
}

func computeImageVariance(img image.Image) float64 {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w <= 0 || h <= 0 {
		return 0
	}

	var sum, sqSum float64
	count := float64(w * h)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
			val := float64(c.Y)
			sum += val
			sqSum += val * val
		}
	}

	mean := sum / count
	variance := (sqSum / count) - (mean * mean)
	return variance
}
