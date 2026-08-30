package pipeline

import (
	"fmt"
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

// PipelineStage represents the discrete execution phases of the pipeline.
type PipelineStage string

const (
	StageIdle        PipelineStage = "idle"
	StagePreflight   PipelineStage = "preflight"
	StageHoming      PipelineStage = "homing"
	StageCalibrating PipelineStage = "calibrating"
	StageCapturing   PipelineStage = "capturing"
	StageAuditing    PipelineStage = "auditing"
	StageRepairing   PipelineStage = "repairing"
	StageOptimizing  PipelineStage = "optimizing"
	StageStitching   PipelineStage = "stitching"
	StagePackaging   PipelineStage = "packaging"
	StageComplete    PipelineStage = "complete"
	StageFailed      PipelineStage = "failed"
	StageCancelled   PipelineStage = "cancelled"
)

// PipelineConfig contains all options and configuration thresholds for the full pipeline.
type PipelineConfig struct {
	// Project storage paths
	ProjectDir  string `json:"project_dir"`
	ArchivePath string `json:"archive_path,omitempty"` // If set, packs into .pimap archive on completion
	ProjectID   string `json:"project_id,omitempty"`
	Title       string `json:"title,omitempty"`
	GameVersion string `json:"game_version,omitempty"`
	Resume      bool   `json:"resume"` // Resume existing workspace if true

	// Subsystem configurations
	SkipPreflight      bool                                `json:"skip_preflight"`
	PreflightConfig    adapter.PreflightConfig             `json:"preflight_config"`
	EngineConfig       capture.EngineConfig                `json:"engine_config"`
	RegistrationConfig registration.PhaseCorrelationConfig `json:"registration_config"`
	QualitySharpness   quality.SharpnessConfig             `json:"quality_sharpness"`
	QualityStability   quality.StabilityConfig             `json:"quality_stability"`
	SolverOptions      optimizer.SolverOptions             `json:"solver_options"`
	StitchOptions      stitch.StitchOptions                `json:"stitch_options"`

	// Target overlap requirements for coverage audit
	MinOverlap float64 `json:"min_overlap"` // default: 0.10
	MaxOverlap float64 `json:"max_overlap"` // default: 0.90
}

// DefaultPipelineConfig returns standard production defaults.
func DefaultPipelineConfig(projectDir string) PipelineConfig {
	return PipelineConfig{
		ProjectDir:         projectDir,
		SkipPreflight:      false,
		PreflightConfig:    adapter.DefaultPreflightConfig(),
		EngineConfig:       capture.DefaultEngineConfig(),
		RegistrationConfig: registration.DefaultConfig(),
		QualitySharpness:   quality.DefaultSharpnessConfig(),
		QualityStability:   quality.DefaultStabilityConfig(),
		SolverOptions:      optimizer.DefaultSolverOptions(),
		StitchOptions:      stitch.DefaultStitchOptions(),
		MinOverlap:         0.05,
		MaxOverlap:         0.95,
	}
}

// ProgressSnapshot contains atomic point-in-time progress data.
type ProgressSnapshot struct {
	Stage          PipelineStage             `json:"stage"`
	CurrentTileID  string                    `json:"current_tile_id,omitempty"`
	DiscoveredRows int                       `json:"discovered_rows"`
	TotalTiles     int                       `json:"total_tiles"`
	ConfirmedEdges capture.ConfirmedEdges    `json:"confirmed_edges"`
	Frontier       *capture.FrontierSnapshot `json:"frontier,omitempty"`
	Message        string                    `json:"message"`
	Percent        float64                   `json:"percent,omitempty"`
}

// ProgressListener receives lifecycle and diagnostic events from the pipeline.
type ProgressListener interface {
	OnStageTransition(from, to PipelineStage)
	OnProgress(snapshot ProgressSnapshot)
	OnTileStatus(tileID string, status string)
	OnLog(level, message string)
	OnWarning(message string)
	OnPreflightReport(report adapter.PreflightReport)
}

// NoopProgressListener provides a default no-op implementation.
type NoopProgressListener struct{}

func (n *NoopProgressListener) OnStageTransition(from, to PipelineStage)         {}
func (n *NoopProgressListener) OnProgress(snapshot ProgressSnapshot)             {}
func (n *NoopProgressListener) OnTileStatus(tileID string, status string)        {}
func (n *NoopProgressListener) OnLog(level, message string)                      {}
func (n *NoopProgressListener) OnWarning(message string)                         {}
func (n *NoopProgressListener) OnPreflightReport(report adapter.PreflightReport) {}

// PipelineResult represents the comprehensive return summary of a pipeline execution.
type PipelineResult struct {
	ProjectID         string                          `json:"project_id"`
	ProjectDir        string                          `json:"project_dir"`
	ArchivePath       string                          `json:"archive_path,omitempty"`
	PreflightReport   *adapter.PreflightReport        `json:"preflight_report,omitempty"`
	CalibrationRecord *capture.CalibrationRecord      `json:"calibration_record,omitempty"`
	FrontierSnapshot  *capture.FrontierSnapshot       `json:"frontier_snapshot,omitempty"`
	CoverageAudit     *project.CoverageAudit          `json:"coverage_audit,omitempty"`
	ResidualStats     *optimizer.ResidualStats        `json:"residual_stats,omitempty"`
	TileCount         int                             `json:"tile_count"`
	Bounds            geometry.Rect                   `json:"bounds"`
	PanoramaPath      string                          `json:"panorama_path,omitempty"`
	PreviewPath       string                          `json:"preview_path,omitempty"`
	Duration          time.Duration                   `json:"duration"`
	StageDurations    map[PipelineStage]time.Duration `json:"stage_durations"`
	FinalStage        PipelineStage                   `json:"final_stage"`
	Success           bool                            `json:"success"`
}

// ErrorClassification categorizes errors for appropriate user/system action.
type ErrorClassification string

const (
	ErrClassPreflightNoGo      ErrorClassification = "preflight_no_go"
	ErrClassUserActionable     ErrorClassification = "user_actionable"
	ErrClassSessionRecoverable ErrorClassification = "session_recoverable"
	ErrClassCorruptProject     ErrorClassification = "corrupt_project"
	ErrClassCancelled          ErrorClassification = "cancelled"
)

// PipelineError encapsulates rich context for operational diagnosis.
type PipelineError struct {
	Stage          PipelineStage       `json:"stage"`
	Classification ErrorClassification `json:"classification"`
	Message        string              `json:"message"`
	Cause          error               `json:"-"`
	Details        map[string]any      `json:"details,omitempty"`
}

func (e *PipelineError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Stage, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Stage, e.Message)
}

func (e *PipelineError) Unwrap() error {
	return e.Cause
}
