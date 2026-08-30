package project

import (
	"encoding/json"
	"time"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/capture"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
)

const (
	FormatName           = "protocol-imaging-project"
	FormatVersion        = 1
	CaptureSchemaVersion = 1
)

type CaptureState string

const (
	CaptureCreated           CaptureState = "created"
	CaptureHoming            CaptureState = "homing"
	CaptureCalibrating       CaptureState = "calibrating"
	CaptureCapturing         CaptureState = "capturing"
	CaptureRepairing         CaptureState = "repairing"
	CaptureProcessing        CaptureState = "processing"
	CaptureComplete          CaptureState = "complete"
	CaptureCancelled         CaptureState = "cancelled"
	CaptureFailedRecoverable CaptureState = "failed_recoverable"
	CaptureFailedCorrupt     CaptureState = "failed_corrupt"
)

func (state CaptureState) Valid() bool {
	switch state {
	case CaptureCreated,
		CaptureHoming,
		CaptureCalibrating,
		CaptureCapturing,
		CaptureRepairing,
		CaptureProcessing,
		CaptureComplete,
		CaptureCancelled,
		CaptureFailedRecoverable,
		CaptureFailedCorrupt:
		return true
	default:
		return false
	}
}

type GeometryStatus string

const (
	GeometryDiscovering   GeometryStatus = "discovering"
	GeometryObservedLocal GeometryStatus = "observed_local"
	GeometryUnresolved    GeometryStatus = "unresolved"
)

func (status GeometryStatus) Valid() bool {
	switch status {
	case GeometryDiscovering, GeometryObservedLocal, GeometryUnresolved:
		return true
	default:
		return false
	}
}

type Manifest struct {
	Format            string             `json:"format"`
	FormatVersion     int                `json:"format_version"`
	ProjectID         string             `json:"project_id"`
	Title             string             `json:"title"`
	CreatedAt         time.Time          `json:"created_at"`
	UpdatedAt         time.Time          `json:"updated_at"`
	GameVersion       string             `json:"game_version"`
	Profile           *ProfileReference  `json:"profile"`
	Geometry          GeometryDescriptor `json:"geometry"`
	CoordinateSystem  CoordinateSystem   `json:"coordinate_system"`
	CaptureState      CaptureState       `json:"capture_state"`
	Capture           string             `json:"capture"`
	ActiveCalibration string             `json:"active_calibration,omitempty"`
	ActivePlan        string             `json:"active_plan,omitempty"`
	ActiveVersion     string             `json:"active_version,omitempty"`
	Layers            []LayerReference   `json:"layers"`
	Annotations       *string            `json:"annotations"`
	Extra             ExtraFields        `json:"-"`
}

type ProfileReference struct {
	ID       string      `json:"id"`
	Snapshot string      `json:"snapshot"`
	Extra    ExtraFields `json:"-"`
}

type GeometryDescriptor struct {
	Status                  GeometryStatus `json:"status"`
	Source                  string         `json:"source"`
	Observation             string         `json:"observation"`
	CoordinateCompatibility string         `json:"coordinate_compatibility"`
	Extra                   ExtraFields    `json:"-"`
}

type CoordinateSystem struct {
	SpaceID string      `json:"space_id"`
	Unit    string      `json:"unit"`
	Axis    string      `json:"axis"`
	Extra   ExtraFields `json:"-"`
}

type LayerReference struct {
	ID    string      `json:"id"`
	Path  string      `json:"path"`
	Extra ExtraFields `json:"-"`
}

type BurstPolicy string

const (
	BurstNone     BurstPolicy = "none"
	BurstSelected BurstPolicy = "selected"
	BurstAll      BurstPolicy = "all"
)

func (policy BurstPolicy) Valid() bool {
	switch policy {
	case BurstNone, BurstSelected, BurstAll:
		return true
	default:
		return false
	}
}

type DiagnosticsPolicy string

const (
	DiagnosticsStandard DiagnosticsPolicy = "standard"
	DiagnosticsFull     DiagnosticsPolicy = "full"
)

func (policy DiagnosticsPolicy) Valid() bool {
	switch policy {
	case DiagnosticsStandard, DiagnosticsFull:
		return true
	default:
		return false
	}
}

type CaptureDocument struct {
	SchemaVersion int                    `json:"schema_version"`
	Request       CaptureRequest         `json:"request"`
	Environment   EnvironmentFingerprint `json:"environment"`
	Limits        capture.SafetyLimits   `json:"limits"`
	Calibrations  []CalibrationRecord    `json:"calibrations"`
	Extra         ExtraFields            `json:"-"`
}

type CaptureRequest struct {
	Version          int               `json:"version"`
	FrozenAt         time.Time         `json:"frozen_at"`
	ImagingMode      string            `json:"imaging_mode"`
	QualityLevel     string            `json:"quality_level"`
	TargetOverlap    Overlap           `json:"target_overlap"`
	BurstPolicy      BurstPolicy       `json:"burst_policy"`
	Diagnostics      DiagnosticsPolicy `json:"diagnostics"`
	GeneratePanorama bool              `json:"generate_panorama"`
	GeneratePyramid  bool              `json:"generate_pyramid"`
	Extra            ExtraFields       `json:"-"`
}

type Overlap struct {
	Horizontal float64     `json:"horizontal"`
	Vertical   float64     `json:"vertical"`
	Extra      ExtraFields `json:"-"`
}

type EnvironmentFingerprint struct {
	ObservedAt     time.Time         `json:"observed_at"`
	ControllerKind string            `json:"controller_kind"`
	RawFrameSize   PixelDimensions   `json:"raw_frame_size"`
	InputViewport  geometry.Size     `json:"input_viewport"`
	EffectiveCrop  geometry.Rect     `json:"effective_crop"`
	DPIScale       float64           `json:"dpi_scale"`
	Window         WindowFingerprint `json:"window"`
	GameVersion    string            `json:"game_version"`
	Extra          ExtraFields       `json:"-"`
}

type PixelDimensions struct {
	Width  int         `json:"width"`
	Height int         `json:"height"`
	Extra  ExtraFields `json:"-"`
}

type WindowFingerprint struct {
	ProcessName string          `json:"process_name"`
	ClassName   string          `json:"class_name"`
	TitleHash   string          `json:"title_hash"`
	ClientSize  PixelDimensions `json:"client_size"`
	Extra       ExtraFields     `json:"-"`
}

type CalibrationRecord struct {
	ID                 string              `json:"id"`
	CreatedAt          time.Time           `json:"created_at"`
	Actions            []CalibrationAction `json:"actions"`
	HorizontalMotion   geometry.Vector     `json:"horizontal_motion"`
	VerticalMotion     geometry.Vector     `json:"vertical_motion"`
	EffectiveViewport  geometry.Rect       `json:"effective_viewport"`
	InputToRaw         geometry.Affine2D   `json:"input_to_raw"`
	RawToSession       geometry.Affine2D   `json:"raw_to_session"`
	Confidence         float64             `json:"confidence"`
	InvalidatedAt      *time.Time          `json:"invalidated_at,omitempty"`
	InvalidationReason string              `json:"invalidation_reason,omitempty"`
	Extra              ExtraFields         `json:"-"`
}

type CalibrationAction struct {
	Purpose          string          `json:"purpose"`
	InputDelta       geometry.Vector `json:"input_delta"`
	MeasuredRawDelta geometry.Vector `json:"measured_raw_delta"`
	EvidenceIDs      []string        `json:"evidence_ids"`
	Extra            ExtraFields     `json:"-"`
}

// ExtraFields retains fields introduced by newer writers. Readers must write
// these values back unchanged unless a known field with the same name exists.
type ExtraFields map[string]json.RawMessage
