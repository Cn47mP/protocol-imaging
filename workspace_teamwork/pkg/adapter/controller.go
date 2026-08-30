package adapter

import (
	"time"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/capture"
)

// Re-export core types from capture for ergonomics and unified adapter interface.
type Controller = capture.Controller
type Frame = capture.Frame
type PixelSize = capture.PixelSize
type DragGesture = capture.DragGesture

// ProbeStatus represents the Go/No-Go status of a capability probe.
type ProbeStatus string

const (
	ProbeStatusGo   ProbeStatus = "GO"
	ProbeStatusNoGo ProbeStatus = "NO_GO"
)

// ProbeResult stores the outcome, latency, metrics, and diagnostics of an individual probe.
type ProbeResult struct {
	Name    string             `json:"name"`
	Status  ProbeStatus        `json:"status"` // "GO" or "NO_GO"
	Passed  bool               `json:"passed"`
	Details string             `json:"details"`
	Latency time.Duration      `json:"latency_ns"`
	Metrics map[string]float64 `json:"metrics,omitempty"`
}

// PreflightReport is the aggregated report of all capability probes.
type PreflightReport struct {
	Verdict     ProbeStatus                `json:"verdict"` // "GO" or "NO_GO"
	OverallPass bool                       `json:"overall_pass"`
	Timestamp   time.Time                  `json:"timestamp"`
	Probes      map[string]ProbeResult     `json:"probes"`
	Calibration *capture.CalibrationRecord `json:"calibration,omitempty"`
}

// Reporter interface for task lifecycle, progress updates, and diagnostic warnings.
type Reporter interface {
	Phase(name string)
	Progress(current, total int, message string)
	TileStatus(id string, status string)
	Warning(message string)
}

// NoopReporter provides a default no-op implementation of Reporter.
type NoopReporter struct{}

func (n *NoopReporter) Phase(name string)                       {}
func (n *NoopReporter) Progress(current, total int, msg string) {}
func (n *NoopReporter) TileStatus(id string, status string)     {}
func (n *NoopReporter) Warning(msg string)                      {}
