package pipeline

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/adapter"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/capture"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/project"
)

// resumeExecution determines the appropriate resume strategy based on the session's recorded CaptureState.
func (p *Pipeline) resumeExecution(
	ctx context.Context,
	session *project.Session,
	sessionAdapter *projectSessionAdapter,
	engine *capture.Engine,
	ctrl adapter.Controller,
) error {
	manifest := session.Manifest()
	state := manifest.CaptureState

	p.listener.OnLog("INFO", fmt.Sprintf("Resuming pipeline from state: %s", state))

	switch state {
	case project.CaptureFailedCorrupt:
		return fmt.Errorf("%w: project session is marked failed_corrupt and cannot be resumed", project.ErrCorruptSession)

	case project.CaptureComplete:
		p.listener.OnLog("INFO", "Session is already complete, skipping capture engine")
		return nil

	case project.CaptureProcessing:
		p.listener.OnLog("INFO", "Session is in processing state, proceeding to post-capture stages")
		return nil

	case project.CaptureCreated, project.CaptureHoming, project.CaptureCalibrating:
		// Not enough stable state was captured before interruption; restart full sequence
		p.listener.OnLog("INFO", "Restarting fresh capture sequence from homing")
		if state != project.CaptureCreated {
			_ = session.UpdateCaptureState(context.Background(), project.CaptureCancelled, "", time.Now().UTC())
		}
		return engine.Run(ctx, sessionAdapter)

	case project.CaptureCapturing, project.CaptureRepairing, project.CaptureCancelled, project.CaptureFailedRecoverable:
		activePlan := session.ActivePlan()
		if activePlan == nil {
			p.listener.OnLog("WARN", "Active plan not found on resume, falling back to full capture")
			return engine.Run(ctx, sessionAdapter)
		}

		p.transitionStage(StageCapturing)
		p.listener.OnLog("INFO", fmt.Sprintf("Restoring frontier snapshot from plan %s (rev %d, %d tiles)",
			activePlan.ID, activePlan.Frontier.Revision, len(activePlan.Frontier.Tiles)))

		if err := engine.Resume(ctx, sessionAdapter, activePlan.Frontier); err != nil {
			return fmt.Errorf("engine resume: %w", err)
		}
		return nil

	default:
		return fmt.Errorf("unsupported capture state on resume: %s", state)
	}
}

// InspectResumeState returns human-readable diagnosis of an interrupted project.
func InspectResumeState(session *project.Session) (state project.CaptureState, tileCount int, canResume bool, err error) {
	if session == nil {
		return "", 0, false, errors.New("nil session")
	}
	manifest := session.Manifest()
	state = manifest.CaptureState

	activePlan := session.ActivePlan()
	if activePlan != nil {
		tileCount = len(activePlan.Frontier.Tiles)
	}

	switch state {
	case project.CaptureFailedCorrupt:
		canResume = false
	case project.CaptureComplete:
		canResume = true
	default:
		canResume = true
	}

	return state, tileCount, canResume, nil
}
