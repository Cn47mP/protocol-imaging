package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"os"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/adapter"
)

type cliPreflightReporter struct {
	stdout io.Writer
	stderr io.Writer
	opts   GlobalOptions
}

func (r *cliPreflightReporter) Phase(name string) {
	if !r.opts.JSON {
		fmt.Fprintf(r.stdout, "--> Running probe phase: %s\n", name)
	}
}

func (r *cliPreflightReporter) Progress(current, total int, msg string) {
	if r.opts.Verbose && !r.opts.JSON {
		fmt.Fprintf(r.stdout, "    [%d/%d] %s\n", current, total, msg)
	}
}

func (r *cliPreflightReporter) TileStatus(id string, status string) {}

func (r *cliPreflightReporter) Warning(msg string) {
	if !r.opts.JSON {
		fmt.Fprintf(r.stderr, "    [WARN] %s\n", msg)
	}
}

func runPreflight(ctx context.Context, globalOpts GlobalOptions, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("preflight", flag.ContinueOnError)
	if globalOpts.JSON {
		fs.SetOutput(io.Discard)
	} else {
		fs.SetOutput(stderr)
	}

	windowName := fs.String("window", "Endfield", "Target process window name")
	windowTitle := fs.String("title", "明日方舟：终末地", "Target window class/title prefix")
	quick := fs.Bool("quick", false, "Run only essential capability probes (skip freshness & mapping)")
	minDisp := fs.Float64("min-disp", 5.0, "Minimum displacement threshold in pixels for freshness probe")
	driverChoice := fs.String("driver", "maaend", "Offline CLI backend; only 'mock' is supported (live preflight runs inside MaaEnd)")
	mockCanvasPath := fs.String("mock-canvas", "", "Path to synthetic PNG image to use MockController instead of native window")

	if err := fs.Parse(args); err != nil {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{
				"error": err.Error(),
				"code":  ExitCodeInvalidArgs,
			})
		}
		return ExitCodeInvalidArgs
	}

	cfg := adapter.DefaultPreflightConfig()
	if *minDisp > 0 {
		cfg.Freshness.MinDisplacement = *minDisp
	}
	if *quick {
		cfg.SkipFreshness = true
		cfg.SkipMapping = true
	}

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

		// Adjust minimum resolution and drag parameters for test mock canvas
		cfg.Resolution.MinWidth = 100
		cfg.Resolution.MinHeight = 100
		cfg.Freshness.DragDistance = 50
		cfg.Freshness.MinDisplacement = 5
		cfg.Mapping.ProbeDistance = 40
	} else {
		message := fmt.Sprintf("failed to connect to MaaFramework runtime: standalone online preflight backend %q was removed; run preflight from the MaaEnd custom action", *driverChoice)
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": message, "code": ExitCodeProbeNoGo})
		} else {
			fmt.Fprintf(stderr, "error: %s (target flags %q/%q are ignored)\n", message, *windowTitle, *windowName)
		}
		return ExitCodeProbeNoGo
	}

	reporter := &cliPreflightReporter{
		stdout: stdout,
		stderr: stderr,
		opts:   globalOpts,
	}

	if !globalOpts.JSON {
		fmt.Fprintf(stdout, "Starting Preflight Probes on target...\n\n")
	}

	report, err := adapter.RunPreflightProbes(ctx, ctrl, reporter, cfg)
	if err != nil {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": err.Error(), "report": report})
		} else {
			fmt.Fprintf(stderr, "\nPreflight execution failed: %v\n", err)
		}
		return ExitCodeProbeNoGo
	}

	if globalOpts.JSON {
		_ = outputJSON(stdout, report)
	} else {
		fmt.Fprintf(stdout, "\n================ PREFLIGHT SUMMARY ================\n")
		fmt.Fprintf(stdout, "Verdict: %s\n", report.Verdict)
		fmt.Fprintf(stdout, "Overall Pass: %v\n", report.OverallPass)
		fmt.Fprintf(stdout, "Timestamp: %s\n", report.Timestamp.Format("2006-01-02T15:04:05Z07:00"))
		fmt.Fprintf(stdout, "---------------------------------------------------\n")
		for name, probe := range report.Probes {
			passStr := "FAIL"
			if probe.Passed {
				passStr = "PASS"
			}
			fmt.Fprintf(stdout, "[%-4s] %-25s (%v): %s\n", passStr, name, probe.Latency, probe.Details)
		}
		fmt.Fprintf(stdout, "===================================================\n")
	}

	if report.Verdict == adapter.ProbeStatusGo && report.OverallPass {
		return ExitCodeSuccess
	}
	return ExitCodeProbeNoGo
}
