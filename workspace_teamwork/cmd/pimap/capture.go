package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/adapter"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/pipeline"
)

type cliProgressListener struct {
	stdout io.Writer
	stderr io.Writer
	opts   GlobalOptions
}

func (l *cliProgressListener) OnStageTransition(from, to pipeline.PipelineStage) {
	if !l.opts.JSON {
		fmt.Fprintf(l.stdout, "==> Stage Transition: [%s] -> [%s]\n", from, to)
	}
}

func (l *cliProgressListener) OnProgress(snapshot pipeline.ProgressSnapshot) {
	if l.opts.Verbose && !l.opts.JSON {
		if snapshot.TotalTiles > 0 {
			fmt.Fprintf(l.stdout, "    [%s] Tile: %s | Rows: %d | Total Tiles: %d | %s\n",
				snapshot.Stage, snapshot.CurrentTileID, snapshot.DiscoveredRows, snapshot.TotalTiles, snapshot.Message)
		} else {
			fmt.Fprintf(l.stdout, "    [%s] %s\n", snapshot.Stage, snapshot.Message)
		}
	}
}

func (l *cliProgressListener) OnTileStatus(tileID string, status string) {
	if l.opts.Verbose && !l.opts.JSON {
		fmt.Fprintf(l.stdout, "    Tile %s: %s\n", tileID, status)
	}
}

func (l *cliProgressListener) OnLog(level, message string) {
	if l.opts.Verbose && !l.opts.JSON {
		fmt.Fprintf(l.stdout, "    [%s] %s\n", level, message)
	}
}

func (l *cliProgressListener) OnWarning(message string) {
	if !l.opts.JSON {
		fmt.Fprintf(l.stderr, "    [WARN] %s\n", message)
	}
}

func (l *cliProgressListener) OnPreflightReport(report adapter.PreflightReport) {
	if !l.opts.JSON {
		fmt.Fprintf(l.stdout, "    Preflight Verdict: %s (Pass: %v)\n",
			report.Verdict, report.OverallPass)
	}
}

func runCapture(ctx context.Context, globalOpts GlobalOptions, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	if globalOpts.JSON {
		fs.SetOutput(io.Discard)
	} else {
		fs.SetOutput(stderr)
	}

	projectDir := fs.String("project", "", "Target project directory")
	archivePath := fs.String("archive", "", "Target archive .pimap container file path (auto-pack on success)")
	skipPreflight := fs.Bool("skip-preflight", false, "Skip initial preflight Go/No-Go capability probes")
	resume := fs.Bool("resume", false, "Resume from an interrupted capture session in project directory")
	windowName := fs.String("window", "Endfield", "Target process window name")
	windowTitle := fs.String("title", "明日方舟：终末地", "Target window class/title prefix")
	mockCanvasPath := fs.String("mock-canvas", "", "Path to synthetic PNG image to use MockController")
	driverChoice := fs.String("driver", "maaend", "Offline CLI backend; only 'mock' is supported (live capture runs inside MaaEnd)")

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
		// Generate default project directory with timestamp
		*projectDir = filepath.Join(".", fmt.Sprintf("pimap-project-%d", time.Now().Unix()))
	}

	cfg := pipeline.DefaultPipelineConfig(*projectDir)
	cfg.ArchivePath = *archivePath
	cfg.SkipPreflight = *skipPreflight
	cfg.Resume = *resume

	var ctrl adapter.Controller
	if *mockCanvasPath != "" || *driverChoice == "mock" {
		if *mockCanvasPath == "" {
			if globalOpts.JSON {
				_ = outputJSON(stderr, map[string]any{"error": "--mock-canvas path is required when using mock driver", "code": ExitCodeInvalidArgs})
			} else {
				fmt.Fprintf(stderr, "error: --mock-canvas path is required when using mock driver\n")
			}
			return ExitCodeInvalidArgs
		}
		f, err := os.Open(*mockCanvasPath)
		if err != nil {
			if globalOpts.JSON {
				_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("open mock canvas: %v", err), "code": ExitCodeInvalidArgs})
			} else {
				fmt.Fprintf(stderr, "error: open mock canvas: %v\n", err)
			}
			return ExitCodeInvalidArgs
		}
		img, _, err := image.Decode(f)
		_ = f.Close()
		if err != nil {
			if globalOpts.JSON {
				_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("decode mock canvas: %v", err), "code": ExitCodeInvalidArgs})
			} else {
				fmt.Fprintf(stderr, "error: decode mock canvas: %v\n", err)
			}
			return ExitCodeInvalidArgs
		}

		b := img.Bounds()
		mockCtrl, err := adapter.NewMockController(adapter.MockControllerConfig{
			Canvas:         img,
			CanvasWidth:    b.Dx(),
			CanvasHeight:   b.Dy(),
			ViewportWidth:  200,
			ViewportHeight: 150,
		})
		if err != nil {
			if globalOpts.JSON {
				_ = outputJSON(stderr, map[string]any{"error": err.Error(), "code": ExitCodeGeneralError})
			} else {
				fmt.Fprintf(stderr, "error creating mock controller: %v\n", err)
			}
			return ExitCodeGeneralError
		}
		ctrl = mockCtrl

		// Adjust pipeline parameters for miniature mock canvases in test runs
		cfg.EngineConfig.FrontierConfig.HorizontalStep = 80
		cfg.EngineConfig.FrontierConfig.VerticalStep = 60
		cfg.EngineConfig.FrontierConfig.ProbeStep = 20
		cfg.EngineConfig.FrontierConfig.MinimumConfidence = 0.4
		cfg.EngineConfig.HomingConfig.DragDistance = 40
		cfg.EngineConfig.HomingConfig.DragDuration = 5 * time.Millisecond
		cfg.EngineConfig.HomingConfig.SettlingDelay = 0
		cfg.EngineConfig.HomingConfig.MinimumConfidence = 0.4
		cfg.EngineConfig.CalibratorConfig.ProbeDistance = 40
		cfg.EngineConfig.CalibratorConfig.ProbeDuration = 5 * time.Millisecond
		cfg.EngineConfig.CalibratorConfig.SettlingDelay = 0
		cfg.EngineConfig.CalibratorConfig.MinimumConfidence = 0.4
		cfg.MinOverlap = 0.05
		cfg.MaxOverlap = 0.99
	} else {
		message := fmt.Sprintf("failed to connect to MaaFramework runtime: standalone online capture backend %q was removed; start capture from the MaaEnd custom action", *driverChoice)
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": message, "code": ExitCodeProbeNoGo})
		} else {
			fmt.Fprintf(stderr, "error: %s (target flags %q/%q are ignored)\n", message, *windowTitle, *windowName)
		}
		return ExitCodeProbeNoGo
	}

	listener := &cliProgressListener{
		stdout: stdout,
		stderr: stderr,
		opts:   globalOpts,
	}

	pipe := pipeline.NewPipeline(cfg, listener)

	if !globalOpts.JSON {
		fmt.Fprintf(stdout, "Initiating Capture Pipeline (Project: %s)...\n\n", *projectDir)
	}

	result, err := pipe.Execute(ctx, ctrl)
	if err != nil {
		var pErr *pipeline.PipelineError
		exitCode := ExitCodeGeneralError
		if errors.As(err, &pErr) {
			switch pErr.Classification {
			case pipeline.ErrClassPreflightNoGo:
				exitCode = ExitCodeProbeNoGo
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
			fmt.Fprintf(stderr, "\nCapture Pipeline Failed at stage [%s]: %v\n", pipe.CurrentStage(), err)
		}
		return exitCode
	}

	if globalOpts.JSON {
		_ = outputJSON(stdout, result)
	} else {
		fmt.Fprintf(stdout, "\n================ CAPTURE COMPLETE ================\n")
		fmt.Fprintf(stdout, "Project ID:        %s\n", result.ProjectID)
		fmt.Fprintf(stdout, "Project Directory: %s\n", result.ProjectDir)
		fmt.Fprintf(stdout, "Total Tiles:       %d\n", result.TileCount)
		fmt.Fprintf(stdout, "Coverage Audit:    %s (Pass: %v)\n", result.CoverageAudit.Algorithm, result.CoverageAudit.Passed)
		fmt.Fprintf(stdout, "Panorama Image:    %s\n", result.PanoramaPath)
		fmt.Fprintf(stdout, "Preview Image:     %s\n", result.PreviewPath)
		if result.ArchivePath != "" {
			fmt.Fprintf(stdout, "Archive Container: %s\n", result.ArchivePath)
		}
		fmt.Fprintf(stdout, "Duration:          %v\n", result.Duration)
		fmt.Fprintf(stdout, "===================================================\n")
	}

	return ExitCodeSuccess
}
