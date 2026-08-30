package main

import (
	"context"
	"flag"
	"fmt"
	"io"
)

func runResume(ctx context.Context, globalOpts GlobalOptions, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	if globalOpts.JSON {
		fs.SetOutput(io.Discard)
	} else {
		fs.SetOutput(stderr)
	}

	projectDir := fs.String("project", "", "Path to interrupted project directory")
	archivePath := fs.String("archive", "", "Target archive .pimap file on completion")
	windowName := fs.String("window", "Endfield", "Target window process name")
	windowTitle := fs.String("title", "明日方舟：终末地", "Target window class/title prefix")
	mockCanvasPath := fs.String("mock-canvas", "", "Path to synthetic PNG image to use MockController")

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

	captureArgs := []string{
		"--project", *projectDir,
		"--resume",
		"--skip-preflight",
		"--window", *windowName,
		"--title", *windowTitle,
	}
	if *archivePath != "" {
		captureArgs = append(captureArgs, "--archive", *archivePath)
	}
	if *mockCanvasPath != "" {
		captureArgs = append(captureArgs, "--mock-canvas", *mockCanvasPath)
	}

	return runCapture(ctx, globalOpts, captureArgs, stdin, stdout, stderr)
}
