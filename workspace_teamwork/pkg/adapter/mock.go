package adapter

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
)

var (
	ErrNilCanvas = errors.New("mock controller canvas is nil")
)

// MockControllerConfig configures the MockController.
type MockControllerConfig struct {
	Canvas             image.Image
	CanvasWidth        int
	CanvasHeight       int
	ViewportWidth      int
	ViewportHeight     int
	InitialCamX        float64
	InitialCamY        float64
	DragFactorX        float64 // default: 1.0 (input px -> camera px)
	DragFactorY        float64 // default: 1.0
	PixelScale         float64 // default: 1.0
	NoiseStdDev        float64 // random camera jitter in px
	CaptureNoiseStdDev float64 // sensor noise on pixel channels
	NoiseSeed          int64   // deterministic PRNG seed
	ClampBounds        *geometry.Rect
	SettlingDelay      time.Duration
}

// DefaultMockControllerConfig returns default configuration.
func DefaultMockControllerConfig() MockControllerConfig {
	return MockControllerConfig{
		CanvasWidth:    3840,
		CanvasHeight:   2160,
		ViewportWidth:  1920,
		ViewportHeight: 1080,
		DragFactorX:    1.0,
		DragFactorY:    1.0,
		PixelScale:     1.0,
		NoiseSeed:      42,
	}
}

// MockController simulates physical camera movement over a synthetic map canvas.
type MockController struct {
	mu          sync.Mutex
	cfg         MockControllerConfig
	canvas      image.Image
	camX        float64
	camY        float64
	rng         *rand.Rand
	seq         int64
	isDragging  bool
	lastScrollX int32
	lastScrollY int32
}

// NewMockController creates a new MockController with the given configuration.
func NewMockController(cfg MockControllerConfig) (*MockController, error) {
	if cfg.CanvasWidth <= 0 {
		cfg.CanvasWidth = 3840
	}
	if cfg.CanvasHeight <= 0 {
		cfg.CanvasHeight = 2160
	}
	if cfg.ViewportWidth <= 0 {
		cfg.ViewportWidth = 1920
	}
	if cfg.ViewportHeight <= 0 {
		cfg.ViewportHeight = 1080
	}
	if cfg.DragFactorX == 0 {
		cfg.DragFactorX = 1.0
	}
	if cfg.DragFactorY == 0 {
		cfg.DragFactorY = 1.0
	}
	if cfg.PixelScale == 0 {
		cfg.PixelScale = 1.0
	}

	var canvas image.Image = cfg.Canvas
	if canvas == nil {
		// Generate high-entropy default procedural test canvas
		canvas = generateDefaultCanvas(cfg.CanvasWidth, cfg.CanvasHeight)
	}

	seed := cfg.NoiseSeed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}

	camX := cfg.InitialCamX
	camY := cfg.InitialCamY

	maxX := float64(cfg.CanvasWidth - cfg.ViewportWidth)
	maxY := float64(cfg.CanvasHeight - cfg.ViewportHeight)
	if maxX < 0 {
		maxX = 0
	}
	if maxY < 0 {
		maxY = 0
	}

	if camX < 0 {
		camX = 0
	} else if camX > maxX {
		camX = maxX
	}
	if camY < 0 {
		camY = 0
	} else if camY > maxY {
		camY = maxY
	}

	return &MockController{
		cfg:    cfg,
		canvas: canvas,
		camX:   camX,
		camY:   camY,
		rng:    rand.New(rand.NewSource(seed)),
	}, nil
}

// CaptureRaw renders the current subpixel viewport from the synthetic canvas.
func (c *MockController) CaptureRaw(ctx context.Context) (Frame, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	default:
	}

	if c.canvas == nil {
		return Frame{}, ErrNilCanvas
	}

	if c.cfg.SettlingDelay > 0 {
		select {
		case <-ctx.Done():
			return Frame{}, ctx.Err()
		case <-time.After(c.cfg.SettlingDelay):
		}
	}

	rendered := c.renderSubpixelViewport(c.camX, c.camY, c.cfg.ViewportWidth, c.cfg.ViewportHeight)

	c.seq++
	return Frame{
		ID:         fmt.Sprintf("frame-%06d", c.seq),
		CapturedAt: time.Now().UTC(),
		Image:      rendered,
		Size: PixelSize{
			Width:  c.cfg.ViewportWidth,
			Height: c.cfg.ViewportHeight,
		},
	}, nil
}

// MiddleDrag simulates dragging the map with the middle mouse button.
func (c *MockController) MiddleDrag(ctx context.Context, gesture DragGesture) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := gesture.Begin.Validate(); err != nil {
		return fmt.Errorf("invalid gesture begin: %w", err)
	}
	if err := gesture.End.Validate(); err != nil {
		return fmt.Errorf("invalid gesture end: %w", err)
	}

	c.isDragging = true
	defer func() {
		c.isDragging = false
	}()

	// Nominal camera displacement
	dx := gesture.End.X - gesture.Begin.X
	dy := gesture.End.Y - gesture.Begin.Y

	dCamX := -dx * c.cfg.DragFactorX * c.cfg.PixelScale
	dCamY := -dy * c.cfg.DragFactorY * c.cfg.PixelScale

	// Optional jitter noise
	if c.cfg.NoiseStdDev > 0 {
		dCamX += c.rng.NormFloat64() * c.cfg.NoiseStdDev
		dCamY += c.rng.NormFloat64() * c.cfg.NoiseStdDev
	}

	newCamX := c.camX + dCamX
	newCamY := c.camY + dCamY

	if gesture.Duration > 0 {
		steps := int(gesture.Duration / (10 * time.Millisecond))
		if steps < 1 {
			steps = 1
		}
		stepDuration := gesture.Duration / time.Duration(steps)
		for i := 0; i < steps; i++ {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(stepDuration):
			}
		}
	}

	// Boundary clamping physics
	minX := 0.0
	minY := 0.0
	maxX := float64(c.cfg.CanvasWidth - c.cfg.ViewportWidth)
	maxY := float64(c.cfg.CanvasHeight - c.cfg.ViewportHeight)

	if c.cfg.ClampBounds != nil {
		minX = c.cfg.ClampBounds.X
		minY = c.cfg.ClampBounds.Y
		maxX = c.cfg.ClampBounds.X + c.cfg.ClampBounds.Width
		maxY = c.cfg.ClampBounds.Y + c.cfg.ClampBounds.Height
	}

	if maxX < minX {
		maxX = minX
	}
	if maxY < minY {
		maxY = minY
	}

	c.camX = math.Max(minX, math.Min(maxX, newCamX))
	c.camY = math.Max(minY, math.Min(maxY, newCamY))

	return nil
}

// Scroll simulates scrolling.
func (c *MockController) Scroll(ctx context.Context, deltaX, deltaY int32) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	c.lastScrollX += deltaX
	c.lastScrollY += deltaY
	return nil
}

// InputViewport returns the configured input viewport.
func (c *MockController) InputViewport(ctx context.Context) (PixelSize, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return PixelSize{}, ctx.Err()
	default:
	}

	return PixelSize{
		Width:  c.cfg.ViewportWidth,
		Height: c.cfg.ViewportHeight,
	}, nil
}

// Release resets the active drag / touch state.
func (c *MockController) Release(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.isDragging = false
	return nil
}

// CameraPosition returns the current continuous camera coordinates.
func (c *MockController) CameraPosition() (float64, float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.camX, c.camY
}

// SetCameraPosition sets the continuous camera coordinates.
func (c *MockController) SetCameraPosition(x, y float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	maxX := float64(c.cfg.CanvasWidth - c.cfg.ViewportWidth)
	maxY := float64(c.cfg.CanvasHeight - c.cfg.ViewportHeight)
	if maxX < 0 {
		maxX = 0
	}
	if maxY < 0 {
		maxY = 0
	}

	c.camX = math.Max(0, math.Min(maxX, x))
	c.camY = math.Max(0, math.Min(maxY, y))
}

// IsDragging returns whether an active drag is in progress.
func (c *MockController) IsDragging() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.isDragging
}

// SetCanvas updates the backing canvas image.
func (c *MockController) SetCanvas(canvas image.Image) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.canvas = canvas
	if canvas != nil {
		b := canvas.Bounds()
		c.cfg.CanvasWidth = b.Dx()
		c.cfg.CanvasHeight = b.Dy()
	}
}

// renderSubpixelViewport performs bilinear sampling of the canvas.
func (c *MockController) renderSubpixelViewport(cx, cy float64, vw, vh int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, vw, vh))
	bounds := c.canvas.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	for v := 0; v < vh; v++ {
		for u := 0; u < vw; u++ {
			sx := cx + float64(u)
			sy := cy + float64(v)

			// Clamped coordinates in canvas
			sx = math.Max(0, math.Min(float64(srcW-1), sx))
			sy = math.Max(0, math.Min(float64(srcH-1), sy))

			x0 := int(math.Floor(sx))
			y0 := int(math.Floor(sy))
			x1 := x0 + 1
			y1 := y0 + 1
			if x1 >= srcW {
				x1 = srcW - 1
			}
			if y1 >= srcH {
				y1 = srcH - 1
			}

			fx := sx - float64(x0)
			fy := sy - float64(y0)

			c00 := rgbaAt(c.canvas, bounds.Min.X+x0, bounds.Min.Y+y0)
			c10 := rgbaAt(c.canvas, bounds.Min.X+x1, bounds.Min.Y+y0)
			c01 := rgbaAt(c.canvas, bounds.Min.X+x0, bounds.Min.Y+y1)
			c11 := rgbaAt(c.canvas, bounds.Min.X+x1, bounds.Min.Y+y1)

			w00 := (1.0 - fx) * (1.0 - fy)
			w10 := fx * (1.0 - fy)
			w01 := (1.0 - fx) * fy
			w11 := fx * fy

			r := w00*float64(c00.R) + w10*float64(c10.R) + w01*float64(c01.R) + w11*float64(c11.R)
			g := w00*float64(c00.G) + w10*float64(c10.G) + w01*float64(c01.G) + w11*float64(c11.G)
			b := w00*float64(c00.B) + w10*float64(c10.B) + w01*float64(c01.B) + w11*float64(c11.B)
			a := w00*float64(c00.A) + w10*float64(c10.A) + w01*float64(c01.A) + w11*float64(c11.A)

			if c.cfg.CaptureNoiseStdDev > 0 {
				noise := c.rng.NormFloat64() * c.cfg.CaptureNoiseStdDev
				r += noise
				g += noise
				b += noise
			}

			dst.SetRGBA(u, v, color.RGBA{
				R: uint8(math.Max(0, math.Min(255, math.Round(r)))),
				G: uint8(math.Max(0, math.Min(255, math.Round(g)))),
				B: uint8(math.Max(0, math.Min(255, math.Round(b)))),
				A: uint8(math.Max(0, math.Min(255, math.Round(a)))),
			})
		}
	}

	return dst
}

func rgbaAt(img image.Image, x, y int) color.RGBA {
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba.RGBAAt(x, y)
	}
	c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
	return c
}

func generateDefaultCanvas(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	cellSize := 128
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			cellX := x / cellSize
			cellY := y / cellSize
			isDark := (cellX+cellY)%2 == 0

			base := uint8(40)
			if !isDark {
				base = 80
			}

			// Add grid line
			if x%cellSize == 0 || y%cellSize == 0 {
				base = 180
			}

			// Add sub-grid (16px)
			if x%16 == 0 || y%16 == 0 {
				base += 20
			}

			r := base + uint8((x*50)/w)
			g := base + uint8((y*50)/h)
			b := base + uint8(((x+y)*30)/(w+h))

			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return img
}
