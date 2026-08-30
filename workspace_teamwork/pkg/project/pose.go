package project

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
)

const PoseSchemaVersion = 1

type PoseDocument struct {
	SchemaVersion int                 `json:"schema_version"`
	GeneratedAt   time.Time           `json:"generated_at"`
	Poses         []TilePose          `json:"poses"`
	Constraints   []AdjacencyEvidence `json:"constraints"`
}

type TilePose struct {
	TileID   string         `json:"tile_id"`
	Position geometry.Point `json:"position"`
}

type AdjacencyEvidence struct {
	FromTile         string          `json:"from_tile"`
	ToTile           string          `json:"to_tile"`
	Axis             string          `json:"axis"`
	Translation      geometry.Vector `json:"measured_translation"`
	Confidence       float64         `json:"confidence"`
	Residual         float64         `json:"residual"`
	Rejected         bool            `json:"rejected"`
	EvidenceFrameIDs []string        `json:"evidence_frame_ids"`
}

func (session *Session) WritePoseDocument(ctx context.Context, document PoseDocument) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode poses: %w", err)
	}
	data = append(data, '\n')
	target := filepath.Join(session.Root(), "poses.json")
	temp := target + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return fmt.Errorf("write temporary poses: %w", err)
	}
	if err := os.Rename(temp, target); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("replace poses: %w", err)
	}
	return nil
}
