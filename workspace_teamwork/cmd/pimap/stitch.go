package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/capture"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/optimizer"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/project"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/quality"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/registration"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/stitch"
)

func runStitch(ctx context.Context, globalOpts GlobalOptions, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stitch", flag.ContinueOnError)
	if globalOpts.JSON {
		fs.SetOutput(io.Discard)
	} else {
		fs.SetOutput(stderr)
	}

	projectDir := fs.String("project", "", "Path to captured project directory")
	layerID := fs.String("layer", "overview", "Layer ID to stitch")
	outputFile := fs.String("output", "", "Optional custom output path for stitched panorama PNG")
	featherRadius := fs.Int("feather", 16, "Feather blend radius in pixels")

	if err := fs.Parse(args); err != nil {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{
				"error": err.Error(),
				"code":  ExitCodeInvalidArgs,
			})
		}
		return ExitCodeInvalidArgs
	}

	if *projectDir == "" {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": "--project flag is required"})
		} else {
			fmt.Fprintf(stderr, "error: --project flag is required\n")
		}
		return ExitCodeInvalidArgs
	}

	store, err := project.NewStore(*projectDir)
	if err != nil {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("open project store: %v", err)})
		} else {
			fmt.Fprintf(stderr, "error: open project store: %v\n", err)
		}
		return ExitCodeCorruptSession
	}

	session, err := store.Resume(ctx)
	if err != nil {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("resume project session: %v", err)})
		} else {
			fmt.Fprintf(stderr, "error: resume project session: %v\n", err)
		}
		return ExitCodeCorruptSession
	}

	activePlan := session.ActivePlan()
	if activePlan == nil {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": "no active plan found in project"})
		} else {
			fmt.Fprintf(stderr, "error: no active plan found in project\n")
		}
		return ExitCodeCorruptSession
	}

	frontier := activePlan.Frontier
	if len(frontier.Tiles) == 0 {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": "no tiles found in project plan"})
		} else {
			fmt.Fprintf(stderr, "error: no tiles found in project plan\n")
		}
		return ExitCodeCorruptSession
	}

	// Load tile images first
	tileImages := make(map[string]image.Image, len(frontier.Tiles))
	for _, tile := range frontier.Tiles {
		relPath := fmt.Sprintf("layers/%s/tiles/%s.png", *layerID, tile.ID)
		absPath := filepath.Join(session.Root(), filepath.FromSlash(relPath))
		f, err := os.Open(absPath)
		if err != nil {
			if globalOpts.JSON {
				_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("open tile %s: %v", tile.ID, err)})
			} else {
				fmt.Fprintf(stderr, "error opening tile %s: %v\n", tile.ID, err)
			}
			return ExitCodeGeneralError
		}
		img, err := png.Decode(f)
		_ = f.Close()
		if err != nil {
			if globalOpts.JSON {
				_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("decode tile %s: %v", tile.ID, err)})
			} else {
				fmt.Fprintf(stderr, "error decoding tile %s: %v\n", tile.ID, err)
			}
			return ExitCodeGeneralError
		}
		tileImages[tile.ID] = img
	}

	// Solve pose graph
	graph := optimizer.NewPoseGraph()
	for _, tile := range frontier.Tiles {
		graph.AddNode(tile.ID, tile.Position)
	}
	_ = graph.SetAnchor(frontier.Tiles[0].ID)

	var adjacencies []project.TileAdjacency
	if activePlan.CoverageAudit != nil && len(activePlan.CoverageAudit.VerifiedAdjacencies) > 0 {
		adjacencies = activePlan.CoverageAudit.VerifiedAdjacencies
	} else {
		audit, _ := quality.AuditCoverage(frontier, project.OverlapRange{Minimum: 0.05, Maximum: 0.99})
		adjacencies = audit.VerifiedAdjacencies
	}

	for _, adj := range adjacencies {
		imgA, okA := tileImages[adj.FromTile]
		imgB, okB := tileImages[adj.ToTile]
		if okA && okB {
			regRes, err := registration.ComputePhaseCorrelation(imgA, imgB, registration.DefaultConfig())
			if err == nil && regRes.Confidence > 0.3 {
				weight := math.Max(0.1, regRes.Confidence*10.0)
				relDelta := geometry.Vector{X: -regRes.Delta.X, Y: -regRes.Delta.Y}
				graph.AddEdge(adj.FromTile, adj.ToTile, relDelta, weight)
				continue
			}
		}
		tileA := findTileInPlacements(frontier.Tiles, adj.FromTile)
		tileB := findTileInPlacements(frontier.Tiles, adj.ToTile)
		if tileA != nil && tileB != nil {
			nominalDelta := tileB.Position.Sub(tileA.Position)
			graph.AddEdge(adj.FromTile, adj.ToTile, nominalDelta, 1.0)
		}
	}

	solvedPoses, _, err := graph.SolveWithOptions(optimizer.DefaultSolverOptions())
	if err != nil {
		solvedPoses = make(map[string]geometry.Point, len(frontier.Tiles))
		for _, tile := range frontier.Tiles {
			solvedPoses[tile.ID] = tile.Position
		}
	}

	stitchTiles := make([]stitch.StitchTile, 0, len(frontier.Tiles))
	for _, tile := range frontier.Tiles {
		img := tileImages[tile.ID]
		optPose, ok := solvedPoses[tile.ID]
		if !ok {
			optPose = tile.Position
		}

		b := img.Bounds()
		stitchTiles = append(stitchTiles, stitch.StitchTile{
			ID:    tile.ID,
			Row:   tile.Row,
			Col:   tile.Sequence,
			Image: img,
			Pose:  optPose,
			Size:  geometry.Size{Width: float64(b.Dx()), Height: float64(b.Dy())},
		})
	}

	opts := stitch.DefaultStitchOptions()
	if *featherRadius > 0 {
		opts.FeatherWidth = *featherRadius
	}
	stitcher := stitch.NewStitcher(opts)

	if !globalOpts.JSON {
		fmt.Fprintf(stdout, "Stitching %d tiles from layer [%s]...\n", len(stitchTiles), *layerID)
	}

	stitchRes, err := stitcher.Stitch(ctx, stitchTiles)
	if err != nil {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("stitch failed: %v", err)})
		} else {
			fmt.Fprintf(stderr, "error stitching canvas: %v\n", err)
		}
		return ExitCodeGeneralError
	}

	targetDir := filepath.Join(session.Root(), "layers", *layerID)
	panoPath, prevPath, err := stitcher.ExportFiles(stitchRes, targetDir)
	if err != nil {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("export files: %v", err)})
		} else {
			fmt.Fprintf(stderr, "error exporting stitched files: %v\n", err)
		}
		return ExitCodeGeneralError
	}

	if *outputFile != "" {
		if err := copyFile(panoPath, *outputFile); err != nil {
			if globalOpts.JSON {
				_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("copy output file: %v", err)})
			} else {
				fmt.Fprintf(stderr, "error copying output to %s: %v\n", *outputFile, err)
			}
			return ExitCodeGeneralError
		}
		panoPath = *outputFile
	}

	if globalOpts.JSON {
		_ = outputJSON(stdout, map[string]any{
			"status":        "success",
			"project_dir":   *projectDir,
			"layer_id":      *layerID,
			"tile_count":    len(stitchTiles),
			"canvas_width":  int(stitchRes.Bounds.Width),
			"canvas_height": int(stitchRes.Bounds.Height),
			"panorama_path": panoPath,
			"preview_path":  prevPath,
		})
	} else {
		fmt.Fprintf(stdout, "\nStitching complete successfully!\n")
		fmt.Fprintf(stdout, "Canvas Dimensions: %dx%d\n", int(stitchRes.Bounds.Width), int(stitchRes.Bounds.Height))
		fmt.Fprintf(stdout, "Panorama Export:   %s\n", panoPath)
		fmt.Fprintf(stdout, "Preview Export:    %s\n", prevPath)
	}

	return ExitCodeSuccess
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func findTileInPlacements(tiles []capture.TilePlacement, id string) *capture.TilePlacement {
	for i := range tiles {
		if tiles[i].ID == id {
			return &tiles[i]
		}
	}
	return nil
}
