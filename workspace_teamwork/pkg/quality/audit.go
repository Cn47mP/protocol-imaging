package quality

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/capture"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/project"
)

const CoverageAlgorithmID = "planned_measured_coverage_v2"

type RegistrationConstraintEvaluator func(fromTileID, toTileID, axis string) (confidence float64, ok bool)

type AuditConfig struct {
	RequiredAdjacencies []project.TileAdjacency
	RequiredOverlap     project.OverlapRange
	TileSize            geometry.Size
	MinConfidence       float64
	TileFileChecker     func(tileID string) bool
	ConstraintEvaluator RegistrationConstraintEvaluator
}

func DefaultAuditConfig() AuditConfig {
	return AuditConfig{
		RequiredOverlap: project.OverlapRange{Minimum: 0.05, Maximum: 0.99},
		MinConfidence:   0.6,
	}
}

type CoverageAuditor interface {
	AuditCoverage(frontier capture.FrontierSnapshot, requiredOverlap project.OverlapRange) (project.CoverageAudit, error)
	AuditPlanCoverage(plan project.PlanDocument, config AuditConfig) (project.CoverageAudit, error)
}

func AuditCoverage(frontier capture.FrontierSnapshot, requiredOverlap project.OverlapRange) (project.CoverageAudit, error) {
	plan := project.PlanDocument{
		SchemaVersion:       project.PlanSchemaVersion,
		ID:                  fmt.Sprintf("plan-%04d", frontier.Revision),
		Frontier:            frontier,
		RequiredAdjacencies: project.RequiredAdjacenciesFromFrontier(frontier),
		AcceptableOverlap:   requiredOverlap,
	}
	return AuditPlanCoverage(plan, AuditConfig{RequiredOverlap: requiredOverlap})
}

// AuditPlanCoverage is deliberately a two-gate audit. It first proves that the
// closed discovery plan and every referenced tile exist. If a constraint
// evaluator is supplied, it then requires measured registration evidence for
// every required adjacency; capture positions alone never satisfy that gate.
func AuditPlanCoverage(plan project.PlanDocument, config AuditConfig) (project.CoverageAudit, error) {
	audit := project.CoverageAudit{Algorithm: CoverageAlgorithmID, AuditedAt: time.Now().UTC()}
	if err := capture.AuditClosed(plan.Frontier); err != nil {
		return audit, fmt.Errorf("frontier closure verification failed: %w", err)
	}

	overlap := config.RequiredOverlap
	if overlap.Minimum == 0 && overlap.Maximum == 0 {
		overlap = plan.AcceptableOverlap
	}
	if err := overlap.Validate(); err != nil {
		return audit, fmt.Errorf("invalid required overlap: %w", err)
	}

	frontier := plan.Frontier
	if len(frontier.Rows) == 0 || len(frontier.Tiles) == 0 {
		return audit, errors.New("closed frontier contains no rows or tiles")
	}
	tiles := make(map[string]capture.TilePlacement, len(frontier.Tiles))
	for _, tile := range frontier.Tiles {
		tiles[tile.ID] = tile
	}
	rowReferences := make(map[string]int, len(tiles))
	for _, row := range frontier.Rows {
		if len(row.TileIDs) == 0 || !row.EndConfirmed {
			return audit, nil
		}
		for _, tileID := range row.TileIDs {
			rowReferences[tileID]++
		}
	}
	for _, tile := range frontier.Tiles {
		missing := rowReferences[tile.ID] != 1
		if config.TileFileChecker != nil && !config.TileFileChecker(tile.ID) {
			missing = true
		}
		if missing {
			audit.MissingTileIDs = append(audit.MissingTileIDs, tile.ID)
		} else {
			audit.CoveredTileIDs = append(audit.CoveredTileIDs, tile.ID)
		}
	}
	sort.Strings(audit.CoveredTileIDs)
	sort.Strings(audit.MissingTileIDs)

	required := plan.RequiredAdjacencies
	if len(config.RequiredAdjacencies) > 0 {
		required = config.RequiredAdjacencies
	}
	if len(required) == 0 {
		required = project.RequiredAdjacenciesFromFrontier(frontier)
	}

	tileWidth, tileHeight := config.TileSize.Width, config.TileSize.Height
	if tileWidth <= 0 {
		tileWidth = frontier.Config.HorizontalStep / (1 - (overlap.Minimum+overlap.Maximum)/2)
	}
	if tileHeight <= 0 {
		tileHeight = frontier.Config.VerticalStep / (1 - (overlap.Minimum+overlap.Maximum)/2)
	}
	minConfidence := config.MinConfidence
	if minConfidence <= 0 {
		minConfidence = 0.6
	}

	allConstraintsAccepted := true
	for _, adjacency := range required {
		from, fromExists := tiles[adjacency.FromTile]
		to, toExists := tiles[adjacency.ToTile]
		if !fromExists || !toExists || (adjacency.Axis != "horizontal" && adjacency.Axis != "vertical") {
			allConstraintsAccepted = false
			continue
		}
		span, delta := tileWidth, math.Abs(to.Position.X-from.Position.X)
		if adjacency.Axis == "vertical" {
			span, delta = tileHeight, math.Abs(to.Position.Y-from.Position.Y)
		}
		actualOverlap := 1 - delta/span
		if span <= 0 || actualOverlap < overlap.Minimum-1e-6 || actualOverlap > overlap.Maximum+1e-6 {
			allConstraintsAccepted = false
			continue
		}
		if config.ConstraintEvaluator != nil {
			confidence, ok := config.ConstraintEvaluator(adjacency.FromTile, adjacency.ToTile, adjacency.Axis)
			if !ok || confidence < minConfidence {
				allConstraintsAccepted = false
				continue
			}
		}
		audit.VerifiedAdjacencies = append(audit.VerifiedAdjacencies, adjacency)
	}

	audit.Passed = len(audit.MissingTileIDs) == 0 &&
		len(audit.CoveredTileIDs) == len(frontier.Tiles) &&
		frontier.ConfirmedEdges.All() &&
		allConstraintsAccepted &&
		len(audit.VerifiedAdjacencies) == len(required)
	if audit.Passed {
		audit.Confidence = 1
	}
	return audit, nil
}

func ValidateObservedLocalEligibility(plan project.PlanDocument, boundary project.BoundaryDocument) error {
	if plan.Frontier.Status != capture.FrontierClosed {
		return errors.New("cannot promote to observed_local: dynamic frontier is not closed")
	}
	if !plan.Frontier.ConfirmedEdges.All() {
		return errors.New("cannot promote to observed_local: not all 4 edges are confirmed")
	}
	if plan.CoverageAudit == nil || !plan.CoverageAudit.Passed {
		return errors.New("cannot promote to observed_local: measured coverage audit has not passed")
	}
	if len(plan.CoverageAudit.MissingTileIDs) != 0 {
		return errors.New("cannot promote to observed_local: missing tile IDs reported")
	}
	return nil
}

func adjacencyKey(from, to, axis string) string {
	ends := []string{from, to}
	sort.Strings(ends)
	return ends[0] + "\x00" + ends[1] + "\x00" + axis
}
