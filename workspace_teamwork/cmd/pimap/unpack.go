package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/project"
)

func runUnpack(ctx context.Context, globalOpts GlobalOptions, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("unpack", flag.ContinueOnError)
	if globalOpts.JSON {
		fs.SetOutput(io.Discard)
	} else {
		fs.SetOutput(stderr)
	}

	archiveFile := fs.String("archive", "", "Path to .pimap archive file to unpack")
	outputDir := fs.String("output", "", "Target output directory")
	maxBytes := fs.Int64("max-bytes", 2<<30, "Maximum allowable uncompressed size in bytes (anti-zip-bomb)")
	maxFiles := fs.Int("max-files", 50000, "Maximum allowable file count (anti-zip-bomb)")
	_ = maxFiles

	if err := fs.Parse(args); err != nil {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{
				"error": err.Error(),
				"code":  ExitCodeInvalidArgs,
			})
		}
		return ExitCodeInvalidArgs
	}

	if *archiveFile == "" {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": "--archive flag is required"})
		} else {
			fmt.Fprintf(stderr, "error: --archive flag is required\n")
		}
		return ExitCodeInvalidArgs
	}

	if *outputDir == "" {
		trimmed := strings.TrimSuffix(*archiveFile, ".pimap")
		if trimmed == *archiveFile {
			*outputDir = *archiveFile + "_unpacked"
		} else {
			*outputDir = trimmed
		}
	}

	if !globalOpts.JSON {
		fmt.Fprintf(stdout, "Unpacking archive %s into directory %s...\n", *archiveFile, *outputDir)
	}

	if err := project.UnpackWithLimits(*archiveFile, *outputDir, project.MaxZipEntryBytes, uint64(*maxBytes), project.MaxZipCompressionRatio); err != nil {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("unpack failed: %v", err)})
		} else {
			fmt.Fprintf(stderr, "error unpacking archive: %v\n", err)
		}
		return ExitCodeGeneralError
	}

	if globalOpts.JSON {
		_ = outputJSON(stdout, map[string]any{
			"status":       "success",
			"archive_path": *archiveFile,
			"output_dir":   *outputDir,
		})
	} else {
		fmt.Fprintf(stdout, "Archive unpacked successfully into %s\n", *outputDir)
	}

	return ExitCodeSuccess
}
