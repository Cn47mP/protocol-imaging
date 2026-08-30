package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	ExitCodeSuccess          = 0
	ExitCodeGeneralError     = 1
	ExitCodeInvalidArgs      = 2
	ExitCodeProbeNoGo        = 3
	ExitCodeValidationFailed = 4
	ExitCodeCorruptSession   = 5
	ExitCodeCancelled        = 5
)

const Version = "v1.0.0"

// GlobalOptions stores top-level flags.
type GlobalOptions struct {
	JSON    bool
	Verbose bool
}

// SubcommandHandler is the function signature for subcommands.
type SubcommandHandler func(ctx context.Context, globalOpts GlobalOptions, args []string, stdin io.Reader, stdout, stderr io.Writer) int

var subcommands = map[string]SubcommandHandler{
	"capture":   runCapture,
	"preflight": runPreflight,
	"stitch":    runStitch,
	"pack":      runPack,
	"unpack":    runUnpack,
	"inspect":   runInspect,
	"verify":    runVerify,
	"resume":    runResume,
	"replay":    runReplay,
}

// RunCLI is the testable entry point for all command execution.
func RunCLI(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var globalOpts GlobalOptions
	var subcmdArgs []string
	var cmdName string

	// Early detect --json anywhere in args to ensure flag parse errors format as JSON
	for _, arg := range args {
		if arg == "--json" {
			globalOpts.JSON = true
			break
		}
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--json" {
			globalOpts.JSON = true
		} else if arg == "-v" || arg == "--verbose" {
			globalOpts.Verbose = true
		} else if arg == "--version" {
			if globalOpts.JSON {
				_ = outputJSON(stdout, map[string]string{"version": Version})
			} else {
				fmt.Fprintf(stdout, "pimap version %s\n", Version)
			}
			return ExitCodeSuccess
		} else if arg == "-h" || arg == "--help" || arg == "help" {
			printUsage(stdout)
			return ExitCodeSuccess
		} else if !strings.HasPrefix(arg, "-") && cmdName == "" {
			cmdName = arg
			subcmdArgs = args[i+1:]
			break
		} else {
			// Unknown global flag or unexpected argument
			if globalOpts.JSON {
				_ = outputJSON(stderr, map[string]any{
					"error": fmt.Sprintf("unknown flag or argument: %s", arg),
					"code":  ExitCodeInvalidArgs,
				})
			} else {
				fmt.Fprintf(stderr, "unknown flag or argument: %s\n", arg)
				printUsage(stderr)
			}
			return ExitCodeInvalidArgs
		}
	}

	if cmdName == "" {
		printUsage(stdout)
		return ExitCodeSuccess
	}

	handler, exists := subcommands[cmdName]
	if !exists {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{
				"error": fmt.Sprintf("unknown command %q", cmdName),
				"code":  ExitCodeInvalidArgs,
			})
		} else {
			fmt.Fprintf(stderr, "error: unknown command %q\n\n", cmdName)
			printUsage(stderr)
		}
		return ExitCodeInvalidArgs
	}

	return handler(ctx, globalOpts, subcmdArgs, stdin, stdout, stderr)
}

func outputJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func printUsage(w io.Writer) {
	usage := `pimap - Endfield Protocol Imaging Map Orchestrator & CLI Tool

Usage:
  pimap [global-flags] <command> [command-flags]

Global Flags:
  --json          Output results in structured JSON
  -v, --verbose   Enable verbose progress and debug logging
  --version       Print version information
  -h, --help      Show help for pimap

Commands:
  capture         Execute the full automated map capture and assembly pipeline
  preflight       Run non-destructive Go/No-Go hardware and window capability probes
  stitch          Run offline pose-graph optimization and canvas stitching on a project
  pack            Pack a captured project directory into a single .pimap archive
  unpack          Safely unpack a .pimap archive with anti-zip-slip protection
  inspect         Inspect project health, manifest metadata, boundaries, and layers
  verify          Verify cryptographic bundle hashes and geometric coverage audit
  resume          Resume an interrupted capture session from checkpoint
  replay          Deterministic offline replay of hardware interaction recording trace

Run 'pimap <command> --help' for detailed flags on each subcommand.
`
	fmt.Fprint(w, usage)
}
