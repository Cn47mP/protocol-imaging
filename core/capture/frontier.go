package capture

import (
	"errors"
	"fmt"
	"math"

	"github.com/Cn47mP/protocol-imaging/core/geometry"
)

type Frontier struct {
	state FrontierSnapshot
}

func NewFrontier(config FrontierConfig, anchorTileID string) (*Frontier, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("frontier config: %w", err)
	}
	if anchorTileID == "" {
		return nil, errors.New("anchor tile id is required")
	}

	anchor := geometry.Point{}
	frontier := &Frontier{
		state: FrontierSnapshot{
			SchemaVersion:  FrontierSchemaVersion,
			Revision:       1,
			Status:         FrontierDiscovering,
			Phase:          PhaseHorizontal,
			Config:         config,
			CurrentRow:     0,
			Direction:      DirectionRight,
			Position:       anchor,
			NextStep:       config.HorizontalStep,
			ConfirmedEdges: ConfirmedEdges{Left: true, Top: true},
			Rows: []RowSnapshot{
				newRowSnapshot(0, DirectionRight, EdgeLeft, anchor, anchorTileID),
			},
			Tiles: []TilePlacement{
				{Sequence: 0, ID: anchorTileID, Row: 0, Position: anchor, ObservationIndex: -1},
			},
		},
	}
	return frontier, nil
}

func RestoreFrontier(snapshot FrontierSnapshot) (*Frontier, error) {
	if err := ValidateFrontierSnapshot(snapshot); err != nil {
		return nil, err
	}
	return &Frontier{state: cloneSnapshot(snapshot)}, nil
}

func (frontier *Frontier) Snapshot() FrontierSnapshot {
	return cloneSnapshot(frontier.state)
}

func (frontier *Frontier) NextIntent() (MoveIntent, error) {
	state := frontier.state
	switch state.Status {
	case FrontierClosed:
		return MoveIntent{}, ErrComplete
	case FrontierUnresolved:
		return MoveIntent{}, fmt.Errorf("%w: %s", ErrUnresolved, state.UnresolvedReason)
	case FrontierDiscovering:
	default:
		return MoveIntent{}, fmt.Errorf("%w: unknown status %q", ErrInvalidState, state.Status)
	}

	purpose := PurposeTraverse
	if state.UncertainStreak > 0 {
		purpose = PurposeRetry
	} else if state.Phase == PhaseDescend {
		purpose = PurposeDescend
		if state.ClampStreak > 0 {
			purpose = PurposeConfirmBottom
		}
	} else if state.ClampStreak > 0 {
		purpose = PurposeConfirmEdge
	}

	direction := state.Direction
	if state.Phase == PhaseDescend {
		direction = DirectionDown
	}
	return MoveIntent{Direction: direction, Distance: state.NextStep, Purpose: purpose}, nil
}

func (frontier *Frontier) Observe(observation MotionObservation, tileID string) (Transition, error) {
	intent, err := frontier.NextIntent()
	if err != nil {
		return Transition{}, err
	}
	if err := observation.ValidateForIntent(intent, frontier.state.Config); err != nil {
		return Transition{}, err
	}
	if (observation.Kind == MotionMoved || observation.Kind == MotionPartial) && tileID == "" {
		return Transition{}, errors.New("a moved or partial observation requires a tile id")
	}
	if observation.Kind != MotionMoved && observation.Kind != MotionPartial && tileID != "" {
		return Transition{}, fmt.Errorf("%s observation cannot commit tile %q", observation.Kind, tileID)
	}
	if tileID != "" && frontier.hasTile(tileID) {
		return Transition{}, fmt.Errorf("duplicate tile id %q", tileID)
	}

	frontier.state.Observations = append(frontier.state.Observations, cloneObservation(observation))
	observationIndex := len(frontier.state.Observations) - 1
	frontier.state.Revision++
	transition := Transition{Revision: frontier.state.Revision, Status: frontier.state.Status}

	switch observation.Kind {
	case MotionUncertain:
		frontier.state.UncertainStreak++
		frontier.state.ClampStreak = 0
		frontier.state.NextStep = frontier.state.Config.ProbeStep
		if frontier.state.UncertainStreak > frontier.state.Config.Limits.MaxStepRetries {
			return frontier.markUnresolved("motion remained uncertain after the configured retry limit", nil, transition)
		}
	case MotionClamped:
		frontier.state.UncertainStreak = 0
		frontier.state.ClampStreak++
		frontier.state.NextStep = frontier.state.Config.ProbeStep
		if frontier.state.ClampStreak >= frontier.state.Config.ClampConfirmations {
			if frontier.state.Phase == PhaseHorizontal {
				edge := edgeForDirection(frontier.state.Direction)
				frontier.confirmCurrentRow(edge)
				frontier.state.Phase = PhaseDescend
				frontier.state.NextStep = frontier.state.Config.VerticalStep
				frontier.state.ClampStreak = 0
				transition.ConfirmedEdge = edge
			} else {
				frontier.state.ConfirmedEdges.Bottom = true
				frontier.state.Status = FrontierClosed
				frontier.state.Phase = PhaseComplete
				frontier.state.ClampStreak = 0
				transition.ConfirmedEdge = EdgeBottom
			}
		}
	case MotionMoved, MotionPartial:
		frontier.state.UncertainStreak = 0
		frontier.state.ClampStreak = 0
		frontier.state.Position = frontier.state.Position.Add(observation.Delta)
		frontier.state.Travel += observation.Delta.ManhattanLength()

		if frontier.state.Phase == PhaseHorizontal {
			frontier.appendTile(tileID, frontier.state.CurrentRow, observationIndex)
			frontier.updateCurrentRowBounds(frontier.state.Position)
			frontier.state.NextStep = frontier.state.Config.HorizontalStep
			transition.AcceptedTile = true
			if observation.Kind == MotionPartial {
				frontier.state.NextStep = frontier.state.Config.ProbeStep
			}
		} else {
			nextDirection, oppositeErr := frontier.state.Direction.OppositeHorizontal()
			if oppositeErr != nil {
				return Transition{}, oppositeErr
			}
			frontier.state.CurrentRow++
			frontier.state.Direction = nextDirection
			frontier.state.Rows = append(frontier.state.Rows, newRowSnapshot(
				frontier.state.CurrentRow,
				nextDirection,
				edgeForDirection(oppositeDirection(nextDirection)),
				frontier.state.Position,
				"",
			))
			frontier.appendTile(tileID, frontier.state.CurrentRow, observationIndex)
			frontier.state.Phase = PhaseHorizontal
			frontier.state.NextStep = frontier.state.Config.HorizontalStep
			transition.AcceptedTile = true
			transition.StartedRow = true
		}
	}

	transition.Status = frontier.state.Status
	if err := frontier.checkSafetyLimits(); err != nil {
		return frontier.markUnresolved(err.Error(), err, transition)
	}
	return transition, nil
}

func ValidateFrontierSnapshot(snapshot FrontierSnapshot) error {
	if snapshot.SchemaVersion != FrontierSchemaVersion {
		return fmt.Errorf("%w: unsupported frontier schema version %d", ErrInvalidState, snapshot.SchemaVersion)
	}
	if snapshot.Revision <= 0 {
		return fmt.Errorf("%w: revision must be positive", ErrInvalidState)
	}
	if snapshot.Revision != len(snapshot.Observations)+1 {
		return fmt.Errorf("%w: revision %d does not match %d observations", ErrInvalidState, snapshot.Revision, len(snapshot.Observations))
	}
	if !snapshot.Status.Valid() || !snapshot.Phase.Valid() {
		return fmt.Errorf("%w: unknown status or phase", ErrInvalidState)
	}
	if err := snapshot.Config.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidState, err)
	}
	if err := snapshot.Position.Validate(); err != nil {
		return fmt.Errorf("%w: position: %v", ErrInvalidState, err)
	}
	if !snapshot.Direction.Valid() || snapshot.Direction == DirectionDown {
		return fmt.Errorf("%w: current row direction must be horizontal", ErrInvalidState)
	}
	if len(snapshot.Rows) == 0 || len(snapshot.Tiles) == 0 {
		return fmt.Errorf("%w: frontier requires rows and tiles", ErrInvalidState)
	}
	if snapshot.CurrentRow != len(snapshot.Rows)-1 {
		return fmt.Errorf("%w: current row %d is not the final row %d", ErrInvalidState, snapshot.CurrentRow, len(snapshot.Rows)-1)
	}
	if snapshot.NextStep <= 0 || math.IsNaN(snapshot.NextStep) || math.IsInf(snapshot.NextStep, 0) {
		return fmt.Errorf("%w: next step must be finite and positive", ErrInvalidState)
	}
	if snapshot.ClampStreak < 0 || snapshot.UncertainStreak < 0 {
		return fmt.Errorf("%w: streak counters cannot be negative", ErrInvalidState)
	}
	if snapshot.ClampStreak >= snapshot.Config.ClampConfirmations {
		return fmt.Errorf("%w: clamp streak should have transitioned before reaching confirmation count", ErrInvalidState)
	}
	if snapshot.ClampStreak > 0 && snapshot.UncertainStreak > 0 {
		return fmt.Errorf("%w: clamp and uncertain streaks cannot both be active", ErrInvalidState)
	}
	if math.IsNaN(snapshot.Travel) || math.IsInf(snapshot.Travel, 0) || snapshot.Travel < 0 {
		return fmt.Errorf("%w: travel must be finite and non-negative", ErrInvalidState)
	}

	if !snapshot.ConfirmedEdges.Left || !snapshot.ConfirmedEdges.Top {
		return fmt.Errorf("%w: a frontier anchored at top-left must retain left and top evidence", ErrInvalidState)
	}

	seenTiles := make(map[string]TilePlacement, len(snapshot.Tiles))
	observationTiles := make(map[int]string, len(snapshot.Tiles))
	for index, tile := range snapshot.Tiles {
		if tile.ID == "" {
			return fmt.Errorf("%w: tile %d has an empty id", ErrInvalidState, index)
		}
		if _, exists := seenTiles[tile.ID]; exists {
			return fmt.Errorf("%w: duplicate tile id %q", ErrInvalidState, tile.ID)
		}
		seenTiles[tile.ID] = tile
		if tile.Sequence != index || tile.Row < 0 || tile.Row >= len(snapshot.Rows) {
			return fmt.Errorf("%w: invalid tile sequence or row for %q", ErrInvalidState, tile.ID)
		}
		if err := tile.Position.Validate(); err != nil {
			return fmt.Errorf("%w: tile %q position: %v", ErrInvalidState, tile.ID, err)
		}
		if tile.ObservationIndex < -1 || tile.ObservationIndex >= len(snapshot.Observations) {
			return fmt.Errorf("%w: tile %q observation index is out of range", ErrInvalidState, tile.ID)
		}
		if index == 0 {
			if tile.Row != 0 || tile.ObservationIndex != -1 || tile.Position != (geometry.Point{}) {
				return fmt.Errorf("%w: first tile must be the top-left anchor", ErrInvalidState)
			}
		} else {
			if tile.ObservationIndex < 0 {
				return fmt.Errorf("%w: non-anchor tile %q lacks an observation", ErrInvalidState, tile.ID)
			}
			if prior, duplicate := observationTiles[tile.ObservationIndex]; duplicate {
				return fmt.Errorf("%w: tiles %q and %q share observation %d", ErrInvalidState, prior, tile.ID, tile.ObservationIndex)
			}
			observationTiles[tile.ObservationIndex] = tile.ID
		}
	}
	rowReferences := make(map[string]int, len(snapshot.Tiles))
	confirmedHorizontalEdges := ConfirmedEdges{Left: true, Top: true}
	for index, row := range snapshot.Rows {
		expectedDirection := DirectionRight
		if index%2 == 1 {
			expectedDirection = DirectionLeft
		}
		expectedStartEdge := edgeForDirection(oppositeDirection(expectedDirection))
		if row.Index != index || row.Direction != expectedDirection || row.StartEdge != expectedStartEdge || len(row.TileIDs) == 0 {
			return fmt.Errorf("%w: invalid row %d", ErrInvalidState, index)
		}
		if err := row.StartPosition.Validate(); err != nil {
			return fmt.Errorf("%w: row %d start position: %v", ErrInvalidState, index, err)
		}
		bounds := []float64{row.MinX, row.MaxX, row.MinY, row.MaxY}
		for _, value := range bounds {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("%w: row %d contains non-finite bounds", ErrInvalidState, index)
			}
		}
		if row.MinX > row.MaxX || row.MinY > row.MaxY {
			return fmt.Errorf("%w: row %d has inverted bounds", ErrInvalidState, index)
		}
		if row.EndConfirmed {
			expectedEnd := edgeForDirection(row.Direction)
			if row.EndEdge != expectedEnd {
				return fmt.Errorf("%w: row %d confirmed wrong end edge %q", ErrInvalidState, index, row.EndEdge)
			}
			if expectedEnd == EdgeLeft {
				confirmedHorizontalEdges.Left = true
			} else {
				confirmedHorizontalEdges.Right = true
			}
		} else if row.EndEdge != "" {
			return fmt.Errorf("%w: row %d has an unconfirmed end edge", ErrInvalidState, index)
		}
		if index < len(snapshot.Rows)-1 && !row.EndConfirmed {
			return fmt.Errorf("%w: row %d started a successor before confirming its side boundary", ErrInvalidState, index)
		}

		minX, maxX := math.Inf(1), math.Inf(-1)
		minY, maxY := math.Inf(1), math.Inf(-1)
		for _, tileID := range row.TileIDs {
			tile, exists := seenTiles[tileID]
			if !exists {
				return fmt.Errorf("%w: row %d references unknown tile %q", ErrInvalidState, index, tileID)
			}
			if tile.Row != index {
				return fmt.Errorf("%w: row %d references tile %q assigned to row %d", ErrInvalidState, index, tileID, tile.Row)
			}
			rowReferences[tileID]++
			minX = math.Min(minX, tile.Position.X)
			maxX = math.Max(maxX, tile.Position.X)
			minY = math.Min(minY, tile.Position.Y)
			maxY = math.Max(maxY, tile.Position.Y)
		}
		if !near(row.MinX, minX) || !near(row.MaxX, maxX) || !near(row.MinY, minY) || !near(row.MaxY, maxY) {
			return fmt.Errorf("%w: row %d bounds do not match its tile positions", ErrInvalidState, index)
		}
	}
	for tileID := range seenTiles {
		if rowReferences[tileID] != 1 {
			return fmt.Errorf("%w: tile %q has %d row references", ErrInvalidState, tileID, rowReferences[tileID])
		}
	}
	if snapshot.Direction != snapshot.Rows[snapshot.CurrentRow].Direction {
		return fmt.Errorf("%w: current direction does not match current row", ErrInvalidState)
	}
	if snapshot.ConfirmedEdges.Left != confirmedHorizontalEdges.Left || snapshot.ConfirmedEdges.Right != confirmedHorizontalEdges.Right {
		return fmt.Errorf("%w: confirmed horizontal edges disagree with row evidence", ErrInvalidState)
	}

	calculatedTravel := 0.0
	for index, observation := range snapshot.Observations {
		if err := observation.Validate(observation.Direction, snapshot.Config); err != nil {
			return fmt.Errorf("%w: observation %d: %v", ErrInvalidState, index, err)
		}
		_, hasTile := observationTiles[index]
		moves := observation.Kind == MotionMoved || observation.Kind == MotionPartial
		if moves != hasTile {
			return fmt.Errorf("%w: observation %d movement/tile evidence disagrees", ErrInvalidState, index)
		}
		if moves {
			calculatedTravel += observation.Delta.ManhattanLength()
		}
	}
	if !near(snapshot.Travel, calculatedTravel) {
		return fmt.Errorf("%w: travel %g does not match observed travel %g", ErrInvalidState, snapshot.Travel, calculatedTravel)
	}
	if snapshot.Position != snapshot.Tiles[len(snapshot.Tiles)-1].Position {
		return fmt.Errorf("%w: current position does not match latest accepted tile", ErrInvalidState)
	}

	if snapshot.Status == FrontierClosed {
		if snapshot.Phase != PhaseComplete || !snapshot.ConfirmedEdges.All() {
			return fmt.Errorf("%w: closed frontier requires all edges and complete phase", ErrInvalidState)
		}
	}
	if snapshot.Status == FrontierUnresolved {
		if snapshot.Phase != PhaseUnresolved || snapshot.UnresolvedReason == "" {
			return fmt.Errorf("%w: unresolved status requires unresolved phase and reason", ErrInvalidState)
		}
	} else if snapshot.UnresolvedReason != "" {
		return fmt.Errorf("%w: non-unresolved status contains an unresolved reason", ErrInvalidState)
	}
	if snapshot.Status == FrontierDiscovering && (snapshot.Phase == PhaseComplete || snapshot.Phase == PhaseUnresolved) {
		return fmt.Errorf("%w: discovering status has a terminal phase", ErrInvalidState)
	}
	currentRow := snapshot.Rows[snapshot.CurrentRow]
	if snapshot.Status == FrontierDiscovering {
		if snapshot.Phase == PhaseDescend && !currentRow.EndConfirmed {
			return fmt.Errorf("%w: descend phase requires a confirmed current row", ErrInvalidState)
		}
		if snapshot.Phase == PhaseHorizontal && currentRow.EndConfirmed {
			return fmt.Errorf("%w: horizontal phase cannot retain a confirmed current row", ErrInvalidState)
		}
	}
	if snapshot.Status != FrontierClosed && snapshot.ConfirmedEdges.Bottom {
		return fmt.Errorf("%w: bottom edge can only be confirmed on a closed frontier", ErrInvalidState)
	}
	return nil
}

func AuditClosed(snapshot FrontierSnapshot) error {
	if err := ValidateFrontierSnapshot(snapshot); err != nil {
		return err
	}
	if snapshot.Status != FrontierClosed || !snapshot.ConfirmedEdges.All() {
		return errors.New("frontier is not closed")
	}
	for _, row := range snapshot.Rows {
		if !row.EndConfirmed {
			return fmt.Errorf("row %d does not have a confirmed side boundary", row.Index)
		}
	}
	return nil
}

func (frontier *Frontier) appendTile(id string, row int, observationIndex int) {
	frontier.state.Tiles = append(frontier.state.Tiles, TilePlacement{
		Sequence:         len(frontier.state.Tiles),
		ID:               id,
		Row:              row,
		Position:         frontier.state.Position,
		ObservationIndex: observationIndex,
	})
	frontier.state.Rows[row].TileIDs = append(frontier.state.Rows[row].TileIDs, id)
}

func (frontier *Frontier) confirmCurrentRow(edge Edge) {
	row := &frontier.state.Rows[frontier.state.CurrentRow]
	row.EndEdge = edge
	row.EndConfirmed = true
	switch edge {
	case EdgeLeft:
		frontier.state.ConfirmedEdges.Left = true
	case EdgeRight:
		frontier.state.ConfirmedEdges.Right = true
	}
}

func (frontier *Frontier) updateCurrentRowBounds(position geometry.Point) {
	row := &frontier.state.Rows[frontier.state.CurrentRow]
	row.MinX = math.Min(row.MinX, position.X)
	row.MaxX = math.Max(row.MaxX, position.X)
	row.MinY = math.Min(row.MinY, position.Y)
	row.MaxY = math.Max(row.MaxY, position.Y)
}

func (frontier *Frontier) hasTile(id string) bool {
	for _, tile := range frontier.state.Tiles {
		if tile.ID == id {
			return true
		}
	}
	return false
}

func (frontier *Frontier) checkSafetyLimits() error {
	limits := frontier.state.Config.Limits
	if len(frontier.state.Rows) > limits.MaxRows {
		return fmt.Errorf("%w: rows %d exceed %d", ErrSafetyLimit, len(frontier.state.Rows), limits.MaxRows)
	}
	if len(frontier.state.Tiles) > limits.MaxTiles {
		return fmt.Errorf("%w: tiles %d exceed %d", ErrSafetyLimit, len(frontier.state.Tiles), limits.MaxTiles)
	}
	for _, row := range frontier.state.Rows {
		if len(row.TileIDs) > limits.MaxColumns {
			return fmt.Errorf("%w: row %d columns %d exceed %d", ErrSafetyLimit, row.Index, len(row.TileIDs), limits.MaxColumns)
		}
	}
	if frontier.state.Travel > limits.MaxTravel {
		return fmt.Errorf("%w: travel %g exceeds %g", ErrSafetyLimit, frontier.state.Travel, limits.MaxTravel)
	}
	return nil
}

func (frontier *Frontier) markUnresolved(reason string, cause error, transition Transition) (Transition, error) {
	frontier.state.Status = FrontierUnresolved
	frontier.state.Phase = PhaseUnresolved
	frontier.state.UnresolvedReason = reason
	transition.Revision = frontier.state.Revision
	transition.Status = frontier.state.Status
	transition.Reason = reason
	err := fmt.Errorf("%w: %s", ErrUnresolved, reason)
	if cause != nil {
		err = errors.Join(err, cause)
	}
	return transition, err
}

func newRowSnapshot(index int, direction Direction, startEdge Edge, position geometry.Point, tileID string) RowSnapshot {
	row := RowSnapshot{
		Index:         index,
		Direction:     direction,
		StartEdge:     startEdge,
		StartPosition: position,
		MinX:          position.X,
		MaxX:          position.X,
		MinY:          position.Y,
		MaxY:          position.Y,
	}
	if tileID != "" {
		row.TileIDs = []string{tileID}
	}
	return row
}

func edgeForDirection(direction Direction) Edge {
	if direction == DirectionLeft {
		return EdgeLeft
	}
	if direction == DirectionRight {
		return EdgeRight
	}
	return EdgeBottom
}

func oppositeDirection(direction Direction) Direction {
	if direction == DirectionLeft {
		return DirectionRight
	}
	return DirectionLeft
}

func cloneSnapshot(snapshot FrontierSnapshot) FrontierSnapshot {
	clone := snapshot
	clone.Rows = make([]RowSnapshot, len(snapshot.Rows))
	for index, row := range snapshot.Rows {
		clone.Rows[index] = row
		clone.Rows[index].TileIDs = append([]string(nil), row.TileIDs...)
	}
	clone.Tiles = append([]TilePlacement(nil), snapshot.Tiles...)
	clone.Observations = make([]MotionObservation, len(snapshot.Observations))
	for index, observation := range snapshot.Observations {
		clone.Observations[index] = cloneObservation(observation)
	}
	return clone
}

func cloneObservation(observation MotionObservation) MotionObservation {
	clone := observation
	clone.EvidenceIDs = append([]string(nil), observation.EvidenceIDs...)
	return clone
}

func near(left, right float64) bool {
	return math.Abs(left-right) <= 1e-9
}
