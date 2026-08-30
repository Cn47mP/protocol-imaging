package pipeline

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/adapter"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/capture"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/optimizer"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/project"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/quality"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/registration"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/stitch"
)

// Pipeline coordinates the complete protocol imaging capture and stitching lifecycle.
type Pipeline struct {
	config   PipelineConfig
	listener ProgressListener

	mu             sync.Mutex
	currentStage   PipelineStage
	stageStarts    map[PipelineStage]time.Time
	stageDurations map[PipelineStage]time.Duration
}

// NewPipeline creates a new Pipeline instance.
func NewPipeline(config PipelineConfig, listener ProgressListener) *Pipeline {
	if listener == nil {
		listener = &NoopProgressListener{}
	}
	if config.MinOverlap <= 0 {
		config.MinOverlap = 0.10
	}
	if config.MaxOverlap <= 0 {
		config.MaxOverlap = 0.90
	}
	if config.EngineConfig.TileLayerID == "" {
		config.EngineConfig.TileLayerID = "overview"
	}
	return &Pipeline{
		config:         config,
		listener:       listener,
		currentStage:   StageIdle,
		stageStarts:    make(map[PipelineStage]time.Time),
		stageDurations: make(map[PipelineStage]time.Duration),
	}
}

// CurrentStage returns the active stage of the pipeline.
func (p *Pipeline) CurrentStage() PipelineStage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentStage
}

func (p *Pipeline) transitionStage(to PipelineStage) {
	p.mu.Lock()
	from := p.currentStage
	now := time.Now().UTC()
	if from != StageIdle && from != "" {
		if start, ok := p.stageStarts[from]; ok {
			p.stageDurations[from] += now.Sub(start)
		}
	}
	p.currentStage = to
	p.stageStarts[to] = now
	p.mu.Unlock()

	p.listener.OnStageTransition(from, to)
}

func (p *Pipeline) snapshotDurations() map[PipelineStage]time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	res := make(map[PipelineStage]time.Duration, len(p.stageDurations))
	now := time.Now().UTC()
	for k, v := range p.stageDurations {
		res[k] = v
	}
	if p.currentStage != StageIdle && p.currentStage != "" {
		if start, ok := p.stageStarts[p.currentStage]; ok {
			res[p.currentStage] += now.Sub(start)
		}
	}
	return res
}

// Execute runs the full protocol imaging lifecycle.
func (p *Pipeline) Execute(ctx context.Context, ctrl adapter.Controller) (*PipelineResult, error) {
	if ctrl == nil {
		return nil, &PipelineError{
			Stage:          StageIdle,
			Classification: ErrClassUserActionable,
			Message:        "controller is required",
			Cause:          errors.New("nil controller"),
		}
	}

	startTime := time.Now().UTC()

	// Stop dispatching new operations on cancellation, then leave the borrowed
	// Maa controller inactive after any already-running atomic job has returned.
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_ = ctrl.Release(releaseCtx)
	}()

	var preflightReport *adapter.PreflightReport
	var activeCalibRecord *capture.CalibrationRecord

	// 1. Stage: Preflight
	if !p.config.SkipPreflight {
		p.transitionStage(StagePreflight)
		reporterAdapter := &preflightReporterBridge{listener: p.listener}
		report, err := adapter.RunPreflightProbes(ctx, ctrl, reporterAdapter, p.config.PreflightConfig)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				p.transitionStage(StageCancelled)
				return nil, &PipelineError{
					Stage:          StageCancelled,
					Classification: ErrClassCancelled,
					Message:        "preflight cancelled",
					Cause:          err,
				}
			}
			p.transitionStage(StageFailed)
			return nil, &PipelineError{
				Stage:          StagePreflight,
				Classification: ErrClassPreflightNoGo,
				Message:        "preflight probe execution failed",
				Cause:          err,
			}
		}
		preflightReport = &report
		p.listener.OnPreflightReport(report)

		if report.Verdict != adapter.ProbeStatusGo || !report.OverallPass {
			if ctx.Err() != nil {
				p.transitionStage(StageCancelled)
				return nil, &PipelineError{
					Stage:          StageCancelled,
					Classification: ErrClassCancelled,
					Message:        "preflight cancelled",
					Cause:          ctx.Err(),
				}
			}
			p.transitionStage(StageFailed)
			return nil, &PipelineError{
				Stage:          StagePreflight,
				Classification: ErrClassPreflightNoGo,
				Message:        "preflight capability gate not passed",
				Cause:          fmt.Errorf("verdict %s (overall pass: %v)", report.Verdict, report.OverallPass),
			}
		}
		if report.Calibration != nil {
			activeCalibRecord = report.Calibration
		}
	}

	// 2. Open or Create Project Session
	var store *project.Store
	var session *project.Session
	var err error

	if p.config.Resume {
		store, err = project.NewStore(p.config.ProjectDir)
		if err != nil {
			p.transitionStage(StageFailed)
			return nil, &PipelineError{
				Stage:          StageCapturing,
				Classification: ErrClassSessionRecoverable,
				Message:        "open project store for resume",
				Cause:          err,
			}
		}
		session, err = store.Resume(ctx)
		if err != nil {
			p.transitionStage(StageFailed)
			return nil, &PipelineError{
				Stage:          StageCapturing,
				Classification: ErrClassCorruptProject,
				Message:        "resume project session",
				Cause:          err,
			}
		}
	} else {
		store, session, activeCalibRecord, err = p.initNewProject(ctx, ctrl, activeCalibRecord)
		if err != nil {
			p.transitionStage(StageFailed)
			return nil, &PipelineError{
				Stage:          StageHoming,
				Classification: ErrClassUserActionable,
				Message:        "initialize project session",
				Cause:          err,
			}
		}
	}

	// Active Calibration ID
	calibID := ""
	if len(session.CaptureDocument().Calibrations) > 0 {
		calibID = session.CaptureDocument().Calibrations[0].ID
	}
	if calibID == "" && activeCalibRecord != nil {
		calibID = activeCalibRecord.ID
	}
	if calibID == "" {
		calibID = "calib-default-001"
	}

	sessionAdapter := &projectSessionAdapter{
		session:  session,
		pipeline: p,
		calibID:  calibID,
	}

	// 3. Configure Capture Engine & Observer
	estimator := &cameraMotionEstimator{config: p.config.RegistrationConfig}
	observer := capture.NewMotionObserver(
		p.config.EngineConfig.FrontierConfig,
		estimator,
	)

	reporter := &pipelineReporterBridge{
		pipeline: p,
		listener: p.listener,
	}

	engine, err := capture.NewEngine(p.config.EngineConfig, ctrl, observer, reporter)
	if err != nil {
		p.transitionStage(StageFailed)
		return nil, &PipelineError{
			Stage:          StageCapturing,
			Classification: ErrClassGeneral,
			Message:        "initialize capture engine",
			Cause:          err,
		}
	}

	// 4. Execution: Resume or Full Run
	if p.config.Resume {
		if err := p.resumeExecution(ctx, session, sessionAdapter, engine, ctrl); err != nil {
			if isContextCancelled(ctx, err) {
				p.transitionStage(StageCancelled)
				_ = session.UpdateCaptureState(context.Background(), project.CaptureCancelled, calibID, time.Now().UTC())
				cErr := err
				if !errors.Is(cErr, context.Canceled) && !errors.Is(cErr, context.DeadlineExceeded) && ctx.Err() != nil {
					cErr = ctx.Err()
				}
				return nil, &PipelineError{
					Stage:          StageCancelled,
					Classification: ErrClassCancelled,
					Message:        "capture cancelled",
					Cause:          cErr,
				}
			}
			p.transitionStage(StageFailed)
			return nil, &PipelineError{
				Stage:          p.CurrentStage(),
				Classification: ErrClassSessionRecoverable,
				Message:        "capture resume execution failed",
				Cause:          err,
			}
		}
	} else {
		// Run full capture lifecycle (Homing -> Calibrating -> Capturing)
		if err := engine.Run(ctx, sessionAdapter); err != nil {
			if isContextCancelled(ctx, err) {
				p.transitionStage(StageCancelled)
				_ = session.UpdateCaptureState(context.Background(), project.CaptureCancelled, calibID, time.Now().UTC())
				cErr := err
				if !errors.Is(cErr, context.Canceled) && !errors.Is(cErr, context.DeadlineExceeded) && ctx.Err() != nil {
					cErr = ctx.Err()
				}
				return nil, &PipelineError{
					Stage:          StageCancelled,
					Classification: ErrClassCancelled,
					Message:        "capture cancelled",
					Cause:          cErr,
				}
			}
			p.transitionStage(StageFailed)
			return nil, &PipelineError{
				Stage:          p.CurrentStage(),
				Classification: ErrClassSessionRecoverable,
				Message:        "capture engine execution failed",
				Cause:          err,
			}
		}
	}

	// 5. Stage: Auditing
	p.transitionStage(StageAuditing)
	activePlan := session.ActivePlan()
	if activePlan == nil {
		p.transitionStage(StageFailed)
		return nil, &PipelineError{
			Stage:          StageAuditing,
			Classification: ErrClassCorruptProject,
			Message:        "missing active plan after capture",
			Cause:          errors.New("active plan is nil"),
		}
	}

	layerID := p.config.EngineConfig.TileLayerID
	if layerID == "" {
		layerID = "overview"
	}

	auditConfig := quality.AuditConfig{
		RequiredAdjacencies: activePlan.RequiredAdjacencies,
		RequiredOverlap: project.OverlapRange{
			Minimum: p.config.MinOverlap,
			Maximum: p.config.MaxOverlap,
		},
		MinConfidence: 0.6,
		TileSize: geometry.Size{
			Width:  float64(session.CaptureDocument().Environment.RawFrameSize.Width),
			Height: float64(session.CaptureDocument().Environment.RawFrameSize.Height),
		},
		TileFileChecker: func(tileID string) bool {
			expectedChecksum, exists := activePlan.TileChecksums[tileID]
			if !exists || len(expectedChecksum) != sha256.Size*2 {
				return false
			}
			tileRelPath := fmt.Sprintf("layers/%s/tiles/%s.png", layerID, tileID)
			tileAbsPath := filepath.Join(session.Root(), tileRelPath)
			info, err := os.Stat(tileAbsPath)
			if err != nil || info.Size() == 0 {
				return false
			}
			f, err := os.Open(tileAbsPath)
			if err != nil {
				return false
			}
			defer f.Close()
			h := sha256.New()
			if _, err := io.Copy(h, f); err != nil {
				return false
			}
			return hex.EncodeToString(h.Sum(nil)) == expectedChecksum
		},
	}

	captureAudit, err := quality.AuditPlanCoverage(*activePlan, auditConfig)
	if err != nil || !captureAudit.Passed {
		p.transitionStage(StageFailed)
		_ = session.UpdateCaptureState(context.Background(), project.CaptureFailedRecoverable, calibID, time.Now().UTC())
		return nil, &PipelineError{
			Stage:          StageAuditing,
			Classification: ErrClassSessionRecoverable,
			Message:        fmt.Sprintf("capture completeness audit failed (missing tiles: %v, adjacencies: %d)", captureAudit.MissingTileIDs, len(captureAudit.VerifiedAdjacencies)),
			Cause:          err,
		}
	}

	// 6. Stages: Optimizing & Stitching
	residualStats, stitchResult, panoPath, prevPath, audit, err := p.runOptimizationAndStitch(ctx, session, activePlan, captureAudit)
	if err != nil {
		if isContextCancelled(ctx, err) {
			p.transitionStage(StageCancelled)
			_ = session.UpdateCaptureState(context.Background(), project.CaptureCancelled, calibID, time.Now().UTC())
			cErr := err
			if !errors.Is(cErr, context.Canceled) && !errors.Is(cErr, context.DeadlineExceeded) && ctx.Err() != nil {
				cErr = ctx.Err()
			}
			return nil, &PipelineError{
				Stage:          StageCancelled,
				Classification: ErrClassCancelled,
				Message:        "optimization / stitching cancelled",
				Cause:          cErr,
			}
		}
		p.transitionStage(StageFailed)
		return nil, &PipelineError{
			Stage:          p.CurrentStage(),
			Classification: ErrClassGeneral,
			Message:        "optimization or stitching failed",
			Cause:          err,
		}
	}

	// Promote session state to observed_local and complete
	if err := p.finalizeSession(ctx, session, activePlan, audit); err != nil {
		p.transitionStage(StageFailed)
		return nil, &PipelineError{
			Stage:          StageStitching,
			Classification: ErrClassCorruptProject,
			Message:        "finalize session manifest",
			Cause:          err,
		}
	}

	// 7. Stage: Packaging
	archivePath := p.config.ArchivePath
	if archivePath != "" {
		p.transitionStage(StagePackaging)
		if err := project.Pack(session.Root(), archivePath); err != nil {
			p.transitionStage(StageFailed)
			return nil, &PipelineError{
				Stage:          StagePackaging,
				Classification: ErrClassUserActionable,
				Message:        "package .pimap container",
				Cause:          err,
			}
		}
	}

	// 8. Stage: Complete
	p.transitionStage(StageComplete)
	totalDuration := time.Since(startTime)
	frontierSnap := activePlan.Frontier

	result := &PipelineResult{
		ProjectID:         session.Manifest().ProjectID,
		ProjectDir:        session.Root(),
		ArchivePath:       archivePath,
		PreflightReport:   preflightReport,
		CalibrationRecord: activeCalibRecord,
		FrontierSnapshot:  &frontierSnap,
		CoverageAudit:     &audit,
		ResidualStats:     residualStats,
		TileCount:         len(frontierSnap.Tiles),
		Bounds:            stitchResult.Bounds,
		PanoramaPath:      panoPath,
		PreviewPath:       prevPath,
		Duration:          totalDuration,
		StageDurations:    p.snapshotDurations(),
		FinalStage:        StageComplete,
		Success:           true,
	}

	return result, nil
}

func (p *Pipeline) initNewProject(ctx context.Context, ctrl adapter.Controller, preflightCalib *capture.CalibrationRecord) (*project.Store, *project.Session, *capture.CalibrationRecord, error) {
	now := time.Now().UTC()

	vp, err := ctrl.InputViewport(ctx)
	if err != nil {
		vp = adapter.PixelSize{Width: 1920, Height: 1080}
	}
	rawFrame, err := ctrl.CaptureRaw(ctx)
	if err != nil {
		rawFrame = adapter.Frame{
			Size: adapter.PixelSize{Width: vp.Width, Height: vp.Height},
		}
	}

	rawWidth := rawFrame.Size.Width
	rawHeight := rawFrame.Size.Height
	if rawWidth <= 0 {
		rawWidth = vp.Width
	}
	if rawHeight <= 0 {
		rawHeight = vp.Height
	}

	rawSize := project.PixelDimensions{Width: rawWidth, Height: rawHeight}

	calibRecord := preflightCalib
	if calibRecord == nil {
		calibRecord = &capture.CalibrationRecord{
			ID:        fmt.Sprintf("calib-%s", now.Format("20060102-150405")),
			CreatedAt: now,
			Actions: []capture.CalibrationAction{
				{
					Purpose:          "initial_calibration",
					InputDelta:       geometry.Vector{X: 100, Y: 0},
					MeasuredRawDelta: geometry.Vector{X: 100, Y: 0},
					EvidenceIDs:      []string{"init-h"},
				},
				{
					Purpose:          "initial_calibration",
					InputDelta:       geometry.Vector{X: 0, Y: 100},
					MeasuredRawDelta: geometry.Vector{X: 0, Y: 100},
					EvidenceIDs:      []string{"init-v"},
				},
			},
			HorizontalMotion: geometry.Vector{X: 100, Y: 0},
			VerticalMotion:   geometry.Vector{X: 0, Y: 100},
			EffectiveViewport: geometry.Rect{
				X:      0,
				Y:      0,
				Width:  float64(rawWidth),
				Height: float64(rawHeight),
			},
			InputToRaw:   geometry.IdentityAffine2D(),
			RawToSession: geometry.IdentityAffine2D(),
			Confidence:   1.0,
		}
	}

	projCalib := convertToProjectCalibration(*calibRecord, rawSize)

	projectID := p.config.ProjectID
	if projectID == "" {
		randBytes := make([]byte, 4)
		_, _ = rand.Read(randBytes)
		projectID = fmt.Sprintf("project-%s-%s", now.Format("20060102-150405"), hex.EncodeToString(randBytes))
	}

	title := p.config.Title
	if title == "" {
		title = "Protocol Imaging Project"
	}
	gameVersion := p.config.GameVersion
	if gameVersion == "" {
		gameVersion = "unknown"
	}

	layerID := p.config.EngineConfig.TileLayerID
	if layerID == "" {
		layerID = "overview"
	}

	manifest := project.Manifest{
		Format:        project.FormatName,
		FormatVersion: project.FormatVersion,
		ProjectID:     projectID,
		Title:         title,
		CreatedAt:     now,
		UpdatedAt:     now,
		GameVersion:   gameVersion,
		Geometry: project.GeometryDescriptor{
			Status:                  project.GeometryDiscovering,
			Source:                  "dynamic_boundary_observation",
			Observation:             "boundary.json",
			CoordinateCompatibility: "session_local",
		},
		CoordinateSystem: project.CoordinateSystem{
			SpaceID: "session:" + projectID,
			Unit:    "reference_layer_pixel",
			Axis:    "x_right_y_down",
		},
		CaptureState: project.CaptureCreated,
		Capture:      "capture.json",
		Layers: []project.LayerReference{
			{
				ID:   layerID,
				Path: "layers/" + layerID,
			},
		},
	}

	captureDoc := project.CaptureDocument{
		SchemaVersion: project.CaptureSchemaVersion,
		Request: project.CaptureRequest{
			Version:      1,
			FrozenAt:     now,
			ImagingMode:  "full_grid",
			QualityLevel: "lossless",
			TargetOverlap: project.Overlap{
				Horizontal: p.config.EngineConfig.TargetOverlap.Width,
				Vertical:   p.config.EngineConfig.TargetOverlap.Height,
			},
			BurstPolicy:      project.BurstNone,
			Diagnostics:      project.DiagnosticsStandard,
			GeneratePanorama: p.config.StitchOptions.GeneratePanorama,
			GeneratePyramid:  false,
		},
		Environment: project.EnvironmentFingerprint{
			ObservedAt:     now,
			ControllerKind: "adapter",
			RawFrameSize:   rawSize,
			InputViewport: geometry.Size{
				Width:  float64(vp.Width),
				Height: float64(vp.Height),
			},
			EffectiveCrop: geometry.Rect{
				X:      0,
				Y:      0,
				Width:  float64(rawWidth),
				Height: float64(rawHeight),
			},
			DPIScale: 1.0,
			Window: project.WindowFingerprint{
				ProcessName: "game",
				ClassName:   "UnityWndClass",
				TitleHash:   "hash",
				ClientSize:  project.PixelDimensions{Width: vp.Width, Height: vp.Height},
			},
			GameVersion: gameVersion,
		},
		Limits:       p.config.EngineConfig.FrontierConfig.Limits,
		Calibrations: []project.CalibrationRecord{projCalib},
	}

	boundaryDoc := project.NewInitialBoundary()

	store, err := project.NewStore(p.config.ProjectDir)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create store: %w", err)
	}

	session, err := store.Create(ctx, manifest, captureDoc, boundaryDoc)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create session: %w", err)
	}

	return store, session, calibRecord, nil
}

func (p *Pipeline) runOptimizationAndStitch(ctx context.Context, session *project.Session, activePlan *project.PlanDocument, captureAudit project.CoverageAudit) (*optimizer.ResidualStats, *stitch.StitchResult, string, string, project.CoverageAudit, error) {
	p.transitionStage(StageOptimizing)
	frontier := activePlan.Frontier

	graph := optimizer.NewPoseGraph()
	for _, tile := range frontier.Tiles {
		graph.AddNode(tile.ID, tile.Position)
	}
	if len(frontier.Tiles) > 0 {
		_ = graph.SetAnchor(frontier.Tiles[0].ID)
	}

	tileImages := make(map[string]image.Image, len(frontier.Tiles))
	layerID := p.config.EngineConfig.TileLayerID
	if layerID == "" {
		layerID = "overview"
	}

	for _, tile := range frontier.Tiles {
		if err := ctx.Err(); err != nil {
			return nil, nil, "", "", project.CoverageAudit{}, err
		}
		tileRelPath := fmt.Sprintf("layers/%s/tiles/%s.png", layerID, tile.ID)
		absTilePath := filepath.Join(session.Root(), filepath.FromSlash(tileRelPath))
		file, err := os.Open(absTilePath)
		if err != nil {
			return nil, nil, "", "", project.CoverageAudit{}, fmt.Errorf("open tile image %s: %w", tile.ID, err)
		}
		img, err := png.Decode(file)
		_ = file.Close()
		if err != nil {
			return nil, nil, "", "", project.CoverageAudit{}, fmt.Errorf("decode tile png %s: %w", tile.ID, err)
		}
		tileImages[tile.ID] = img
	}

	requiredAdjacencies := activePlan.RequiredAdjacencies
	if len(requiredAdjacencies) == 0 {
		requiredAdjacencies = project.RequiredAdjacenciesFromFrontier(frontier)
	}
	evidence := make([]project.AdjacencyEvidence, 0, len(requiredAdjacencies))
	evidenceIndex := make(map[string]int, len(requiredAdjacencies))
	for _, adj := range requiredAdjacencies {
		if err := ctx.Err(); err != nil {
			return nil, nil, "", "", project.CoverageAudit{}, err
		}
		imgA, okA := tileImages[adj.FromTile]
		imgB, okB := tileImages[adj.ToTile]
		record := project.AdjacencyEvidence{
			FromTile: adj.FromTile, ToTile: adj.ToTile, Axis: adj.Axis,
			Rejected: true, EvidenceFrameIDs: []string{adj.FromTile, adj.ToTile},
		}
		key := pipelineAdjacencyKey(adj.FromTile, adj.ToTile, adj.Axis)
		evidenceIndex[key] = len(evidence)
		if !okA || !okB {
			evidence = append(evidence, record)
			continue
		}

		tileA := findTile(frontier.Tiles, adj.FromTile)
		tileB := findTile(frontier.Tiles, adj.ToTile)
		if tileA == nil || tileB == nil {
			evidence = append(evidence, record)
			continue
		}
		overlapA, overlapB, cropErr := cropPredictedOverlap(imgA, imgB, tileA.Position, tileB.Position)
		if cropErr != nil {
			evidence = append(evidence, record)
			continue
		}
		nominalDelta := tileB.Position.Sub(tileA.Position)
		regRes, err := registration.ComputePhaseCorrelation(overlapA, overlapB, p.config.RegistrationConfig)
		relDelta := geometry.Vector{}
		selected := false
		if err == nil && regRes.Confidence >= p.config.RegistrationConfig.MinConfidence {
			relDelta = geometry.Vector{X: nominalDelta.X - regRes.Delta.X, Y: nominalDelta.Y - regRes.Delta.Y}
			record.Confidence = regRes.Confidence
			selected = true
		}
		if !selected || vectorDistance(relDelta, nominalDelta) > 8 {
			nccRes, nccErr := registration.MatchTemplateNCCWindow(overlapA, overlapB, geometry.Vector{}, 12, 64)
			if nccErr == nil && nccRes.Confidence >= p.config.RegistrationConfig.MinConfidence {
				nccDelta := geometry.Vector{X: nominalDelta.X - nccRes.Delta.X, Y: nominalDelta.Y - nccRes.Delta.Y}
				if !selected || vectorDistance(nccDelta, nominalDelta) < vectorDistance(relDelta, nominalDelta) {
					relDelta = nccDelta
					record.Confidence = nccRes.Confidence
					selected = true
				}
			}
		}
		if !selected || vectorDistance(relDelta, nominalDelta) > 8 {
			fullRes, fullErr := registration.ComputePhaseCorrelation(imgA, imgB, p.config.RegistrationConfig)
			if fullErr == nil && fullRes.Confidence >= p.config.RegistrationConfig.MinConfidence {
				fullDelta := geometry.Vector{X: -fullRes.Delta.X, Y: -fullRes.Delta.Y}
				if !selected || vectorDistance(fullDelta, nominalDelta) < vectorDistance(relDelta, nominalDelta) {
					relDelta = fullDelta
					record.Confidence = fullRes.Confidence
					selected = true
				}
			}
		}
		if selected {
			weight := math.Max(0.1, record.Confidence*10.0)
			record.Translation = relDelta
			record.Rejected = false
			_ = graph.AddEdgeDetailed(optimizer.Edge{
				ID: key, FromNode: adj.FromTile, ToNode: adj.ToTile,
				Translation: relDelta, Weight: weight, CurrentWeight: weight,
				Type: optimizer.EdgeTypeMeasured, Confidence: record.Confidence,
			})
		}
		evidence = append(evidence, record)
	}

	optimizedPoses, stats, err := graph.SolveWithOptions(p.config.SolverOptions)
	if err != nil {
		return nil, nil, "", "", project.CoverageAudit{}, fmt.Errorf("solve measured pose graph: %w", err)
	}
	for _, edge := range graph.GetEdges() {
		index, exists := evidenceIndex[edge.ID]
		if !exists {
			continue
		}
		evidence[index].Residual = edge.Residual
		evidence[index].Rejected = edge.Rejected
	}

	rawSize := session.CaptureDocument().Environment.RawFrameSize
	maxResidual := p.config.SolverOptions.OutlierThreshold
	if maxResidual <= 0 {
		maxResidual = 12
	}
	measuredByKey := make(map[string]project.AdjacencyEvidence, len(evidence))
	for _, item := range evidence {
		measuredByKey[pipelineAdjacencyKey(item.FromTile, item.ToTile, item.Axis)] = item
	}
	geometryAudit, auditErr := quality.AuditPlanCoverage(*activePlan, quality.AuditConfig{
		RequiredAdjacencies: requiredAdjacencies,
		RequiredOverlap: project.OverlapRange{
			Minimum: p.config.MinOverlap,
			Maximum: p.config.MaxOverlap,
		},
		TileSize:      geometry.Size{Width: float64(rawSize.Width), Height: float64(rawSize.Height)},
		MinConfidence: p.config.RegistrationConfig.MinConfidence,
		ConstraintEvaluator: func(from, to, axis string) (float64, bool) {
			item, exists := measuredByKey[pipelineAdjacencyKey(from, to, axis)]
			return item.Confidence, exists && !item.Rejected && item.Residual <= maxResidual
		},
	})
	if auditErr != nil {
		return &stats, nil, "", "", geometryAudit, fmt.Errorf("measured geometric coverage audit failed: %w", auditErr)
	}
	if !geometryAudit.Passed {
		return &stats, nil, "", "", geometryAudit, fmt.Errorf(
			"measured geometric coverage audit failed: verified %d of %d required adjacencies; rejected evidence: %v",
			len(geometryAudit.VerifiedAdjacencies), len(requiredAdjacencies), rejectedEvidence(evidence),
		)
	}
	poses := make([]project.TilePose, 0, len(optimizedPoses))
	for tileID, position := range optimizedPoses {
		poses = append(poses, project.TilePose{TileID: tileID, Position: position})
	}
	if err := session.WritePoseDocument(ctx, project.PoseDocument{
		SchemaVersion: project.PoseSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Poses:         poses,
		Constraints:   evidence,
	}); err != nil {
		return &stats, nil, "", "", geometryAudit, err
	}

	p.transitionStage(StageStitching)
	tileSize := geometry.Size{Width: float64(rawSize.Width), Height: float64(rawSize.Height)}

	stitchTiles := make([]stitch.StitchTile, 0, len(frontier.Tiles))
	for _, tile := range frontier.Tiles {
		optPose, exists := optimizedPoses[tile.ID]
		if !exists {
			optPose = tile.Position
		}
		stitchTiles = append(stitchTiles, stitch.StitchTile{
			ID:    tile.ID,
			Row:   tile.Row,
			Col:   tile.Sequence,
			Image: tileImages[tile.ID],
			Pose:  optPose,
			Size:  tileSize,
		})
	}

	stitcher := stitch.NewStitcher(p.config.StitchOptions)
	stitchRes, err := stitcher.Stitch(ctx, stitchTiles)
	if err != nil {
		return nil, nil, "", "", geometryAudit, fmt.Errorf("stitch canvas: %w", err)
	}

	exportDir := filepath.Join(session.Root(), "layers", layerID)
	panoPath, prevPath, err := stitcher.ExportFiles(stitchRes, exportDir)
	if err != nil {
		return nil, nil, "", "", geometryAudit, fmt.Errorf("export stitched files: %w", err)
	}

	_ = captureAudit
	return &stats, stitchRes, panoPath, prevPath, geometryAudit, nil
}

func pipelineAdjacencyKey(from, to, axis string) string {
	if from > to {
		from, to = to, from
	}
	return from + "\x00" + to + "\x00" + axis
}

func rejectedEvidence(evidence []project.AdjacencyEvidence) []string {
	result := make([]string, 0)
	for _, item := range evidence {
		if item.Rejected {
			result = append(result, fmt.Sprintf("%s<->%s/%s(conf=%.3f,res=%.3f)", item.FromTile, item.ToTile, item.Axis, item.Confidence, item.Residual))
		}
	}
	return result
}

func cropPredictedOverlap(imgA, imgB image.Image, poseA, poseB geometry.Point) (image.Image, image.Image, error) {
	widthA, heightA := float64(imgA.Bounds().Dx()), float64(imgA.Bounds().Dy())
	widthB, heightB := float64(imgB.Bounds().Dx()), float64(imgB.Bounds().Dy())
	left := math.Max(poseA.X, poseB.X)
	top := math.Max(poseA.Y, poseB.Y)
	right := math.Min(poseA.X+widthA, poseB.X+widthB)
	bottom := math.Min(poseA.Y+heightA, poseB.Y+heightB)
	width := int(math.Floor(right - left))
	height := int(math.Floor(bottom - top))
	if width < 16 || height < 16 {
		return nil, nil, errors.New("predicted tile overlap is too small for registration")
	}
	crop := func(src image.Image, pose geometry.Point) image.Image {
		x := int(math.Round(left - pose.X))
		y := int(math.Round(top - pose.Y))
		dst := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.Draw(dst, dst.Bounds(), src, image.Pt(src.Bounds().Min.X+x, src.Bounds().Min.Y+y), draw.Src)
		return dst
	}
	return crop(imgA, poseA), crop(imgB, poseB), nil
}

func vectorDistance(a, b geometry.Vector) float64 {
	return math.Hypot(a.X-b.X, a.Y-b.Y)
}

func (p *Pipeline) finalizeSession(ctx context.Context, session *project.Session, activePlan *project.PlanDocument, audit project.CoverageAudit) error {
	boundary, err := project.NewBoundaryFromFrontier(activePlan.Frontier, 1.0)
	if err != nil {
		return fmt.Errorf("create final boundary: %w", err)
	}
	boundary.Status = project.GeometryObservedLocal

	plan := *activePlan
	maxIndex := plan.Frontier.Revision
	if entries, err := os.ReadDir(filepath.Join(session.Root(), "plans")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			var idx int
			if n, _ := fmt.Sscanf(e.Name(), "plan-%d.json", &idx); n == 1 {
				if idx > maxIndex {
					maxIndex = idx
				}
			}
		}
	}
	nextIndex := maxIndex + 1
	for {
		candidate := fmt.Sprintf("plan-%04d", nextIndex)
		if _, err := os.Stat(filepath.Join(session.Root(), "plans", candidate+".json")); errors.Is(err, os.ErrNotExist) {
			plan.ID = candidate
			break
		}
		nextIndex++
	}

	plan.CreatedAt = time.Now().UTC()
	plan.Trigger = "coverage_audit_passed"
	plan.CoverageAudit = &audit
	plan.Supersedes = project.PlanArchivePath(activePlan.ID)

	calibID := ""
	if len(session.CaptureDocument().Calibrations) > 0 {
		calibID = session.CaptureDocument().Calibrations[0].ID
	}

	return session.CommitCheckpoint(ctx, project.CheckpointCommit{
		Plan:              plan,
		Boundary:          boundary,
		Tiles:             nil,
		CaptureState:      project.CaptureComplete,
		ActiveCalibration: calibID,
		ActiveVersion:     "v1",
		UpdatedAt:         time.Now().UTC(),
	})
}

func findTile(tiles []capture.TilePlacement, id string) *capture.TilePlacement {
	for i := range tiles {
		if tiles[i].ID == id {
			return &tiles[i]
		}
	}
	return nil
}

func convertToProjectCalibration(c capture.CalibrationRecord, rawSize project.PixelDimensions) project.CalibrationRecord {
	actions := make([]project.CalibrationAction, len(c.Actions))
	for i, a := range c.Actions {
		actions[i] = project.CalibrationAction{
			Purpose:          a.Purpose,
			InputDelta:       a.InputDelta,
			MeasuredRawDelta: a.MeasuredRawDelta,
			EvidenceIDs:      a.EvidenceIDs,
		}
	}
	if len(actions) == 0 {
		actions = []project.CalibrationAction{
			{
				Purpose:          "horizontal_probe",
				InputDelta:       geometry.Vector{X: 100, Y: 0},
				MeasuredRawDelta: geometry.Vector{X: 100, Y: 0},
				EvidenceIDs:      []string{"calib-h"},
			},
			{
				Purpose:          "vertical_probe",
				InputDelta:       geometry.Vector{X: 0, Y: 100},
				MeasuredRawDelta: geometry.Vector{X: 0, Y: 100},
				EvidenceIDs:      []string{"calib-v"},
			},
		}
	}
	hMotion := c.HorizontalMotion
	if hMotion.Length() == 0 {
		hMotion = geometry.Vector{X: 100, Y: 0}
	}
	vMotion := c.VerticalMotion
	if vMotion.Length() == 0 {
		vMotion = geometry.Vector{X: 0, Y: 100}
	}
	vp := c.EffectiveViewport
	if vp.Width == 0 || vp.Height == 0 {
		vp = geometry.Rect{X: 0, Y: 0, Width: float64(rawSize.Width), Height: float64(rawSize.Height)}
	}
	conf := c.Confidence
	if conf <= 0 || conf > 1 {
		conf = 0.95
	}
	id := c.ID
	if id == "" {
		id = fmt.Sprintf("calib-%s", time.Now().UTC().Format("20060102-150405"))
	}
	createdAt := c.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	return project.CalibrationRecord{
		ID:                id,
		CreatedAt:         createdAt,
		Actions:           actions,
		HorizontalMotion:  hMotion,
		VerticalMotion:    vMotion,
		EffectiveViewport: vp,
		InputToRaw:        c.InputToRaw,
		RawToSession:      c.RawToSession,
		Confidence:        conf,
	}
}

// Internal adapter bridging capture.ProjectSession to project.Session
type projectSessionAdapter struct {
	session  *project.Session
	pipeline *Pipeline
	calibID  string
}

func (a *projectSessionAdapter) Root() string {
	return a.session.Root()
}

func (a *projectSessionAdapter) UpdateCaptureState(ctx context.Context, state string, activeCalibration string, updatedAt time.Time) error {
	cs := project.CaptureState(state)
	switch cs {
	case project.CaptureHoming:
		a.pipeline.transitionStage(StageHoming)
	case project.CaptureCalibrating:
		a.pipeline.transitionStage(StageCalibrating)
	case project.CaptureCapturing:
		a.pipeline.transitionStage(StageCapturing)
	case project.CaptureRepairing:
		a.pipeline.transitionStage(StageRepairing)
	case project.CaptureProcessing:
		a.pipeline.transitionStage(StageOptimizing)
	}

	calib := activeCalibration
	if calib == "" && (cs == project.CaptureCapturing || cs == project.CaptureRepairing || cs == project.CaptureProcessing || cs == project.CaptureComplete) {
		calib = a.calibID
	} else if calib != "" && a.calibID != "" {
		calib = a.calibID
	}
	return a.session.UpdateCaptureState(ctx, cs, calib, updatedAt)
}

func (a *projectSessionAdapter) CommitCaptureStep(ctx context.Context, commit capture.EngineCheckpointCommit) error {
	if a.pipeline.CurrentStage() != StageCapturing && (commit.CaptureState == "capturing" || commit.CaptureState == string(project.CaptureCapturing)) {
		a.pipeline.transitionStage(StageCapturing)
	}
	if a.calibID != "" {
		commit.ActiveCalibration = a.calibID
	}
	return a.session.CommitCaptureStep(ctx, commit)
}

type pipelineReporterBridge struct {
	pipeline *Pipeline
	listener ProgressListener
}

func (r *pipelineReporterBridge) OnProgress(snapshot capture.FrontierSnapshot, tileID string) {
	r.listener.OnProgress(ProgressSnapshot{
		Stage:          r.pipeline.CurrentStage(),
		CurrentTileID:  tileID,
		DiscoveredRows: len(snapshot.Rows),
		TotalTiles:     len(snapshot.Tiles),
		ConfirmedEdges: snapshot.ConfirmedEdges,
		Frontier:       &snapshot,
		Message:        fmt.Sprintf("Tile %s captured", tileID),
	})
	r.listener.OnTileStatus(tileID, "captured")
}

func (r *pipelineReporterBridge) OnLog(level, message string) {
	r.listener.OnLog(level, message)
}

type preflightReporterBridge struct {
	listener ProgressListener
}

func (b *preflightReporterBridge) Phase(name string) {
	b.listener.OnLog("INFO", fmt.Sprintf("Preflight phase: %s", name))
}

func (b *preflightReporterBridge) Progress(current, total int, msg string) {
	b.listener.OnProgress(ProgressSnapshot{
		Stage:   StagePreflight,
		Message: fmt.Sprintf("[%d/%d] %s", current, total, msg),
		Percent: float64(current) / float64(total) * 100,
	})
}

func (b *preflightReporterBridge) TileStatus(id string, status string) {
	b.listener.OnTileStatus(id, status)
}

func (b *preflightReporterBridge) Warning(msg string) {
	b.listener.OnWarning(msg)
}

type cameraMotionEstimator struct {
	config registration.PhaseCorrelationConfig
}

func (e *cameraMotionEstimator) EstimateTranslation(before, after image.Image) (geometry.Vector, float64, error) {
	res, err := registration.ComputePhaseCorrelation(before, after, e.config)
	if err != nil {
		return geometry.Vector{}, 0, err
	}
	// Image feature shift (dx, dy) corresponds to camera displacement (-dx, -dy)
	return geometry.Vector{X: -res.Delta.X, Y: -res.Delta.Y}, res.Confidence, nil
}

const ErrClassGeneral ErrorClassification = "general"

func isContextCancelled(ctx context.Context, err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	return false
}
