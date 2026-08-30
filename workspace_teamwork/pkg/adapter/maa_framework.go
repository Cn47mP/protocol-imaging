package adapter

import (
	"context"
	"errors"
	"fmt"
	"image"
	"sync"
	"time"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

var (
	ErrMaaFrameworkNotConnected = errors.New("maaend controller or tasker is unavailable")
	ErrMaaTaskerStopping        = errors.New("maaend task is stopping")
	ErrMaaActionFailed          = errors.New("maa framework action failed")
)

// MaaFrameworkDriver borrows MaaFramework objects from the current MaaEnd
// custom action. Ownership and lifecycle remain entirely with MaaEnd.
type MaaFrameworkDriver struct {
	mu     sync.Mutex
	ctrl   *maa.Controller
	tasker *maa.Tasker
}

func NewMaaFrameworkDriver(ctrl *maa.Controller, tasker *maa.Tasker) (*MaaFrameworkDriver, error) {
	if ctrl == nil || tasker == nil {
		return nil, ErrMaaFrameworkNotConnected
	}
	return &MaaFrameworkDriver{ctrl: ctrl, tasker: tasker}, nil
}

func (d *MaaFrameworkDriver) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.ctrl == nil || d.tasker == nil {
		return ErrMaaFrameworkNotConnected
	}
	if d.tasker.Stopping() {
		return ErrMaaTaskerStopping
	}
	return nil
}

func waitJob(job *maa.Job, operation string) error {
	if job == nil {
		return fmt.Errorf("%w: %s returned nil job", ErrMaaActionFailed, operation)
	}
	job.Wait()
	if !job.Success() {
		return fmt.Errorf("%w: %s status %v", ErrMaaActionFailed, operation, job.Status())
	}
	return nil
}

func (d *MaaFrameworkDriver) PostSwipeV2(ctx context.Context, contact int, start, end geometry.Point, duration time.Duration) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ready(ctx); err != nil {
		return err
	}
	return waitJob(d.ctrl.PostSwipeV2(
		int32(start.X), int32(start.Y), int32(end.X), int32(end.Y),
		duration, int32(contact), 100,
	), "swipe")
}

func (d *MaaFrameworkDriver) PostScroll(ctx context.Context, deltaX, deltaY int32) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ready(ctx); err != nil {
		return err
	}
	return waitJob(d.ctrl.PostScroll(deltaX, deltaY), "scroll")
}

func (d *MaaFrameworkDriver) CaptureRaw(ctx context.Context) (image.Image, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ready(ctx); err != nil {
		return nil, err
	}
	if err := waitJob(d.ctrl.PostScreencap(), "screencap"); err != nil {
		return nil, err
	}
	img, err := d.ctrl.CacheImage()
	if err != nil {
		return nil, fmt.Errorf("read maaend screenshot cache: %w", err)
	}
	if img == nil {
		return nil, errors.New("maaend screenshot cache returned nil image")
	}
	return img, nil
}

func (d *MaaFrameworkDriver) InputViewport(ctx context.Context) (PixelSize, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ready(ctx); err != nil {
		return PixelSize{}, err
	}
	w, h, err := d.ctrl.GetResolution()
	if err != nil {
		return PixelSize{}, fmt.Errorf("read maaend resolution: %w", err)
	}
	if w <= 0 || h <= 0 {
		return PixelSize{}, errors.New("maaend returned an invalid resolution")
	}
	return PixelSize{Width: int(w), Height: int(h)}, nil
}

func (d *MaaFrameworkDriver) Inactivate(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ctrl == nil {
		return ErrMaaFrameworkNotConnected
	}
	// Release is cleanup: once an in-flight atomic job has returned, allow
	// PostInactive even if cancellation or Tasker.Stopping triggered the exit.
	return waitJob(d.ctrl.PostInactive(), "inactive")
}
