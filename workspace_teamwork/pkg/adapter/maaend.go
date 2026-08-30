package adapter

import (
	"context"
	"errors"
	"fmt"
	"image"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
)

var (
	ErrDriverNotConfigured = errors.New("maaend host driver not configured")
	ErrInvalidGesture      = errors.New("invalid drag gesture coordinates or duration")
)

// MaaHostDriver is the narrow boundary between the capture engine and the
// Controller/Tasker owned by the current MaaEnd task.
type MaaHostDriver interface {
	PostSwipeV2(ctx context.Context, contact int, start, end geometry.Point, duration time.Duration) error
	PostScroll(ctx context.Context, deltaX, deltaY int32) error
	CaptureRaw(ctx context.Context) (image.Image, error)
	InputViewport(ctx context.Context) (PixelSize, error)
	Inactivate(ctx context.Context) error
}

type MaaEndControllerConfig struct {
	Driver MaaHostDriver
}

// MaaEndController adapts a MaaEnd-owned controller to the capture engine.
// It never discovers windows or creates/destroys MaaFramework objects.
type MaaEndController struct {
	mu       sync.Mutex
	driver   MaaHostDriver
	frameSeq int64
}

func NewMaaEndController(cfg MaaEndControllerConfig) (*MaaEndController, error) {
	if cfg.Driver == nil {
		return nil, ErrDriverNotConfigured
	}
	return &MaaEndController{driver: cfg.Driver}, nil
}

func (c *MaaEndController) CaptureRaw(ctx context.Context) (Frame, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Frame{}, err
	}
	img, err := c.driver.CaptureRaw(ctx)
	if err != nil {
		return Frame{}, fmt.Errorf("maaend capture raw: %w", err)
	}
	if img == nil || img.Bounds().Empty() {
		return Frame{}, errors.New("maaend returned an empty screenshot")
	}
	bounds := img.Bounds()
	seq := atomic.AddInt64(&c.frameSeq, 1)
	return Frame{
		ID:         fmt.Sprintf("frame-%06d", seq),
		CapturedAt: time.Now().UTC(),
		Image:      img,
		Size:       PixelSize{Width: bounds.Dx(), Height: bounds.Dy()},
	}, nil
}

func (c *MaaEndController) MiddleDrag(ctx context.Context, gesture DragGesture) error {
	if err := gesture.Begin.Validate(); err != nil {
		return fmt.Errorf("%w: begin: %v", ErrInvalidGesture, err)
	}
	if err := gesture.End.Validate(); err != nil {
		return fmt.Errorf("%w: end: %v", ErrInvalidGesture, err)
	}
	if gesture.Duration < 0 {
		return fmt.Errorf("%w: negative duration", ErrInvalidGesture)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	duration := gesture.Duration
	if duration == 0 {
		duration = 50 * time.Millisecond
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.driver.PostSwipeV2(ctx, 2, gesture.Begin, gesture.End, duration)
}

func (c *MaaEndController) Scroll(ctx context.Context, deltaX, deltaY int32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.driver.PostScroll(ctx, deltaX, deltaY)
}

func (c *MaaEndController) InputViewport(ctx context.Context) (PixelSize, error) {
	if err := ctx.Err(); err != nil {
		return PixelSize{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	viewport, err := c.driver.InputViewport(ctx)
	if err != nil {
		return PixelSize{}, err
	}
	if viewport.Width <= 0 || viewport.Height <= 0 {
		return PixelSize{}, errors.New("maaend returned an invalid input resolution")
	}
	return viewport, nil
}

// Release leaves MaaFramework input in an inactive state. The host owns the
// controller lifetime, so this method must not destroy or disconnect it.
func (c *MaaEndController) Release(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.driver.Inactivate(ctx)
}
