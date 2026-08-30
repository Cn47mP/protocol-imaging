package stitch

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
)

const (
	MaxCanvasDimension = 65536
	MaxCanvasBytes     = 2 * 1024 * 1024 * 1024 // 2GB
)

// Canvas represents the composited RGBA pixel buffer in session coordinates.
type Canvas struct {
	Bounds      image.Rectangle // Canvas buffer bounds (0, 0, Width, Height)
	RGBA        *image.RGBA     // Allocated raw RGBA image
	Origin      geometry.Point  // (MinX, MinY) session offset
	SessionRect geometry.Rect   // Encompassing session bounding box
}

// ComputeBoundingBox aggregates tile poses and sizes into a global bounding rectangle.
func ComputeBoundingBox(poses map[string]geometry.Point, tileSizes map[string]geometry.Size) (geometry.Rect, error) {
	if len(poses) == 0 {
		return geometry.Rect{}, errors.New("empty tile poses")
	}

	minX := math.MaxFloat64
	minY := math.MaxFloat64
	maxX := -math.MaxFloat64
	maxY := -math.MaxFloat64

	for id, p := range poses {
		size, exists := tileSizes[id]
		if !exists {
			return geometry.Rect{}, fmt.Errorf("missing tile size for %q", id)
		}
		if err := size.Validate(); err != nil {
			return geometry.Rect{}, fmt.Errorf("invalid tile size for %q: %w", id, err)
		}
		minX = math.Min(minX, p.X)
		minY = math.Min(minY, p.Y)
		maxX = math.Max(maxX, p.X+size.Width)
		maxY = math.Max(maxY, p.Y+size.Height)
	}

	rect := geometry.Rect{
		X:      minX,
		Y:      minY,
		Width:  maxX - minX,
		Height: maxY - minY,
	}
	if err := rect.Validate(); err != nil {
		return geometry.Rect{}, fmt.Errorf("invalid canvas bounding box: %w", err)
	}
	return rect, nil
}

// NewCanvas allocates a cleared RGBA buffer for the given session bounding box.
func NewCanvas(sessionRect geometry.Rect, bg color.RGBA) (*Canvas, error) {
	if err := sessionRect.Validate(); err != nil {
		return nil, fmt.Errorf("invalid session rect: %w", err)
	}

	w := int(math.Ceil(sessionRect.Width))
	h := int(math.Ceil(sessionRect.Height))

	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("non-positive canvas dimensions: %dx%d", w, h)
	}
	if w > MaxCanvasDimension || h > MaxCanvasDimension {
		return nil, fmt.Errorf("canvas dimensions %dx%d exceed maximum allowed %d", w, h, MaxCanvasDimension)
	}

	byteSize := int64(w) * int64(h) * 4
	if byteSize > MaxCanvasBytes {
		return nil, fmt.Errorf("canvas allocation (%d bytes) exceeds memory safety limit (%d bytes)", byteSize, MaxCanvasBytes)
	}

	rect := image.Rect(0, 0, w, h)
	img := image.NewRGBA(rect)

	// If background is not transparent black, initialize pixels
	if bg != (color.RGBA{}) {
		for i := 0; i < len(img.Pix); i += 4 {
			img.Pix[i] = bg.R
			img.Pix[i+1] = bg.G
			img.Pix[i+2] = bg.B
			img.Pix[i+3] = bg.A
		}
	}

	return &Canvas{
		Bounds:      rect,
		RGBA:        img,
		Origin:      geometry.Point{X: sessionRect.X, Y: sessionRect.Y},
		SessionRect: sessionRect,
	}, nil
}

// TransformPoint maps a point from session space to canvas pixel coordinate.
func (c *Canvas) TransformPoint(p geometry.Point) image.Point {
	return image.Point{
		X: int(math.Round(p.X - c.Origin.X)),
		Y: int(math.Round(p.Y - c.Origin.Y)),
	}
}
