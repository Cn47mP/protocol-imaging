package stitch

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
)

// StitchTile represents an input tile to stitch.
type StitchTile struct {
	ID    string         `json:"id"`
	Row   int            `json:"row"`
	Col   int            `json:"col"`
	Image image.Image    `json:"-"`
	Pose  geometry.Point `json:"pose"`
	Size  geometry.Size  `json:"size"`
}

// StitchOptions defines stitching configuration.
type StitchOptions struct {
	FeatherWidth     int        `json:"feather_width"`     // Narrow feathering width (default: 12)
	GeneratePanorama bool       `json:"generate_panorama"` // Export full panorama.png
	GeneratePreview  bool       `json:"generate_preview"`  // Export preview.png
	PreviewMaxSize   int        `json:"preview_max_size"`  // Max width/height of preview (default: 1024)
	BackgroundColor  color.RGBA `json:"background_color"`
}

// DefaultStitchOptions provides standard defaults.
func DefaultStitchOptions() StitchOptions {
	return StitchOptions{
		FeatherWidth:     12,
		GeneratePanorama: true,
		GeneratePreview:  true,
		PreviewMaxSize:   1024,
		BackgroundColor:  color.RGBA{R: 0, G: 0, B: 0, A: 0},
	}
}

// StitchResult holds rendered images and summary statistics.
type StitchResult struct {
	Panorama  *image.RGBA   `json:"-"`
	Preview   *image.RGBA   `json:"-"`
	Bounds    geometry.Rect `json:"bounds"`
	TileCount int           `json:"tile_count"`
	Duration  time.Duration `json:"duration"`
}

// Stitcher coordinates tile rendering and preview generation.
type Stitcher struct {
	options StitchOptions
	blender *Blender
}

// NewStitcher initializes a Stitcher instance.
func NewStitcher(opts StitchOptions) *Stitcher {
	return &Stitcher{
		options: opts,
		blender: NewBlender(opts.FeatherWidth),
	}
}

// Stitch renders all tiles onto a single seamless canvas.
func (s *Stitcher) Stitch(ctx context.Context, tiles []StitchTile) (*StitchResult, error) {
	start := time.Now()
	if len(tiles) == 0 {
		return nil, errors.New("no tiles provided for stitching")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 1. Deterministic sort order (Row ascending, then Col ascending, then ID)
	sortedTiles := make([]StitchTile, len(tiles))
	copy(sortedTiles, tiles)
	sort.Slice(sortedTiles, func(i, j int) bool {
		if sortedTiles[i].Row != sortedTiles[j].Row {
			return sortedTiles[i].Row < sortedTiles[j].Row
		}
		if sortedTiles[i].Col != sortedTiles[j].Col {
			return sortedTiles[i].Col < sortedTiles[j].Col
		}
		return sortedTiles[i].ID < sortedTiles[j].ID
	})

	// 2. Compute bounding box
	poses := make(map[string]geometry.Point, len(sortedTiles))
	sizes := make(map[string]geometry.Size, len(sortedTiles))
	for _, t := range sortedTiles {
		poses[t.ID] = t.Pose
		sizes[t.ID] = t.Size
	}

	bounds, err := ComputeBoundingBox(poses, sizes)
	if err != nil {
		return nil, fmt.Errorf("compute canvas bounds: %w", err)
	}

	// 3. Allocate canvas
	canvas, err := NewCanvas(bounds, s.options.BackgroundColor)
	if err != nil {
		return nil, fmt.Errorf("allocate canvas: %w", err)
	}

	// 4. Blend tiles sequentially
	for _, t := range sortedTiles {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if t.Image == nil {
			return nil, fmt.Errorf("tile %q has nil image", t.ID)
		}
		offset := canvas.TransformPoint(t.Pose)
		if err := s.blender.BlendTile(canvas, t.Image, offset); err != nil {
			return nil, fmt.Errorf("blend tile %q: %w", t.ID, err)
		}
	}

	result := &StitchResult{
		Panorama:  canvas.RGBA,
		Bounds:    bounds,
		TileCount: len(sortedTiles),
		Duration:  time.Since(start),
	}

	// 5. Downsample preview if requested
	if s.options.GeneratePreview {
		maxDim := s.options.PreviewMaxSize
		if maxDim <= 0 {
			maxDim = 1024
		}
		result.Preview = DownsampleAreaAverage(canvas.RGBA, maxDim)
	}

	return result, nil
}

// DownsampleAreaAverage scales an image down using an area-weighted box filter (pure Go, zero aliasing).
func DownsampleAreaAverage(src *image.RGBA, maxDim int) *image.RGBA {
	bounds := src.Bounds()
	sw, sh := bounds.Dx(), bounds.Dy()
	if sw <= 0 || sh <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}

	scale := math.Min(float64(maxDim)/float64(sw), float64(maxDim)/float64(sh))
	if scale >= 1.0 {
		dst := image.NewRGBA(bounds)
		copy(dst.Pix, src.Pix)
		return dst
	}

	dw := int(math.Max(1, math.Round(float64(sw)*scale)))
	dh := int(math.Max(1, math.Round(float64(sh)*scale)))
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))

	xScale := float64(sw) / float64(dw)
	yScale := float64(sh) / float64(dh)

	for dy := 0; dy < dh; dy++ {
		sy0 := int(float64(dy) * yScale)
		sy1 := int(math.Min(float64(sh), float64(dy+1)*yScale))
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}

		for dx := 0; dx < dw; dx++ {
			sx0 := int(float64(dx) * xScale)
			sx1 := int(math.Min(float64(sw), float64(dx+1)*xScale))
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}

			var sumR, sumG, sumB, sumA float64
			count := float64((sx1 - sx0) * (sy1 - sy0))

			for sy := sy0; sy < sy1; sy++ {
				rowOffset := sy * src.Stride
				for sx := sx0; sx < sx1; sx++ {
					idx := rowOffset + sx*4
					sumR += float64(src.Pix[idx])
					sumG += float64(src.Pix[idx+1])
					sumB += float64(src.Pix[idx+2])
					sumA += float64(src.Pix[idx+3])
				}
			}

			dIdx := dy*dst.Stride + dx*4
			dst.Pix[dIdx] = uint8(math.Round(sumR / count))
			dst.Pix[dIdx+1] = uint8(math.Round(sumG / count))
			dst.Pix[dIdx+2] = uint8(math.Round(sumB / count))
			dst.Pix[dIdx+3] = uint8(math.Round(sumA / count))
		}
	}

	return dst
}

// ExportFiles saves the rendered panorama and preview to disk.
func (s *Stitcher) ExportFiles(result *StitchResult, outputDir string) (string, string, error) {
	if result == nil {
		return "", "", errors.New("nil stitch result")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create output directory: %w", err)
	}

	panoPath := filepath.Join(outputDir, "panorama.png")
	if result.Panorama != nil && s.options.GeneratePanorama {
		f, err := os.Create(panoPath)
		if err != nil {
			return "", "", fmt.Errorf("create panorama file: %w", err)
		}
		defer f.Close()

		enc := png.Encoder{CompressionLevel: png.DefaultCompression}
		if err := enc.Encode(f, result.Panorama); err != nil {
			return "", "", fmt.Errorf("encode panorama png: %w", err)
		}
	}

	previewPath := filepath.Join(outputDir, "preview.png")
	if result.Preview != nil && s.options.GeneratePreview {
		f, err := os.Create(previewPath)
		if err != nil {
			return "", "", fmt.Errorf("create preview file: %w", err)
		}
		defer f.Close()

		enc := png.Encoder{CompressionLevel: png.DefaultCompression}
		if err := enc.Encode(f, result.Preview); err != nil {
			return "", "", fmt.Errorf("encode preview png: %w", err)
		}
	}

	return panoPath, previewPath, nil
}
