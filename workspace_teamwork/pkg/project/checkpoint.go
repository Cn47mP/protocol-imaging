package project

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/capture"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
)

const (
	PlanSchemaVersion     = 1
	BoundarySchemaVersion = 1
)

type PlanDocument struct {
	SchemaVersion       int                      `json:"schema_version"`
	ID                  string                   `json:"id"`
	CreatedAt           time.Time                `json:"created_at"`
	Supersedes          string                   `json:"supersedes,omitempty"`
	Trigger             string                   `json:"trigger"`
	Frontier            capture.FrontierSnapshot `json:"frontier"`
	RequiredAdjacencies []TileAdjacency          `json:"required_adjacencies"`
	TileChecksums       map[string]string        `json:"tile_checksums,omitempty"`
	AcceptableOverlap   OverlapRange             `json:"acceptable_overlap"`
	CoverageAudit       *CoverageAudit           `json:"coverage_audit,omitempty"`
	Extra               ExtraFields              `json:"-"`
}

type TileAdjacency struct {
	FromTile string      `json:"from_tile"`
	ToTile   string      `json:"to_tile"`
	Axis     string      `json:"axis"`
	Extra    ExtraFields `json:"-"`
}

type OverlapRange struct {
	Minimum float64     `json:"minimum"`
	Maximum float64     `json:"maximum"`
	Extra   ExtraFields `json:"-"`
}

// CoverageAudit is deliberately separate from frontier closure. Four observed
// edges only establish a finite candidate range; observed_local additionally
// requires every accepted tile and required adjacency to pass this audit.
type CoverageAudit struct {
	Algorithm           string          `json:"algorithm"`
	AuditedAt           time.Time       `json:"audited_at"`
	Passed              bool            `json:"passed"`
	CoveredTileIDs      []string        `json:"covered_tile_ids"`
	MissingTileIDs      []string        `json:"missing_tile_ids"`
	VerifiedAdjacencies []TileAdjacency `json:"verified_adjacencies"`
	Confidence          float64         `json:"confidence"`
	Extra               ExtraFields     `json:"-"`
}

type BoundaryDocument struct {
	SchemaVersion           int                    `json:"schema_version"`
	Revision                int                    `json:"revision"`
	Status                  GeometryStatus         `json:"status"`
	CoordinateCompatibility string                 `json:"coordinate_compatibility"`
	Origin                  geometry.Point         `json:"origin"`
	ConfirmedEdges          capture.ConfirmedEdges `json:"confirmed_edges"`
	Events                  []BoundaryEvent        `json:"events"`
	Rows                    []BoundaryRow          `json:"rows"`
	Bounds                  *ObservedBounds        `json:"bounds"`
	CurrentPosition         geometry.Point         `json:"current_position"`
	Travel                  float64                `json:"travel"`
	ClosureError            geometry.Vector        `json:"closure_error"`
	Confidence              float64                `json:"confidence"`
	UnresolvedReason        string                 `json:"unresolved_reason,omitempty"`
	Extra                   ExtraFields            `json:"-"`
}

type BoundaryEvent struct {
	Sequence       int                       `json:"sequence"`
	Phase          string                    `json:"phase"`
	ActionID       string                    `json:"action_id"`
	ObservedAt     time.Time                 `json:"observed_at"`
	Intent         capture.MoveIntent        `json:"intent"`
	Observation    capture.MotionObservation `json:"observation"`
	ConfirmedEdge  capture.Edge              `json:"confirmed_edge,omitempty"`
	SourceFrameIDs []string                  `json:"source_frame_ids"`
	Extra          ExtraFields               `json:"-"`
}

type BoundaryRow struct {
	Index        int               `json:"index"`
	Direction    capture.Direction `json:"direction"`
	MinX         float64           `json:"min_x"`
	MaxX         float64           `json:"max_x"`
	MinY         float64           `json:"min_y"`
	MaxY         float64           `json:"max_y"`
	TileIDs      []string          `json:"tile_ids"`
	EndConfirmed bool              `json:"end_confirmed"`
	Extra        ExtraFields       `json:"-"`
}

type ObservedBounds struct {
	MinX  float64     `json:"min_x"`
	MaxX  float64     `json:"max_x"`
	MinY  float64     `json:"min_y"`
	MaxY  float64     `json:"max_y"`
	Extra ExtraFields `json:"-"`
}

func (plan PlanDocument) Validate() error {
	if plan.SchemaVersion != PlanSchemaVersion {
		return fmt.Errorf("unsupported plan schema_version %d", plan.SchemaVersion)
	}
	if err := validateStableID("id", plan.ID); err != nil {
		return err
	}
	if err := validateUTCTime("created_at", plan.CreatedAt); err != nil {
		return err
	}
	if plan.Supersedes != "" {
		if err := ValidateArchivePath(plan.Supersedes); err != nil {
			return fmt.Errorf("supersedes: %w", err)
		}
	}
	if err := validateStableID("trigger", plan.Trigger); err != nil {
		return err
	}
	if err := capture.ValidateFrontierSnapshot(plan.Frontier); err != nil {
		return fmt.Errorf("frontier: %w", err)
	}
	if err := plan.AcceptableOverlap.Validate(); err != nil {
		return fmt.Errorf("acceptable_overlap: %w", err)
	}

	tiles := make(map[string]struct{}, len(plan.Frontier.Tiles))
	for _, tile := range plan.Frontier.Tiles {
		tiles[tile.ID] = struct{}{}
	}
	for tileID, checksum := range plan.TileChecksums {
		if _, exists := tiles[tileID]; !exists {
			return fmt.Errorf("tile_checksums contains unknown tile %q", tileID)
		}
		if len(checksum) != 64 {
			return fmt.Errorf("tile_checksums[%q] must be a SHA-256 hex digest", tileID)
		}
		for _, char := range checksum {
			if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
				return fmt.Errorf("tile_checksums[%q] is not lowercase hexadecimal", tileID)
			}
		}
	}
	seenAdjacencies := make(map[string]struct{}, len(plan.RequiredAdjacencies))
	for index, adjacency := range plan.RequiredAdjacencies {
		if err := adjacency.Validate(tiles); err != nil {
			return fmt.Errorf("required_adjacencies[%d]: %w", index, err)
		}
		key := adjacencyKey(adjacency)
		if _, duplicate := seenAdjacencies[key]; duplicate {
			return fmt.Errorf("duplicate required adjacency %q -> %q", adjacency.FromTile, adjacency.ToTile)
		}
		seenAdjacencies[key] = struct{}{}
	}
	if plan.CoverageAudit != nil {
		if err := plan.CoverageAudit.Validate(tiles, plan.RequiredAdjacencies); err != nil {
			return fmt.Errorf("coverage_audit: %w", err)
		}
	}
	return nil
}

func (adjacency TileAdjacency) Validate(tiles map[string]struct{}) error {
	if adjacency.FromTile == adjacency.ToTile {
		return errors.New("adjacency cannot connect a tile to itself")
	}
	if _, exists := tiles[adjacency.FromTile]; !exists {
		return fmt.Errorf("from_tile %q is not in the frontier", adjacency.FromTile)
	}
	if _, exists := tiles[adjacency.ToTile]; !exists {
		return fmt.Errorf("to_tile %q is not in the frontier", adjacency.ToTile)
	}
	if adjacency.Axis != "horizontal" && adjacency.Axis != "vertical" {
		return fmt.Errorf("axis must be horizontal or vertical, got %q", adjacency.Axis)
	}
	return nil
}

func (overlap OverlapRange) Validate() error {
	if !finite(overlap.Minimum) || !finite(overlap.Maximum) || overlap.Minimum <= 0 || overlap.Maximum >= 1 || overlap.Minimum > overlap.Maximum {
		return fmt.Errorf("range must satisfy 0 < minimum <= maximum < 1, got [%g,%g]", overlap.Minimum, overlap.Maximum)
	}
	return nil
}

func (audit CoverageAudit) Validate(tiles map[string]struct{}, required []TileAdjacency) error {
	if err := validateStableID("algorithm", audit.Algorithm); err != nil {
		return err
	}
	if err := validateUTCTime("audited_at", audit.AuditedAt); err != nil {
		return err
	}
	if !finite(audit.Confidence) || audit.Confidence < 0 || audit.Confidence > 1 {
		return fmt.Errorf("confidence must be within [0,1], got %g", audit.Confidence)
	}

	classified := make(map[string]string, len(audit.CoveredTileIDs)+len(audit.MissingTileIDs))
	validateTileSet := func(label string, ids []string) error {
		for _, id := range ids {
			if _, exists := tiles[id]; !exists {
				return fmt.Errorf("%s contains unknown tile %q", label, id)
			}
			if prior, duplicate := classified[id]; duplicate {
				return fmt.Errorf("tile %q appears in both or twice within %s and %s", id, prior, label)
			}
			classified[id] = label
		}
		return nil
	}
	if err := validateTileSet("covered_tile_ids", audit.CoveredTileIDs); err != nil {
		return err
	}
	if err := validateTileSet("missing_tile_ids", audit.MissingTileIDs); err != nil {
		return err
	}
	if len(classified) != len(tiles) {
		return fmt.Errorf("coverage audit classifies %d of %d frontier tiles", len(classified), len(tiles))
	}

	requiredSet := make(map[string]struct{}, len(required))
	for _, adjacency := range required {
		requiredSet[adjacencyKey(adjacency)] = struct{}{}
	}
	verifiedSet := make(map[string]struct{}, len(audit.VerifiedAdjacencies))
	for index, adjacency := range audit.VerifiedAdjacencies {
		if err := adjacency.Validate(tiles); err != nil {
			return fmt.Errorf("verified_adjacencies[%d]: %w", index, err)
		}
		key := adjacencyKey(adjacency)
		if _, duplicate := verifiedSet[key]; duplicate {
			return fmt.Errorf("duplicate verified adjacency %q -> %q", adjacency.FromTile, adjacency.ToTile)
		}
		verifiedSet[key] = struct{}{}
	}
	if audit.Passed {
		if len(audit.MissingTileIDs) != 0 || len(audit.CoveredTileIDs) != len(tiles) {
			return errors.New("passed coverage audit must cover every tile and report no missing tiles")
		}
		for key := range requiredSet {
			if _, verified := verifiedSet[key]; !verified {
				return errors.New("passed coverage audit is missing a required adjacency")
			}
		}
	}
	return nil
}

func adjacencyKey(adjacency TileAdjacency) string {
	ends := []string{adjacency.FromTile, adjacency.ToTile}
	sort.Strings(ends)
	return ends[0] + "\x00" + ends[1] + "\x00" + adjacency.Axis
}

func (boundary BoundaryDocument) Validate() error {
	if boundary.SchemaVersion != BoundarySchemaVersion {
		return fmt.Errorf("unsupported boundary schema_version %d", boundary.SchemaVersion)
	}
	if boundary.Revision < 0 {
		return errors.New("revision cannot be negative")
	}
	if !boundary.Status.Valid() {
		return fmt.Errorf("unknown status %q", boundary.Status)
	}
	if boundary.CoordinateCompatibility != "session_local" {
		return fmt.Errorf("coordinate_compatibility must be session_local, got %q", boundary.CoordinateCompatibility)
	}
	if err := boundary.Origin.Validate(); err != nil {
		return fmt.Errorf("origin: %w", err)
	}
	if boundary.Origin != (geometry.Point{}) {
		return errors.New("format v1 boundary origin must be the session top-left anchor (0,0)")
	}
	if err := boundary.CurrentPosition.Validate(); err != nil {
		return fmt.Errorf("current_position: %w", err)
	}
	if !finite(boundary.Travel) || boundary.Travel < 0 {
		return fmt.Errorf("travel must be finite and non-negative, got %g", boundary.Travel)
	}
	if err := boundary.ClosureError.Validate(); err != nil {
		return fmt.Errorf("closure_error: %w", err)
	}
	if !finite(boundary.Confidence) || boundary.Confidence < 0 || boundary.Confidence > 1 {
		return fmt.Errorf("confidence must be within [0,1], got %g", boundary.Confidence)
	}
	for index, event := range boundary.Events {
		if event.Sequence != index {
			return fmt.Errorf("events[%d] has sequence %d", index, event.Sequence)
		}
		if err := event.Validate(); err != nil {
			return fmt.Errorf("events[%d]: %w", index, err)
		}
	}
	seenRows := make(map[int]struct{}, len(boundary.Rows))
	for index, row := range boundary.Rows {
		if err := row.Validate(); err != nil {
			return fmt.Errorf("rows[%d]: %w", index, err)
		}
		if _, duplicate := seenRows[row.Index]; duplicate {
			return fmt.Errorf("duplicate boundary row %d", row.Index)
		}
		seenRows[row.Index] = struct{}{}
	}
	if boundary.Bounds != nil {
		if err := boundary.Bounds.Validate(); err != nil {
			return fmt.Errorf("bounds: %w", err)
		}
	}

	switch boundary.Status {
	case GeometryObservedLocal:
		if !boundary.ConfirmedEdges.All() || boundary.Bounds == nil || len(boundary.Rows) == 0 {
			return errors.New("observed_local boundary requires all edges, rows, and final bounds")
		}
		if boundary.UnresolvedReason != "" {
			return errors.New("observed_local boundary cannot contain an unresolved reason")
		}
	case GeometryDiscovering:
		if boundary.UnresolvedReason != "" {
			return errors.New("discovering boundary cannot contain an unresolved reason")
		}
	case GeometryUnresolved:
		if boundary.UnresolvedReason == "" {
			return errors.New("unresolved boundary requires a reason")
		}
	}
	return nil
}

func (event BoundaryEvent) Validate() error {
	if event.Phase != "homing" && event.Phase != "frontier" {
		return fmt.Errorf("phase must be homing or frontier, got %q", event.Phase)
	}
	if err := validateStableID("action_id", event.ActionID); err != nil {
		return err
	}
	if err := validateUTCTime("observed_at", event.ObservedAt); err != nil {
		return err
	}
	if err := event.Intent.Validate(); err != nil {
		return fmt.Errorf("intent: %w", err)
	}
	if !event.Observation.Kind.Valid() || event.Observation.Direction != event.Intent.Direction {
		return errors.New("observation kind or direction does not match intent")
	}
	if err := event.Observation.Delta.Validate(); err != nil {
		return fmt.Errorf("observation delta: %w", err)
	}
	if !finite(event.Observation.Confidence) || event.Observation.Confidence < 0 || event.Observation.Confidence > 1 {
		return errors.New("observation confidence must be within [0,1]")
	}
	if len(event.Observation.EvidenceIDs) == 0 {
		return errors.New("observation requires evidence_ids")
	}
	if event.ConfirmedEdge != "" && event.ConfirmedEdge != capture.EdgeLeft && event.ConfirmedEdge != capture.EdgeTop &&
		event.ConfirmedEdge != capture.EdgeRight && event.ConfirmedEdge != capture.EdgeBottom {
		return fmt.Errorf("unknown confirmed_edge %q", event.ConfirmedEdge)
	}
	if len(event.SourceFrameIDs) == 0 {
		return errors.New("source_frame_ids must not be empty")
	}
	for _, id := range event.SourceFrameIDs {
		if err := validateStableID("source_frame_id", id); err != nil {
			return err
		}
	}
	return nil
}

func (row BoundaryRow) Validate() error {
	if row.Index < 0 {
		return errors.New("index cannot be negative")
	}
	if !row.Direction.Valid() || row.Direction == capture.DirectionDown {
		return fmt.Errorf("direction must be horizontal, got %q", row.Direction)
	}
	values := [...]float64{row.MinX, row.MaxX, row.MinY, row.MaxY}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.New("row bounds must be finite")
		}
	}
	if row.MinX > row.MaxX || row.MinY > row.MaxY {
		return errors.New("row bounds are inverted")
	}
	if len(row.TileIDs) == 0 {
		return errors.New("tile_ids must not be empty")
	}
	seen := make(map[string]struct{}, len(row.TileIDs))
	for _, id := range row.TileIDs {
		if err := validateStableID("tile_id", id); err != nil {
			return err
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("duplicate tile_id %q", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func (bounds ObservedBounds) Validate() error {
	values := [...]float64{bounds.MinX, bounds.MaxX, bounds.MinY, bounds.MaxY}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.New("bounds must be finite")
		}
	}
	if bounds.MinX > bounds.MaxX || bounds.MinY > bounds.MaxY {
		return errors.New("bounds are inverted")
	}
	return nil
}

func NewBoundaryFromFrontier(frontier capture.FrontierSnapshot, confidence float64) (BoundaryDocument, error) {
	if err := capture.ValidateFrontierSnapshot(frontier); err != nil {
		return BoundaryDocument{}, err
	}
	status := GeometryDiscovering
	if frontier.Status == capture.FrontierUnresolved {
		status = GeometryUnresolved
	}
	rows := make([]BoundaryRow, len(frontier.Rows))
	for index, row := range frontier.Rows {
		rows[index] = BoundaryRow{
			Index:        row.Index,
			Direction:    row.Direction,
			MinX:         row.MinX,
			MaxX:         row.MaxX,
			MinY:         row.MinY,
			MaxY:         row.MaxY,
			TileIDs:      append([]string(nil), row.TileIDs...),
			EndConfirmed: row.EndConfirmed,
		}
	}
	bounds := boundsFromTiles(frontier)
	document := BoundaryDocument{
		SchemaVersion:           BoundarySchemaVersion,
		Revision:                frontier.Revision,
		Status:                  status,
		CoordinateCompatibility: "session_local",
		ConfirmedEdges:          frontier.ConfirmedEdges,
		Rows:                    rows,
		Bounds:                  &bounds,
		CurrentPosition:         frontier.Position,
		Travel:                  frontier.Travel,
		Confidence:              confidence,
		UnresolvedReason:        frontier.UnresolvedReason,
	}
	if err := document.Validate(); err != nil {
		return BoundaryDocument{}, err
	}
	return document, nil
}

func NewInitialBoundary() BoundaryDocument {
	return BoundaryDocument{
		SchemaVersion:           BoundarySchemaVersion,
		Status:                  GeometryDiscovering,
		CoordinateCompatibility: "session_local",
	}
}

func boundsFromTiles(frontier capture.FrontierSnapshot) ObservedBounds {
	bounds := ObservedBounds{
		MinX: frontier.Tiles[0].Position.X,
		MaxX: frontier.Tiles[0].Position.X,
		MinY: frontier.Tiles[0].Position.Y,
		MaxY: frontier.Tiles[0].Position.Y,
	}
	for _, tile := range frontier.Tiles[1:] {
		bounds.MinX = math.Min(bounds.MinX, tile.Position.X)
		bounds.MaxX = math.Max(bounds.MaxX, tile.Position.X)
		bounds.MinY = math.Min(bounds.MinY, tile.Position.Y)
		bounds.MaxY = math.Max(bounds.MaxY, tile.Position.Y)
	}
	return bounds
}

func ValidateCheckpoint(plan PlanDocument, boundary BoundaryDocument) error {
	if err := plan.Validate(); err != nil {
		return fmt.Errorf("plan: %w", err)
	}
	if err := boundary.Validate(); err != nil {
		return fmt.Errorf("boundary: %w", err)
	}
	if boundary.Revision != plan.Frontier.Revision {
		return fmt.Errorf("boundary revision %d does not match frontier revision %d", boundary.Revision, plan.Frontier.Revision)
	}
	switch plan.Frontier.Status {
	case capture.FrontierDiscovering:
		if boundary.Status != GeometryDiscovering {
			return fmt.Errorf("discovering frontier cannot produce geometry status %q", boundary.Status)
		}
	case capture.FrontierClosed:
		if boundary.Status != GeometryDiscovering && boundary.Status != GeometryObservedLocal {
			return fmt.Errorf("closed frontier cannot produce geometry status %q", boundary.Status)
		}
	case capture.FrontierUnresolved:
		if boundary.Status != GeometryUnresolved {
			return fmt.Errorf("unresolved frontier cannot produce geometry status %q", boundary.Status)
		}
	}
	if boundary.ConfirmedEdges != plan.Frontier.ConfirmedEdges {
		return errors.New("boundary confirmed edges do not match frontier")
	}
	if boundary.CurrentPosition != plan.Frontier.Position || !valuesNear(boundary.Travel, plan.Frontier.Travel) {
		return errors.New("boundary motion summary does not match frontier")
	}
	if len(boundary.Rows) != len(plan.Frontier.Rows) {
		return errors.New("boundary rows do not match frontier")
	}
	for index, row := range boundary.Rows {
		frontierRow := plan.Frontier.Rows[index]
		if row.Index != frontierRow.Index || row.Direction != frontierRow.Direction ||
			!valuesNear(row.MinX, frontierRow.MinX) || !valuesNear(row.MaxX, frontierRow.MaxX) ||
			!valuesNear(row.MinY, frontierRow.MinY) || !valuesNear(row.MaxY, frontierRow.MaxY) ||
			row.EndConfirmed != frontierRow.EndConfirmed {
			return fmt.Errorf("boundary row %d does not match frontier", index)
		}
	}
	if plan.Frontier.Status == capture.FrontierClosed {
		if err := capture.AuditClosed(plan.Frontier); err != nil {
			return fmt.Errorf("closed frontier audit: %w", err)
		}
	}
	if boundary.Status == GeometryObservedLocal {
		if plan.Frontier.Status != capture.FrontierClosed || plan.CoverageAudit == nil || !plan.CoverageAudit.Passed {
			return errors.New("observed_local geometry requires a closed frontier and a passed coverage audit")
		}
	}
	return nil
}

func valuesNear(left, right float64) bool {
	return math.Abs(left-right) <= 1e-9
}
