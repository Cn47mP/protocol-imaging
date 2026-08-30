// Package maaend exposes protocol-imaging as one long-running MaaEnd custom
// action. MaaEnd supplies the Controller and Tasker; this package never creates
// or owns either object.
package maaend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/adapter"
	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/pipeline"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

const ActionName = "ProtocolImagingCapture"

type ActionParams struct {
	ProjectDir    string `json:"project_dir"`
	ArchivePath   string `json:"archive_path,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
	Title         string `json:"title,omitempty"`
	GameVersion   string `json:"game_version,omitempty"`
	Resume        bool   `json:"resume,omitempty"`
	SkipPreflight bool   `json:"skip_preflight,omitempty"`
}

func ParseActionParams(raw string) (ActionParams, error) {
	var params ActionParams
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&params); err != nil {
		return ActionParams{}, fmt.Errorf("decode %s parameters: %w", ActionName, err)
	}
	if strings.TrimSpace(params.ProjectDir) == "" {
		return ActionParams{}, errors.New("project_dir is required")
	}
	return params, nil
}

type Action struct {
	Listener pipeline.ProgressListener
}

var _ maa.CustomActionRunner = (*Action)(nil)

func (a *Action) Run(maaCtx *maa.Context, arg *maa.CustomActionArg) bool {
	if maaCtx == nil || arg == nil {
		log.Printf("%s: MaaEnd context or custom action arguments are unavailable", ActionName)
		return false
	}
	params, err := ParseActionParams(arg.CustomActionParam)
	if err != nil {
		log.Printf("%s: invalid action parameters: %v", ActionName, err)
		return false
	}

	tasker := maaCtx.GetTasker()
	if tasker == nil {
		log.Printf("%s: MaaEnd tasker is unavailable", ActionName)
		return false
	}
	ctrl := tasker.GetController()
	if ctrl == nil {
		log.Printf("%s: MaaEnd controller is unavailable", ActionName)
		return false
	}
	if err := ctrl.SetScreenshot(maa.WithScreenshotUseRawSize(true)); err != nil {
		log.Printf("%s: enable raw-size screenshots: %v", ActionName, err)
		return false
	}

	driver, err := adapter.NewMaaFrameworkDriver(ctrl, tasker)
	if err != nil {
		log.Printf("%s: create MaaFramework driver: %v", ActionName, err)
		return false
	}
	controller, err := adapter.NewMaaEndController(adapter.MaaEndControllerConfig{Driver: driver})
	if err != nil {
		log.Printf("%s: create MaaEnd controller adapter: %v", ActionName, err)
		return false
	}

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if tasker.Stopping() {
					cancel()
					return
				}
			}
		}
	}()

	cfg := pipeline.DefaultPipelineConfig(params.ProjectDir)
	cfg.ArchivePath = params.ArchivePath
	cfg.ProjectID = params.ProjectID
	cfg.Title = params.Title
	cfg.GameVersion = params.GameVersion
	cfg.Resume = params.Resume
	cfg.SkipPreflight = params.SkipPreflight
	_, err = pipeline.NewPipeline(cfg, a.Listener).Execute(runCtx, controller)
	cancel()
	<-monitorDone
	if err != nil {
		log.Printf("%s: capture pipeline failed: %v", ActionName, err)
	}
	return err == nil
}

func Register() error {
	return maa.AgentServerRegisterCustomAction(ActionName, &Action{})
}
