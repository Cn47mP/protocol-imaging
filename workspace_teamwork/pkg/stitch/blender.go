package stitch

import (
	"errors"
	"image"
	"math"
)

// BlendMode defines the blending algorithm.
type BlendMode string

const (
	BlendOverwrite     BlendMode = "overwrite"      // Direct deterministic copy
	BlendLinearFeather BlendMode = "linear_feather" // Narrow linear feathering (8-16px) along seams
)

// Blender composites tiles onto the canvas buffer.
type Blender struct {
	FeatherWidth int
	Mode         BlendMode
}

// NewBlender creates a blender with specified narrow feathering width (e.g. 8-16px).
func NewBlender(featherWidth int) *Blender {
	if featherWidth <= 0 {
		featherWidth = 12
	}
	return &Blender{
		FeatherWidth: featherWidth,
		Mode:         BlendLinearFeather,
	}
}

// BlendTile composites an individual tile image onto the target canvas at the specified offset.
func (b *Blender) BlendTile(canvas *Canvas, tile image.Image, offset image.Point) error {
	if canvas == nil || tile == nil {
		return errors.New("canvas or tile cannot be nil")
	}

	tileBounds := tile.Bounds()
	tw, th := tileBounds.Dx(), tileBounds.Dy()
	fw := float64(b.FeatherWidth)

	rgbaCanvas := canvas.RGBA
	cWidth := canvas.Bounds.Dx()
	cHeight := canvas.Bounds.Dy()

	// Fast path for *image.RGBA tiles
	rgbaTile, isRGBA := tile.(*image.RGBA)

	for ty := 0; ty < th; ty++ {
		cy := offset.Y + ty
		if cy < 0 || cy >= cHeight {
			continue
		}

		for tx := 0; tx < tw; tx++ {
			cx := offset.X + tx
			if cx < 0 || cx >= cWidth {
				continue
			}

			// Read source tile pixel
			var tr, tg, tb, ta uint32
			if isRGBA {
				idx := rgbaTile.PixOffset(tileBounds.Min.X+tx, tileBounds.Min.Y+ty)
				tr = uint32(rgbaTile.Pix[idx])
				tg = uint32(rgbaTile.Pix[idx+1])
				tb = uint32(rgbaTile.Pix[idx+2])
				ta = uint32(rgbaTile.Pix[idx+3])
			} else {
				c := tile.At(tileBounds.Min.X+tx, tileBounds.Min.Y+ty)
				r16, g16, b16, a16 := c.RGBA()
				tr = r16 >> 8
				tg = g16 >> 8
				tb = b16 >> 8
				ta = a16 >> 8
			}

			if ta == 0 {
				continue // transparent pixel
			}

			cIdx := cy*rgbaCanvas.Stride + cx*4
			ca := uint32(rgbaCanvas.Pix[cIdx+3])

			if ca == 0 || b.Mode == BlendOverwrite {
				// Canvas is unpainted: direct copy
				rgbaCanvas.Pix[cIdx] = uint8(tr)
				rgbaCanvas.Pix[cIdx+1] = uint8(tg)
				rgbaCanvas.Pix[cIdx+2] = uint8(tb)
				rgbaCanvas.Pix[cIdx+3] = uint8(ta)
				continue
			}

			// Compute narrow linear feathering factor w based on distance to tile border
			distX := math.Min(float64(tx), float64(tw-1-tx))
			distY := math.Min(float64(ty), float64(th-1-ty))
			distBorder := math.Min(distX, distY)

			var weight float64 = 1.0
			if distBorder < fw {
				weight = distBorder / fw
			}

			cr := float64(rgbaCanvas.Pix[cIdx])
			cg := float64(rgbaCanvas.Pix[cIdx+1])
			cb := float64(rgbaCanvas.Pix[cIdx+2])

			// Alpha blending with feathering weight
			outR := math.Round((1.0-weight)*cr + weight*float64(tr))
			outG := math.Round((1.0-weight)*cg + weight*float64(tg))
			outB := math.Round((1.0-weight)*cb + weight*float64(tb))

			rgbaCanvas.Pix[cIdx] = uint8(math.Min(255, math.Max(0, outR)))
			rgbaCanvas.Pix[cIdx+1] = uint8(math.Min(255, math.Max(0, outG)))
			rgbaCanvas.Pix[cIdx+2] = uint8(math.Min(255, math.Max(0, outB)))
			rgbaCanvas.Pix[cIdx+3] = 255
		}
	}

	return nil
}
