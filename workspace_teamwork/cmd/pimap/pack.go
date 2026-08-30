package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/project"
)

func runPack(ctx context.Context, globalOpts GlobalOptions, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pack", flag.ContinueOnError)
	if globalOpts.JSON {
		fs.SetOutput(io.Discard)
	} else {
		fs.SetOutput(stderr)
	}

	projectDir := fs.String("project", "", "Path to project directory to pack")
	outputFile := fs.String("output", "", "Output .pimap archive file path")

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

	if *outputFile == "" {
		*outputFile = *projectDir + ".pimap"
	}

	if !globalOpts.JSON {
		fmt.Fprintf(stdout, "Packing project %s into archive %s...\n", *projectDir, *outputFile)
	}

	if err := project.Pack(*projectDir, *outputFile); err != nil {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("pack failed: %v", err)})
		} else {
			fmt.Fprintf(stderr, "error packing project: %v\n", err)
		}
		return ExitCodeGeneralError
	}

	info, err := os.Stat(*outputFile)
	var sizeBytes int64
	if err == nil {
		sizeBytes = info.Size()
	}

	if globalOpts.JSON {
		_ = outputJSON(stdout, map[string]any{
			"status":       "success",
			"project_dir":  *projectDir,
			"archive_path": *outputFile,
			"size_bytes":   sizeBytes,
		})
	} else {
		fmt.Fprintf(stdout, "Project packed successfully into %s (%d bytes)\n", *outputFile, sizeBytes)
	}

	return ExitCodeSuccess
}
