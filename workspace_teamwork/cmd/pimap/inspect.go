package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/pipeline"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/project"
)

type InspectReport struct {
	ProjectID         string                      `json:"project_id"`
	ProjectDir        string                      `json:"project_dir"`
	CaptureState      project.CaptureState        `json:"capture_state"`
	GeometryStatus    project.GeometryStatus      `json:"geometry_status"`
	ActivePlanID      string                      `json:"active_plan_id"`
	ActiveCalibration string                      `json:"active_calibration"`
	CreatedAt         string                      `json:"created_at"`
	UpdatedAt         string                      `json:"updated_at"`
	TileCount         int                         `json:"tile_count"`
	RowCount          int                         `json:"row_count"`
	ConfirmedEdges    map[string]bool             `json:"confirmed_edges"`
	CoveragePassed    bool                        `json:"coverage_passed"`
	CanResume         bool                        `json:"can_resume"`
	Calibrations      []project.CalibrationRecord `json:"calibrations,omitempty"`
	Layers            []string                    `json:"layers"`
}

func runInspect(ctx context.Context, globalOpts GlobalOptions, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	if globalOpts.JSON {
		fs.SetOutput(io.Discard)
	} else {
		fs.SetOutput(stderr)
	}

	projectDir := fs.String("project", "", "Path to project directory to inspect")
	archiveFile := fs.String("archive", "", "Path to .pimap archive file to inspect")

	if err := fs.Parse(args); err != nil {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{
				"error": err.Error(),
				"code":  ExitCodeInvalidArgs,
			})
		}
		return ExitCodeInvalidArgs
	}

	if *projectDir == "" && *archiveFile == "" {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": "either --project or --archive must be specified"})
		} else {
			fmt.Fprintf(stderr, "error: either --project or --archive must be specified\n")
		}
		return ExitCodeInvalidArgs
	}

	targetDir := *projectDir
	var tempDir string
	if *archiveFile != "" && targetDir == "" {
		var err error
		tempDir, err = os.MkdirTemp("", "pimap-inspect-*")
		if err != nil {
			if globalOpts.JSON {
				_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("create temp dir: %v", err)})
			} else {
				fmt.Fprintf(stderr, "error creating temp dir: %v\n", err)
			}
			return ExitCodeGeneralError
		}
		defer os.RemoveAll(tempDir)

		if err := project.Unpack(*archiveFile, tempDir); err != nil {
			if globalOpts.JSON {
				_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("unpack archive: %v", err)})
			} else {
				fmt.Fprintf(stderr, "error unpacking archive %s: %v\n", *archiveFile, err)
			}
			return ExitCodeCorruptSession
		}
		targetDir = tempDir
	}

	store, err := project.NewStore(targetDir)
	if err != nil {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("open store: %v", err)})
		} else {
			fmt.Fprintf(stderr, "error opening project store: %v\n", err)
		}
		return ExitCodeCorruptSession
	}

	session, err := store.Resume(ctx)
	if err != nil {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("resume session: %v", err)})
		} else {
			fmt.Fprintf(stderr, "error loading project session: %v\n", err)
		}
		return ExitCodeCorruptSession
	}

	manifest := session.Manifest()
	captureDoc := session.CaptureDocument()
	activePlan := session.ActivePlan()

	state, tileCount, canResume, _ := pipeline.InspectResumeState(session)

	rowCount := 0
	confirmedEdges := make(map[string]bool)
	coveragePassed := false
	if activePlan != nil {
		rowCount = len(activePlan.Frontier.Rows)
		confirmedEdges["left"] = activePlan.Frontier.ConfirmedEdges.Left
		confirmedEdges["top"] = activePlan.Frontier.ConfirmedEdges.Top
		confirmedEdges["right"] = activePlan.Frontier.ConfirmedEdges.Right
		confirmedEdges["bottom"] = activePlan.Frontier.ConfirmedEdges.Bottom
		if activePlan.CoverageAudit != nil {
			coveragePassed = activePlan.CoverageAudit.Passed
		}
	}

	layerNames := make([]string, 0, len(manifest.Layers))
	for _, l := range manifest.Layers {
		layerNames = append(layerNames, l.ID)
	}

	report := InspectReport{
		ProjectID:         manifest.ProjectID,
		ProjectDir:        targetDir,
		CaptureState:      state,
		GeometryStatus:    manifest.Geometry.Status,
		ActivePlanID:      manifest.ActivePlan,
		ActiveCalibration: manifest.ActiveCalibration,
		CreatedAt:         manifest.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:         manifest.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		TileCount:         tileCount,
		RowCount:          rowCount,
		ConfirmedEdges:    confirmedEdges,
		CoveragePassed:    coveragePassed,
		CanResume:         canResume,
		Calibrations:      captureDoc.Calibrations,
		Layers:            layerNames,
	}

	if globalOpts.JSON {
		_ = outputJSON(stdout, report)
	} else {
		fmt.Fprintf(stdout, "================ PROJECT INSPECTION ================\n")
		fmt.Fprintf(stdout, "Project ID:         %s\n", report.ProjectID)
		fmt.Fprintf(stdout, "Capture State:      %s\n", report.CaptureState)
		fmt.Fprintf(stdout, "Geometry Status:    %s\n", report.GeometryStatus)
		fmt.Fprintf(stdout, "Active Plan:        %s\n", report.ActivePlanID)
		fmt.Fprintf(stdout, "Total Tiles:        %d\n", report.TileCount)
		fmt.Fprintf(stdout, "Total Rows:         %d\n", report.RowCount)
		fmt.Fprintf(stdout, "Confirmed Edges:    L:%v T:%v R:%v B:%v\n",
			confirmedEdges["left"], confirmedEdges["top"], confirmedEdges["right"], confirmedEdges["bottom"])
		fmt.Fprintf(stdout, "Coverage Passed:    %v\n", report.CoveragePassed)
		fmt.Fprintf(stdout, "Can Resume:         %v\n", report.CanResume)
		fmt.Fprintf(stdout, "Active Calibration: %s\n", report.ActiveCalibration)
		fmt.Fprintf(stdout, "Created At:         %s\n", report.CreatedAt)
		fmt.Fprintf(stdout, "====================================================\n")
	}

	return ExitCodeSuccess
}
