package capture

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"time"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
)

type EngineCheckpointCommit struct {
	Frontier          FrontierSnapshot
	TileID            string
	TilePath          string
	TilePNG           []byte
	CaptureState      string
	ActiveCalibration string
	UpdatedAt         time.Time
}

type ProjectSession interface {
	Root() string
	UpdateCaptureState(ctx context.Context, state string, activeCalibration string, updatedAt time.Time) error
	CommitCaptureStep(ctx context.Context, commit EngineCheckpointCommit) error
}

type Reporter interface {
	OnProgress(snapshot FrontierSnapshot, tileID string)
	OnLog(level, message string)
}

type NilReporter struct{}

func (NilReporter) OnProgress(snapshot FrontierSnapshot, tileID string) {}
func (NilReporter) OnLog(level, message string)                         {}

type EngineConfig struct {
	FrontierConfig   FrontierConfig
	HomingConfig     HomingConfig
	CalibratorConfig CalibratorConfig
	TargetOverlap    geometry.Size
	TileLayerID      string
}

func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		FrontierConfig: FrontierConfig{
			HorizontalStep:     700,
			VerticalStep:       400,
			ProbeStep:          150,
			ClampTolerance:     1.0,
			CrossAxisTolerance: 15.0,
			MinimumConfidence:  0.6,
			ClampConfirmations: 2,
			Limits: SafetyLimits{
				MaxRows:            50,
				MaxColumns:         50,
				MaxTiles:           500,
				MaxTravel:          100000,
				MaxDurationSeconds: 3600,
				MaxDiskBytes:       4 << 30, // 4GB
				MaxStepRetries:     5,
			},
		},
		HomingConfig:     DefaultHomingConfig(),
		CalibratorConfig: DefaultCalibratorConfig(),
		TargetOverlap:    geometry.Size{Width: 0.25, Height: 0.25},
		TileLayerID:      "overview",
	}
}

type Engine struct {
	config   EngineConfig
	ctrl     Controller
	observer *MotionObserver
	homing   *HomingExecutor
	calib    *Calibrator
	reporter Reporter
}

func NewEngine(config EngineConfig, ctrl Controller, observer *MotionObserver, reporter Reporter) (*Engine, error) {
	if ctrl == nil {
		return nil, errors.New("controller is required")
	}
	if observer == nil {
		return nil, errors.New("motion observer is required")
	}
	if reporter == nil {
		reporter = NilReporter{}
	}
	if config.TileLayerID == "" {
		config.TileLayerID = "overview"
	}
	homingExec, err := NewHomingExecutor(observer, config.HomingConfig)
	if err != nil {
		return nil, fmt.Errorf("init homing: %w", err)
	}
	calibrator, err := NewCalibrator(observer, config.CalibratorConfig)
	if err != nil {
		return nil, fmt.Errorf("init calibrator: %w", err)
	}
	return &Engine{
		config:   config,
		ctrl:     ctrl,
		observer: observer,
		homing:   homingExec,
		calib:    calibrator,
		reporter: reporter,
	}, nil
}

func (e *Engine) Run(ctx context.Context, session ProjectSession) error {
	if session == nil {
		return errors.New("session is required")
	}

	// 1. Homing
	e.reporter.OnLog("INFO", "Starting left-top homing")
	if err := session.UpdateCaptureState(ctx, "homing", "", time.Now().UTC()); err != nil {
		return fmt.Errorf("set state homing: %w", err)
	}
	homingResult, err := e.homing.Execute(ctx, e.ctrl)
	if err != nil {
		_ = session.UpdateCaptureState(ctx, "failed_recoverable", "", time.Now().UTC())
		return fmt.Errorf("homing execution: %w", err)
	}

	// 2. Calibration
	e.reporter.OnLog("INFO", "Starting runtime calibration")
	if err := session.UpdateCaptureState(ctx, "calibrating", "", time.Now().UTC()); err != nil {
		return fmt.Errorf("set state calibrating: %w", err)
	}
	calibration, err := e.calib.Calibrate(ctx, e.ctrl, homingResult.AnchorFrame)
	if err != nil {
		_ = session.UpdateCaptureState(ctx, "failed_recoverable", "", time.Now().UTC())
		return fmt.Errorf("calibration execution: %w", err)
	}

	// 3. Step calculations based on calibration and overlap
	fConfig := e.config.FrontierConfig
	if calibration.EffectiveViewport.Width > 0 && e.config.TargetOverlap.Width > 0 && e.config.TargetOverlap.Width < 1 {
		fConfig.HorizontalStep = calibration.EffectiveViewport.Width * (1.0 - e.config.TargetOverlap.Width)
	}
	if calibration.EffectiveViewport.Height > 0 && e.config.TargetOverlap.Height > 0 && e.config.TargetOverlap.Height < 1 {
		fConfig.VerticalStep = calibration.EffectiveViewport.Height * (1.0 - e.config.TargetOverlap.Height)
	}
	if fConfig.ProbeStep > fConfig.HorizontalStep || fConfig.ProbeStep > fConfig.VerticalStep {
		fConfig.ProbeStep = math.Min(fConfig.HorizontalStep, fConfig.VerticalStep) * 0.25
	}

	// 4. Initialize frontier with anchor tile (tile-0000)
	anchorTileID := "tile-0000"
	frontier, err := NewFrontier(fConfig, anchorTileID)
	if err != nil {
		return fmt.Errorf("init frontier: %w", err)
	}

	anchorPNG, err := encodePNG(homingResult.AnchorFrame.Image)
	if err != nil {
		return fmt.Errorf("encode anchor PNG: %w", err)
	}

	tilePath := fmt.Sprintf("layers/%s/tiles/%s.png", e.config.TileLayerID, anchorTileID)
	initCommit := EngineCheckpointCommit{
		Frontier:          frontier.Snapshot(),
		TileID:            anchorTileID,
		TilePath:          tilePath,
		TilePNG:           anchorPNG,
		CaptureState:      "capturing",
		ActiveCalibration: calibration.ID,
		UpdatedAt:         time.Now().UTC(),
	}
	if err := session.CommitCaptureStep(ctx, initCommit); err != nil {
		return fmt.Errorf("commit initial anchor tile: %w", err)
	}
	e.reporter.OnProgress(frontier.Snapshot(), anchorTileID)

	// 5. Exploration loop
	return e.exploreLoop(ctx, session, frontier, calibration.ID, homingResult.AnchorFrame)
}

func (e *Engine) Resume(ctx context.Context, session ProjectSession, snapshot FrontierSnapshot) error {
	if session == nil {
		return errors.New("session is required")
	}
	frontier, err := RestoreFrontier(snapshot)
	if err != nil {
		return fmt.Errorf("restore frontier: %w", err)
	}

	// Capture current frame as baseline
	currentFrame, err := e.ctrl.CaptureRaw(ctx)
	if err != nil {
		return fmt.Errorf("resume capture frame: %w", err)
	}

	return e.exploreLoop(ctx, session, frontier, "", currentFrame)
}

func (e *Engine) exploreLoop(ctx context.Context, session ProjectSession, frontier *Frontier, calibID string, lastFrame Frame) error {
	viewport, err := e.ctrl.InputViewport(ctx)
	if err != nil {
		return fmt.Errorf("get viewport: %w", err)
	}
	cx := float64(viewport.Width) / 2
	cy := float64(viewport.Height) / 2

	currentFrame := lastFrame
	tileSeq := len(frontier.Snapshot().Tiles)

	for {
		if err := ctx.Err(); err != nil {
			_ = session.UpdateCaptureState(ctx, "cancelled", calibID, time.Now().UTC())
			return err
		}

		intent, err := frontier.NextIntent()
		if errors.Is(err, ErrComplete) {
			e.reporter.OnLog("INFO", "Frontier capture complete")
			break
		}
		if err != nil {
			_ = session.UpdateCaptureState(ctx, "failed_recoverable", calibID, time.Now().UTC())
			return fmt.Errorf("frontier next intent: %w", err)
		}

		// Calculate drag gesture from intent
		// To move right, drag left; to move left, drag right; to move down, drag up.
		var gesture DragGesture
		switch intent.Direction {
		case DirectionRight:
			gesture = DragGesture{
				Begin:    geometry.Point{X: cx, Y: cy},
				End:      geometry.Point{X: cx - intent.Distance, Y: cy},
				Duration: 200 * time.Millisecond,
			}
		case DirectionLeft:
			gesture = DragGesture{
				Begin:    geometry.Point{X: cx, Y: cy},
				End:      geometry.Point{X: cx + intent.Distance, Y: cy},
				Duration: 200 * time.Millisecond,
			}
		case DirectionDown:
			gesture = DragGesture{
				Begin:    geometry.Point{X: cx, Y: cy},
				End:      geometry.Point{X: cx, Y: cy - intent.Distance},
				Duration: 200 * time.Millisecond,
			}
		default:
			return fmt.Errorf("unsupported direction %q", intent.Direction)
		}

		if err := e.ctrl.MiddleDrag(ctx, gesture); err != nil {
			return fmt.Errorf("explore drag: %w", err)
		}

		nextFrame, err := e.ctrl.CaptureRaw(ctx)
		if err != nil {
			return fmt.Errorf("explore capture: %w", err)
		}

		evidenceID := fmt.Sprintf("step-%04d", frontier.Snapshot().Revision)
		observation, err := e.observer.Classify(currentFrame, nextFrame, intent, evidenceID)
		if err != nil {
			return fmt.Errorf("classify observation: %w", err)
		}

		var tileID string
		var tilePNG []byte
		var tilePath string

		if observation.Kind == MotionMoved || observation.Kind == MotionPartial {
			tileID = fmt.Sprintf("tile-%04d", tileSeq)
			tileSeq++
			tilePath = fmt.Sprintf("layers/%s/tiles/%s.png", e.config.TileLayerID, tileID)
			pngData, err := encodePNG(nextFrame.Image)
			if err != nil {
				return fmt.Errorf("encode tile PNG: %w", err)
			}
			tilePNG = pngData
		}

		transition, err := frontier.Observe(observation, tileID)
		if err != nil {
			if errors.Is(err, ErrSafetyLimit) || errors.Is(err, ErrUnresolved) {
				_ = session.UpdateCaptureState(ctx, "failed_recoverable", calibID, time.Now().UTC())
			}
			return fmt.Errorf("frontier observe: %w", err)
		}

		if transition.AcceptedTile {
			commit := EngineCheckpointCommit{
				Frontier:          frontier.Snapshot(),
				TileID:            tileID,
				TilePath:          tilePath,
				TilePNG:           tilePNG,
				CaptureState:      "capturing",
				ActiveCalibration: calibID,
				UpdatedAt:         time.Now().UTC(),
			}
			if err := session.CommitCaptureStep(ctx, commit); err != nil {
				return fmt.Errorf("commit capture step: %w", err)
			}
			e.reporter.OnProgress(frontier.Snapshot(), tileID)
		}

		currentFrame = nextFrame
	}

	// Final verification
	finalSnap := frontier.Snapshot()
	if err := AuditClosed(finalSnap); err != nil {
		_ = session.UpdateCaptureState(ctx, "failed_recoverable", calibID, time.Now().UTC())
		return fmt.Errorf("final frontier audit failed: %w", err)
	}

	finalCommit := EngineCheckpointCommit{
		Frontier:          finalSnap,
		CaptureState:      "processing",
		ActiveCalibration: calibID,
		UpdatedAt:         time.Now().UTC(),
	}
	if err := session.CommitCaptureStep(ctx, finalCommit); err != nil {
		return fmt.Errorf("commit final closed frontier: %w", err)
	}

	return nil
}

func encodePNG(img image.Image) ([]byte, error) {
	if img == nil {
		return nil, errors.New("nil image")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
