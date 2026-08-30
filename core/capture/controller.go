package capture

import (
	"context"
	"image"
	"time"

	"github.com/Cn47mP/protocol-imaging/core/geometry"
)

type PixelSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type Frame struct {
	ID         string
	CapturedAt time.Time
	Image      image.Image
	Size       PixelSize
}

type DragGesture struct {
	Begin    geometry.Point `json:"begin"`
	End      geometry.Point `json:"end"`
	Duration time.Duration  `json:"duration_ns"`
}

type Controller interface {
	CaptureRaw(ctx context.Context) (Frame, error)
	Scroll(ctx context.Context, deltaX, deltaY int32) error
	MiddleDrag(ctx context.Context, gesture DragGesture) error
	InputViewport(ctx context.Context) (PixelSize, error)
	Release(ctx context.Context) error
}

type CaptureProgress struct {
	AcceptedTiles  int            `json:"accepted_tiles"`
	DiscoveredRows int            `json:"discovered_rows"`
	ConfirmedEdges ConfirmedEdges `json:"confirmed_edges"`
	TotalTiles     *int           `json:"total_tiles,omitempty"`
}

type TileStatus string

type Reporter interface {
	Phase(name string)
	Progress(snapshot CaptureProgress)
	TileStatus(id string, status TileStatus)
	Warning(message string)
}
