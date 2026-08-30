package capture

import (
	"errors"
	"fmt"
	"math"

	"github.com/Cn47mP/protocol-imaging/core/geometry"
)

const FrontierSchemaVersion = 1

var (
	ErrComplete     = errors.New("dynamic frontier is complete")
	ErrUnresolved   = errors.New("dynamic frontier is unresolved")
	ErrSafetyLimit  = errors.New("dynamic frontier reached a safety limit")
	ErrInvalidState = errors.New("invalid dynamic frontier state")
)

type Direction string

const (
	DirectionLeft  Direction = "left"
	DirectionRight Direction = "right"
	DirectionDown  Direction = "down"
)

func (d Direction) Valid() bool {
	switch d {
	case DirectionLeft, DirectionRight, DirectionDown:
		return true
	default:
		return false
	}
}

func (d Direction) DirectedDelta(delta geometry.Vector) float64 {
	switch d {
	case DirectionLeft:
		return -delta.X
	case DirectionRight:
		return delta.X
	case DirectionDown:
		return delta.Y
	default:
		return math.NaN()
	}
}

func (d Direction) OrthogonalDelta(delta geometry.Vector) float64 {
	switch d {
	case DirectionLeft, DirectionRight:
		return delta.Y
	case DirectionDown:
		return delta.X
	default:
		return math.NaN()
	}
}

func (d Direction) OppositeHorizontal() (Direction, error) {
	switch d {
	case DirectionLeft:
		return DirectionRight, nil
	case DirectionRight:
		return DirectionLeft, nil
	default:
		return "", fmt.Errorf("%q is not a horizontal direction", d)
	}
}

type MotionKind string

const (
	MotionMoved     MotionKind = "moved"
	MotionPartial   MotionKind = "partial"
	MotionClamped   MotionKind = "clamped"
	MotionUncertain MotionKind = "uncertain"
)

func (kind MotionKind) Valid() bool {
	switch kind {
	case MotionMoved, MotionPartial, MotionClamped, MotionUncertain:
		return true
	default:
		return false
	}
}

type MotionObservation struct {
	Kind        MotionKind      `json:"kind"`
	Direction   Direction       `json:"direction"`
	Delta       geometry.Vector `json:"delta"`
	Confidence  float64         `json:"confidence"`
	EvidenceIDs []string        `json:"evidence_ids"`
}

func (observation MotionObservation) Validate(expected Direction, config FrontierConfig) error {
	if !observation.Kind.Valid() {
		return fmt.Errorf("unknown motion kind %q", observation.Kind)
	}
	if !observation.Direction.Valid() {
		return fmt.Errorf("unknown motion direction %q", observation.Direction)
	}
	if observation.Direction != expected {
		return fmt.Errorf("motion direction %q does not match current intent %q", observation.Direction, expected)
	}
	if err := observation.Delta.Validate(); err != nil {
		return fmt.Errorf("motion delta: %w", err)
	}
	if math.IsNaN(observation.Confidence) || math.IsInf(observation.Confidence, 0) || observation.Confidence < 0 || observation.Confidence > 1 {
		return fmt.Errorf("motion confidence must be within [0,1], got %g", observation.Confidence)
	}
	if len(observation.EvidenceIDs) == 0 {
		return errors.New("motion observation requires at least one evidence id")
	}
	for _, id := range observation.EvidenceIDs {
		if id == "" {
			return errors.New("motion observation contains an empty evidence id")
		}
	}

	directed := expected.DirectedDelta(observation.Delta)
	orthogonal := math.Abs(expected.OrthogonalDelta(observation.Delta))
	if observation.Kind != MotionUncertain && observation.Confidence < config.MinimumConfidence {
		return fmt.Errorf("motion confidence %g is below minimum %g; classify it as uncertain", observation.Confidence, config.MinimumConfidence)
	}
	if observation.Kind == MotionMoved || observation.Kind == MotionPartial {
		if directed <= config.ClampTolerance {
			return fmt.Errorf("%s observation has insufficient directed movement %g", observation.Kind, directed)
		}
		if orthogonal > config.CrossAxisTolerance {
			return fmt.Errorf("%s observation has excessive cross-axis movement %g", observation.Kind, orthogonal)
		}
	}
	if observation.Kind == MotionClamped {
		if math.Abs(directed) > config.ClampTolerance || orthogonal > config.CrossAxisTolerance {
			return fmt.Errorf("clamped observation contains movement beyond tolerance: directed=%g orthogonal=%g", directed, orthogonal)
		}
	}
	return nil
}

type FrontierStatus string

const (
	FrontierDiscovering FrontierStatus = "discovering"
	FrontierClosed      FrontierStatus = "closed"
	FrontierUnresolved  FrontierStatus = "unresolved"
)

func (status FrontierStatus) Valid() bool {
	switch status {
	case FrontierDiscovering, FrontierClosed, FrontierUnresolved:
		return true
	default:
		return false
	}
}

type FrontierPhase string

const (
	PhaseHorizontal FrontierPhase = "horizontal"
	PhaseDescend    FrontierPhase = "descend"
	PhaseComplete   FrontierPhase = "complete"
	PhaseUnresolved FrontierPhase = "unresolved"
)

func (phase FrontierPhase) Valid() bool {
	switch phase {
	case PhaseHorizontal, PhaseDescend, PhaseComplete, PhaseUnresolved:
		return true
	default:
		return false
	}
}

type Edge string

const (
	EdgeLeft   Edge = "left"
	EdgeTop    Edge = "top"
	EdgeRight  Edge = "right"
	EdgeBottom Edge = "bottom"
)

type ConfirmedEdges struct {
	Left   bool `json:"left"`
	Top    bool `json:"top"`
	Right  bool `json:"right"`
	Bottom bool `json:"bottom"`
}

func (edges ConfirmedEdges) All() bool {
	return edges.Left && edges.Top && edges.Right && edges.Bottom
}

type SafetyLimits struct {
	MaxRows            int     `json:"max_rows"`
	MaxColumns         int     `json:"max_columns"`
	MaxTiles           int     `json:"max_tiles"`
	MaxTravel          float64 `json:"max_travel"`
	MaxDurationSeconds int64   `json:"max_duration_seconds"`
	MaxDiskBytes       int64   `json:"max_disk_bytes"`
	MaxStepRetries     int     `json:"max_step_retries"`
}

func (limits SafetyLimits) Validate() error {
	if limits.MaxRows <= 0 || limits.MaxColumns <= 0 || limits.MaxTiles <= 0 {
		return errors.New("row, column, and tile safety limits must be positive")
	}
	if math.IsNaN(limits.MaxTravel) || math.IsInf(limits.MaxTravel, 0) || limits.MaxTravel <= 0 {
		return errors.New("travel safety limit must be finite and positive")
	}
	if limits.MaxDurationSeconds <= 0 || limits.MaxDiskBytes <= 0 {
		return errors.New("duration and disk safety limits must be positive")
	}
	if limits.MaxStepRetries < 0 {
		return errors.New("step retry safety limit cannot be negative")
	}
	return nil
}

type FrontierConfig struct {
	HorizontalStep     float64      `json:"horizontal_step"`
	VerticalStep       float64      `json:"vertical_step"`
	ProbeStep          float64      `json:"probe_step"`
	ClampTolerance     float64      `json:"clamp_tolerance"`
	CrossAxisTolerance float64      `json:"cross_axis_tolerance"`
	MinimumConfidence  float64      `json:"minimum_confidence"`
	ClampConfirmations int          `json:"clamp_confirmations"`
	Limits             SafetyLimits `json:"limits"`
}

func (config FrontierConfig) Validate() error {
	values := []struct {
		name  string
		value float64
	}{
		{"horizontal_step", config.HorizontalStep},
		{"vertical_step", config.VerticalStep},
		{"probe_step", config.ProbeStep},
	}
	for _, item := range values {
		if math.IsNaN(item.value) || math.IsInf(item.value, 0) || item.value <= 0 {
			return fmt.Errorf("%s must be finite and positive", item.name)
		}
	}
	if config.ProbeStep > config.HorizontalStep || config.ProbeStep > config.VerticalStep {
		return errors.New("probe_step cannot exceed either nominal step")
	}
	if math.IsNaN(config.ClampTolerance) || math.IsInf(config.ClampTolerance, 0) || config.ClampTolerance < 0 {
		return errors.New("clamp_tolerance must be finite and non-negative")
	}
	if math.IsNaN(config.CrossAxisTolerance) || math.IsInf(config.CrossAxisTolerance, 0) || config.CrossAxisTolerance < 0 {
		return errors.New("cross_axis_tolerance must be finite and non-negative")
	}
	if math.IsNaN(config.MinimumConfidence) || math.IsInf(config.MinimumConfidence, 0) || config.MinimumConfidence < 0 || config.MinimumConfidence > 1 {
		return errors.New("minimum_confidence must be within [0,1]")
	}
	if config.ClampConfirmations <= 0 {
		return errors.New("clamp_confirmations must be positive")
	}
	return config.Limits.Validate()
}

type MovePurpose string

const (
	PurposeTraverse      MovePurpose = "traverse"
	PurposeConfirmEdge   MovePurpose = "confirm_edge"
	PurposeDescend       MovePurpose = "descend"
	PurposeConfirmBottom MovePurpose = "confirm_bottom"
	PurposeRetry         MovePurpose = "retry"
)

func (purpose MovePurpose) Valid() bool {
	switch purpose {
	case PurposeTraverse, PurposeConfirmEdge, PurposeDescend, PurposeConfirmBottom, PurposeRetry:
		return true
	default:
		return false
	}
}

type MoveIntent struct {
	Direction Direction   `json:"direction"`
	Distance  float64     `json:"distance"`
	Purpose   MovePurpose `json:"purpose"`
}

func (intent MoveIntent) Validate() error {
	if !intent.Direction.Valid() {
		return fmt.Errorf("unknown move direction %q", intent.Direction)
	}
	if math.IsNaN(intent.Distance) || math.IsInf(intent.Distance, 0) || intent.Distance <= 0 {
		return fmt.Errorf("move distance must be finite and positive, got %g", intent.Distance)
	}
	if !intent.Purpose.Valid() {
		return fmt.Errorf("unknown move purpose %q", intent.Purpose)
	}
	return nil
}

func (observation MotionObservation) ValidateForIntent(intent MoveIntent, config FrontierConfig) error {
	if err := intent.Validate(); err != nil {
		return fmt.Errorf("intent: %w", err)
	}
	if err := observation.Validate(intent.Direction, config); err != nil {
		return err
	}
	if observation.Kind != MotionMoved && observation.Kind != MotionPartial {
		return nil
	}

	directed := intent.Direction.DirectedDelta(observation.Delta)
	fullThreshold := intent.Distance - config.ClampTolerance
	if observation.Kind == MotionMoved && directed < fullThreshold {
		return fmt.Errorf("moved observation covered %g of requested %g; classify it as partial", directed, intent.Distance)
	}
	if observation.Kind == MotionPartial && directed >= fullThreshold {
		return fmt.Errorf("partial observation covered %g of requested %g; classify it as moved", directed, intent.Distance)
	}
	return nil
}

type TilePlacement struct {
	Sequence         int            `json:"sequence"`
	ID               string         `json:"id"`
	Row              int            `json:"row"`
	Position         geometry.Point `json:"position"`
	ObservationIndex int            `json:"observation_index"`
}

type RowSnapshot struct {
	Index         int            `json:"index"`
	Direction     Direction      `json:"direction"`
	StartEdge     Edge           `json:"start_edge"`
	EndEdge       Edge           `json:"end_edge,omitempty"`
	StartPosition geometry.Point `json:"start_position"`
	MinX          float64        `json:"min_x"`
	MaxX          float64        `json:"max_x"`
	MinY          float64        `json:"min_y"`
	MaxY          float64        `json:"max_y"`
	TileIDs       []string       `json:"tile_ids"`
	EndConfirmed  bool           `json:"end_confirmed"`
}

type FrontierSnapshot struct {
	SchemaVersion    int                 `json:"schema_version"`
	Revision         int                 `json:"revision"`
	Status           FrontierStatus      `json:"status"`
	Phase            FrontierPhase       `json:"phase"`
	Config           FrontierConfig      `json:"config"`
	CurrentRow       int                 `json:"current_row"`
	Direction        Direction           `json:"direction"`
	Position         geometry.Point      `json:"position"`
	NextStep         float64             `json:"next_step"`
	ClampStreak      int                 `json:"clamp_streak"`
	UncertainStreak  int                 `json:"uncertain_streak"`
	ConfirmedEdges   ConfirmedEdges      `json:"confirmed_edges"`
	Rows             []RowSnapshot       `json:"rows"`
	Tiles            []TilePlacement     `json:"tiles"`
	Observations     []MotionObservation `json:"observations"`
	Travel           float64             `json:"travel"`
	UnresolvedReason string              `json:"unresolved_reason,omitempty"`
}

type Transition struct {
	Revision      int            `json:"revision"`
	Status        FrontierStatus `json:"status"`
	AcceptedTile  bool           `json:"accepted_tile"`
	ConfirmedEdge Edge           `json:"confirmed_edge,omitempty"`
	StartedRow    bool           `json:"started_row"`
	Reason        string         `json:"reason,omitempty"`
}
