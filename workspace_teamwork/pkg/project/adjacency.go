package project

import (
	"math"
	"sort"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/capture"
)

// RequiredAdjacenciesFromFrontier derives a topology from actual observed tile
// positions. Adjacent rows are linked by nearest horizontal intervals, so
// irregular row lengths do not create false holes or rely on column indexes.
func RequiredAdjacenciesFromFrontier(frontier capture.FrontierSnapshot) []TileAdjacency {
	byID := make(map[string]capture.TilePlacement, len(frontier.Tiles))
	for _, tile := range frontier.Tiles {
		byID[tile.ID] = tile
	}

	rows := append([]capture.RowSnapshot(nil), frontier.Rows...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Index < rows[j].Index })
	result := make([]TileAdjacency, 0, len(frontier.Tiles)*2)
	seen := make(map[string]struct{})
	add := func(from, to, axis string) {
		if from == "" || to == "" || from == to {
			return
		}
		adj := TileAdjacency{FromTile: from, ToTile: to, Axis: axis}
		key := adjacencyKey(adj)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		result = append(result, adj)
	}

	orderedIDs := func(row capture.RowSnapshot) []string {
		ids := append([]string(nil), row.TileIDs...)
		sort.Slice(ids, func(i, j int) bool { return byID[ids[i]].Position.X < byID[ids[j]].Position.X })
		return ids
	}
	for rowIndex, row := range rows {
		ids := orderedIDs(row)
		for i := 0; i+1 < len(ids); i++ {
			add(ids[i], ids[i+1], "horizontal")
		}
		if rowIndex+1 >= len(rows) {
			continue
		}
		below := orderedIDs(rows[rowIndex+1])
		// Horizontal chains already prove every tile within each row. One
		// measured bridge between consecutive rows is the minimal required
		// topology; demanding every possible vertical overlap creates cycles
		// and turns redundant seams into false completeness failures.
		bestFrom, bestTo := "", ""
		bestDistance := math.Inf(1)
		for _, fromID := range ids {
			for _, toID := range below {
				distance := math.Abs(byID[fromID].Position.X - byID[toID].Position.X)
				if distance < bestDistance {
					bestDistance = distance
					bestFrom, bestTo = fromID, toID
				}
			}
		}
		maxDistance := frontier.Config.HorizontalStep * 1.25
		if maxDistance <= 0 || bestDistance <= maxDistance {
			add(bestFrom, bestTo, "vertical")
		}
	}
	return result
}
