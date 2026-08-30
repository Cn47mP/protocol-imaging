package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/adapter"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/pipeline"
)

func runReplay(ctx context.Context, globalOpts GlobalOptions, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	if globalOpts.JSON {
		fs.SetOutput(io.Discard)
	} else {
		fs.SetOutput(stderr)
	}

	traceDir := fs.String("trace", "", "Path to recorded session trace directory (containing controller-recording.json)")
	projectDir := fs.String("project", "", "Destination project directory")
	speedMultiplier := fs.Float64("speed", 1.0, "Replay execution speed multiplier")

	if err := fs.Parse(args); err != nil {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{
				"error": err.Error(),
				"code":  ExitCodeInvalidArgs,
			})
		}
		return ExitCodeInvalidArgs
	}

	if *traceDir == "" {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": "--trace flag is required"})
		} else {
			fmt.Fprintf(stderr, "error: --trace flag is required\n")
		}
		return ExitCodeInvalidArgs
	}

	if *projectDir == "" {
		*projectDir = filepath.Join(".", fmt.Sprintf("pimap-replay-%d", time.Now().Unix()))
	}

	replayCtrl, err := adapter.NewReplayController(*traceDir, adapter.DefaultReplayOptions())
	if err != nil {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("open replay trace: %v", err)})
		} else {
			fmt.Fprintf(stderr, "error opening replay trace %s: %v\n", *traceDir, err)
		}
		return ExitCodeInvalidArgs
	}
	_ = speedMultiplier

	cfg := pipeline.DefaultPipelineConfig(*projectDir)
	cfg.SkipPreflight = true // Deterministic replay uses recorded frame stream

	// Inspect recorded operations to adapt tolerances if viewport was smaller than standard 1080p
	if recBytes, err := os.ReadFile(filepath.Join(*traceDir, "controller-recording.json")); err == nil {
		var rec adapter.ControllerRecording
		if err := json.Unmarshal(recBytes, &rec); err == nil {
			isSmallViewport := false
			for _, op := range rec.Operations {
				if op.Viewport != nil && (op.Viewport.Width <= 600 || op.Viewport.Height <= 600) {
					isSmallViewport = true
					break
				}
				if op.Frame != nil && (op.Frame.Size.Width <= 600 || op.Frame.Size.Height <= 600) {
					isSmallViewport = true
					break
				}
			}
			if isSmallViewport {
				cfg.EngineConfig.FrontierConfig.HorizontalStep = 100
				cfg.EngineConfig.FrontierConfig.VerticalStep = 80
				cfg.EngineConfig.FrontierConfig.ProbeStep = 30
				cfg.EngineConfig.FrontierConfig.MinimumConfidence = 0.3
				cfg.EngineConfig.HomingConfig.DragDistance = 50
				cfg.EngineConfig.HomingConfig.DragDuration = 5 * time.Millisecond
				cfg.EngineConfig.HomingConfig.SettlingDelay = 0
				cfg.EngineConfig.HomingConfig.MinimumConfidence = 0.3
				cfg.EngineConfig.CalibratorConfig.ProbeDistance = 40
				cfg.EngineConfig.CalibratorConfig.ProbeDuration = 5 * time.Millisecond
				cfg.EngineConfig.CalibratorConfig.SettlingDelay = 0
				cfg.EngineConfig.CalibratorConfig.MinimumConfidence = 0.3
				cfg.MinOverlap = 0.05
				cfg.MaxOverlap = 0.99
			}
		}
	}

	listener := &cliProgressListener{
		stdout: stdout,
		stderr: stderr,
		opts:   globalOpts,
	}

	if !globalOpts.JSON {
		fmt.Fprintf(stdout, "Replaying trace from %s into project %s...\n", *traceDir, *projectDir)
	}

	pipe := pipeline.NewPipeline(cfg, listener)
	result, err := pipe.Execute(ctx, replayCtrl)
	if err != nil {
		var pErr *pipeline.PipelineError
		exitCode := ExitCodeGeneralError
		if errors.As(err, &pErr) {
			switch pErr.Classification {
			case pipeline.ErrClassCorruptProject:
				exitCode = ExitCodeCorruptSession
			case pipeline.ErrClassCancelled:
				exitCode = ExitCodeCancelled
			default:
				exitCode = ExitCodeGeneralError
			}
		}

		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{
				"error":     err.Error(),
				"exit_code": exitCode,
				"stage":     pipe.CurrentStage(),
			})
		} else {
			fmt.Fprintf(stderr, "Replay failed at stage [%s]: %v\n", pipe.CurrentStage(), err)
		}
		return exitCode
	}

	if globalOpts.JSON {
		_ = outputJSON(stdout, result)
	} else {
		fmt.Fprintf(stdout, "\nReplay completed successfully!\n")
		fmt.Fprintf(stdout, "Project ID:     %s\n", result.ProjectID)
		fmt.Fprintf(stdout, "Tile Count:     %d\n", result.TileCount)
		fmt.Fprintf(stdout, "Panorama Image: %s\n", result.PanoramaPath)
		fmt.Fprintf(stdout, "Preview Image:  %s\n", result.PreviewPath)
	}

	return ExitCodeSuccess
}
