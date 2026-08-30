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

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/project"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/quality"
)

type VerifyReport struct {
	Valid         bool   `json:"valid"`
	ProjectID     string `json:"project_id"`
	ManifestValid bool   `json:"manifest_valid"`
	CaptureValid  bool   `json:"capture_valid"`
	BoundaryValid bool   `json:"boundary_valid"`
	PlanValid     bool   `json:"plan_valid"`
	CoverageAudit bool   `json:"coverage_audit"`
	ErrorMessage  string `json:"error_message,omitempty"`
}

func runVerify(ctx context.Context, globalOpts GlobalOptions, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	if globalOpts.JSON {
		fs.SetOutput(io.Discard)
	} else {
		fs.SetOutput(stderr)
	}

	projectDir := fs.String("project", "", "Path to project directory to verify")
	archiveFile := fs.String("archive", "", "Path to .pimap archive file to verify")

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
			_ = outputJSON(stderr, map[string]any{"error": "either --project or --archive must be specified", "code": ExitCodeInvalidArgs})
		} else {
			fmt.Fprintf(stderr, "error: either --project or --archive must be specified\n")
		}
		return ExitCodeInvalidArgs
	}

	targetDir := *projectDir
	var tempDir string
	if *archiveFile != "" && targetDir == "" {
		if _, err := os.Stat(*archiveFile); errors.Is(err, os.ErrNotExist) {
			if globalOpts.JSON {
				_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("archive %s does not exist", *archiveFile), "valid": false})
			} else {
				fmt.Fprintf(stderr, "error: archive %s does not exist\n", *archiveFile)
			}
			return ExitCodeCorruptSession
		}

		var err error
		tempDir, err = os.MkdirTemp("", "pimap-verify-*")
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
				_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("unpack archive: %v", err), "valid": false})
			} else {
				fmt.Fprintf(stderr, "error unpacking archive %s: %v\n", *archiveFile, err)
			}
			return ExitCodeCorruptSession
		}
		targetDir = tempDir
	}

	stat, err := os.Stat(targetDir)
	if errors.Is(err, os.ErrNotExist) || (err == nil && !stat.IsDir()) {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("project directory %s does not exist or is not a directory", targetDir), "valid": false})
		} else {
			fmt.Fprintf(stderr, "error: project directory %s does not exist or is not a directory\n", targetDir)
		}
		return ExitCodeCorruptSession
	}

	manifestPath := filepath.Join(targetDir, "manifest.json")
	mData, err := os.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": "manifest.json not found", "valid": false})
		} else {
			fmt.Fprintf(stderr, "error: manifest.json not found in %s\n", targetDir)
		}
		return ExitCodeCorruptSession
	}
	if err != nil {
		if globalOpts.JSON {
			_ = outputJSON(stderr, map[string]any{"error": fmt.Sprintf("read manifest.json: %v", err), "valid": false})
		} else {
			fmt.Fprintf(stderr, "error: read manifest.json: %v\n", err)
		}
		return ExitCodeCorruptSession
	}

	report := VerifyReport{
		Valid: true,
	}

	var manifest project.Manifest
	if err := json.Unmarshal(mData, &manifest); err != nil {
		report.Valid = false
		report.ErrorMessage = fmt.Sprintf("invalid manifest JSON: %v", err)
	} else {
		report.ProjectID = manifest.ProjectID
		if err := manifest.Validate(); err != nil {
			report.Valid = false
			report.ErrorMessage = fmt.Sprintf("invalid manifest: %v", err)
		} else {
			report.ManifestValid = true
		}
	}

	// Capture document validation
	capPath := manifest.Capture
	if capPath == "" {
		capPath = "capture.json"
	}
	cData, err := os.ReadFile(filepath.Join(targetDir, capPath))
	if err != nil {
		report.Valid = false
		if report.ErrorMessage == "" {
			report.ErrorMessage = fmt.Sprintf("read capture document %s: %v", capPath, err)
		}
	} else {
		var capDoc project.CaptureDocument
		if err := json.Unmarshal(cData, &capDoc); err != nil {
			report.Valid = false
			if report.ErrorMessage == "" {
				report.ErrorMessage = fmt.Sprintf("invalid capture doc JSON: %v", err)
			}
		} else if err := capDoc.Validate(); err != nil {
			report.Valid = false
			if report.ErrorMessage == "" {
				report.ErrorMessage = fmt.Sprintf("invalid capture doc: %v", err)
			}
		} else {
			report.CaptureValid = true
		}
	}

	// Boundary document validation
	bPath := manifest.Geometry.Observation
	if bPath == "" {
		bPath = "geometry/boundary.json"
	}
	bData, err := os.ReadFile(filepath.Join(targetDir, bPath))
	if err != nil {
		report.Valid = false
		if report.ErrorMessage == "" {
			report.ErrorMessage = fmt.Sprintf("read boundary document %s: %v", bPath, err)
		}
	} else {
		var boundDoc project.BoundaryDocument
		if err := json.Unmarshal(bData, &boundDoc); err != nil {
			report.Valid = false
			if report.ErrorMessage == "" {
				report.ErrorMessage = fmt.Sprintf("invalid boundary JSON: %v", err)
			}
		} else if err := boundDoc.Validate(); err != nil {
			report.Valid = false
			if report.ErrorMessage == "" {
				report.ErrorMessage = fmt.Sprintf("invalid boundary: %v", err)
			}
		} else {
			report.BoundaryValid = true
		}
	}

	// Active plan validation (if present)
	if manifest.ActivePlan != "" {
		pData, err := os.ReadFile(filepath.Join(targetDir, manifest.ActivePlan))
		if err != nil {
			report.Valid = false
			if report.ErrorMessage == "" {
				report.ErrorMessage = fmt.Sprintf("read active plan %s: %v", manifest.ActivePlan, err)
			}
		} else {
			var planDoc project.PlanDocument
			if err := json.Unmarshal(pData, &planDoc); err != nil {
				report.Valid = false
				if report.ErrorMessage == "" {
					report.ErrorMessage = fmt.Sprintf("invalid active plan JSON: %v", err)
				}
			} else if err := planDoc.Validate(); err != nil {
				report.Valid = false
				if report.ErrorMessage == "" {
					report.ErrorMessage = fmt.Sprintf("invalid active plan: %v", err)
				}
			} else {
				report.PlanValid = true
				if planDoc.CoverageAudit != nil && planDoc.CoverageAudit.Passed {
					report.CoverageAudit = true
				} else if planDoc.Frontier.ConfirmedEdges.All() {
					audit, err := quality.AuditCoverage(planDoc.Frontier, project.OverlapRange{Minimum: 0.05, Maximum: 0.99})
					if err == nil && audit.Passed {
						report.CoverageAudit = true
					}
				}
			}
		}
	}

	// Try store.Resume for bundle consistency check if store can open
	if store, err := project.NewStore(targetDir); err == nil {
		if session, err := store.Resume(ctx); err == nil {
			if activePlan := session.ActivePlan(); activePlan != nil {
				if activePlan.CoverageAudit != nil && activePlan.CoverageAudit.Passed {
					report.CoverageAudit = true
				}
			}
		}
	}

	if globalOpts.JSON {
		_ = outputJSON(stdout, report)
	} else {
		fmt.Fprintf(stdout, "================ VERIFICATION REPORT ================\n")
		fmt.Fprintf(stdout, "Project ID:      %s\n", report.ProjectID)
		fmt.Fprintf(stdout, "Overall Valid:   %v\n", report.Valid)
		fmt.Fprintf(stdout, "Manifest:        %v\n", report.ManifestValid)
		fmt.Fprintf(stdout, "Capture Doc:     %v\n", report.CaptureValid)
		fmt.Fprintf(stdout, "Boundary:        %v\n", report.BoundaryValid)
		fmt.Fprintf(stdout, "Active Plan:     %v\n", report.PlanValid)
		fmt.Fprintf(stdout, "Coverage Audit:  %v\n", report.CoverageAudit)
		if report.ErrorMessage != "" {
			fmt.Fprintf(stdout, "Error:           %s\n", report.ErrorMessage)
		}
		fmt.Fprintf(stdout, "=====================================================\n")
	}

	if report.Valid {
		return ExitCodeSuccess
	}
	return ExitCodeValidationFailed
}
