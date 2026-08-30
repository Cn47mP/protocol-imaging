package registration

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"math/cmplx"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
)

// RegistrationResult contains the estimated 2D translation delta and quality metrics.
type RegistrationResult struct {
	Delta      geometry.Vector `json:"delta"`
	Confidence float64         `json:"confidence"`
	PeakRatio  float64         `json:"peak_ratio"`
	PSR        float64         `json:"psr"`
	Method     string          `json:"method"`
}

// PhaseCorrelationConfig provides tunable parameters for phase correlation registration.
type PhaseCorrelationConfig struct {
	ApplyHanning       bool
	SubpixelMethod     string // "parabolic", "centroid", "none"
	MinConfidence      float64
	PSRThreshold       float64
	PeakRatioThreshold float64
	EnableNCCFallback  bool
}

// DefaultConfig returns standard default configuration.
func DefaultConfig() PhaseCorrelationConfig {
	return PhaseCorrelationConfig{
		ApplyHanning:       true,
		SubpixelMethod:     "sinc",
		MinConfidence:      0.4,
		PSRThreshold:       4.5,
		PeakRatioThreshold: 1.25,
		EnableNCCFallback:  true,
	}
}

// imageToWindowedMatrix converts an image to a mean-subtracted, 2D Hanning-windowed ComplexMatrix padded to rows x cols.
func imageToWindowedMatrix(img image.Image, rows, cols int, applyHanning bool) (*ComplexMatrix, error) {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil, errors.New("empty image bounds")
	}

	// Precompute 1D Hanning windows
	hx := make([]float64, w)
	hy := make([]float64, h)
	for x := 0; x < w; x++ {
		if applyHanning && w > 1 {
			hx[x] = 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(x)/float64(w-1)))
		} else {
			hx[x] = 1.0
		}
	}
	for y := 0; y < h; y++ {
		if applyHanning && h > 1 {
			hy[y] = 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(y)/float64(h-1)))
		} else {
			hy[y] = 1.0
		}
	}

	// Compute image mean intensity
	var sum float64
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
			sum += float64(c.Y)
		}
	}
	mean := sum / float64(w*h)

	matrix := NewComplexMatrix(rows, cols)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.GrayModel.Convert(img.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.Gray)
			val := (float64(c.Y) - mean) * hx[x] * hy[y]
			matrix.Set(y, x, complex(val, 0))
		}
	}
	return matrix, nil
}

// ComputePhaseCorrelation performs pure 2D translation phase correlation between imgA and imgB.
// The result measures delta such that imgB(x, y) = imgA(x - delta.X, y - delta.Y).
func ComputePhaseCorrelation(imgA, imgB image.Image, config PhaseCorrelationConfig) (RegistrationResult, error) {
	if imgA == nil || imgB == nil {
		return RegistrationResult{}, errors.New("nil image provided")
	}
	bA := imgA.Bounds()
	bB := imgB.Bounds()
	if bA.Empty() || bB.Empty() {
		return RegistrationResult{}, errors.New("empty image bounds")
	}

	maxW := int(math.Max(float64(bA.Dx()), float64(bB.Dx())))
	maxH := int(math.Max(float64(bA.Dy()), float64(bB.Dy())))
	padW := NextPowerOf2(maxW)
	padH := NextPowerOf2(maxH)

	matA, err := imageToWindowedMatrix(imgA, padH, padW, config.ApplyHanning)
	if err != nil {
		return RegistrationResult{}, err
	}
	matB, err := imageToWindowedMatrix(imgB, padH, padW, config.ApplyHanning)
	if err != nil {
		return RegistrationResult{}, err
	}

	if err := FFT2D(matA, false); err != nil {
		return RegistrationResult{}, fmt.Errorf("FFT2D matA: %w", err)
	}
	if err := FFT2D(matB, false); err != nil {
		return RegistrationResult{}, fmt.Errorf("FFT2D matB: %w", err)
	}

	// Normalized cross-power spectrum: R(u, v) = (F_B * conj(F_A)) / (|F_B * conj(F_A)| + eps)
	crossPower := NewComplexMatrix(padH, padW)
	eps := 1e-12
	for i := 0; i < padH*padW; i++ {
		fa := matA.Data[i]
		fb := matB.Data[i]
		prod := fb * cmplx.Conj(fa)
		mag := cmplx.Abs(prod)
		crossPower.Data[i] = prod / complex(mag+eps, 0)
	}

	if err := FFT2D(crossPower, true); err != nil {
		return RegistrationResult{}, fmt.Errorf("IFFT2D crossPower: %w", err)
	}

	// Extract real impulse response surface
	realSurf := make([]float64, padH*padW)
	for i := 0; i < padH*padW; i++ {
		realSurf[i] = real(crossPower.Data[i])
	}

	// Find global maximum peak
	peakIdx := 0
	maxVal := realSurf[0]
	for i := 1; i < len(realSurf); i++ {
		if realSurf[i] > maxVal {
			maxVal = realSurf[i]
			peakIdx = i
		}
	}

	peakY := peakIdx / padW
	peakX := peakIdx % padW

	// Compute PSR & secondary peak
	psr, peakRatio := computePSR(realSurf, padH, padW, peakY, peakX)

	// Wrap around unwrapping (fftshift equivalent)
	dx := float64(peakX)
	if peakX >= padW/2 {
		dx = float64(peakX - padW)
	}
	dy := float64(peakY)
	if peakY >= padH/2 {
		dy = float64(peakY - padH)
	}

	// Subpixel refinement
	if config.SubpixelMethod == "sinc" {
		subDX, subDY := RefineSinc(realSurf, padH, padW, peakY, peakX)
		dx += subDX
		dy += subDY
	} else if config.SubpixelMethod == "parabolic" {
		subDX, subDY := RefineParabolic(realSurf, padH, padW, peakY, peakX)
		dx += subDX
		dy += subDY
	} else if config.SubpixelMethod == "centroid" {
		subDX, subDY := RefineCentroid(realSurf, padH, padW, peakY, peakX)
		dx += subDX
		dy += subDY
	}

	confidence := math.Max(0.0, math.Min(1.0, (psr-3.0)/15.0))

	// Check if fallback to NCC is needed
	if config.EnableNCCFallback && (psr < config.PSRThreshold || peakRatio < config.PeakRatioThreshold) {
		nccRes, nccErr := MatchTemplateNCC(imgA, imgB, geometry.Vector{X: dx, Y: dy})
		if nccErr == nil && nccRes.Confidence > confidence {
			nccRes.Method = "ncc_fallback"
			return nccRes, nil
		}
	}

	return RegistrationResult{
		Delta:      geometry.Vector{X: dx, Y: dy},
		Confidence: confidence,
		PeakRatio:  peakRatio,
		PSR:        psr,
		Method:     "phase_correlation",
	}, nil
}

func computePSR(surf []float64, rows, cols, peakY, peakX int) (psr float64, peakRatio float64) {
	peakVal := surf[peakY*cols+peakX]
	radius := 3
	var sum, sqSum float64
	var count float64
	secondMax := -math.MaxFloat64

	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			dr := math.Abs(float64(r - peakY))
			if dr > float64(rows/2) {
				dr = float64(rows) - dr
			}
			dc := math.Abs(float64(c - peakX))
			if dc > float64(cols/2) {
				dc = float64(cols) - dc
			}

			val := surf[r*cols+c]
			if dr > float64(radius) || dc > float64(radius) {
				sum += val
				sqSum += val * val
				count++
				if val > secondMax {
					secondMax = val
				}
			}
		}
	}

	if count <= 0 {
		return 0, 1
	}
	mean := sum / count
	variance := (sqSum / count) - (mean * mean)
	std := 0.0
	if variance > 0 {
		std = math.Sqrt(variance)
	}
	psr = (peakVal - mean) / (std + 1e-12)
	peakRatio = peakVal / (secondMax + 1e-12)
	return psr, peakRatio
}

// RegisterTiles is the standard public registration API fulfilling PROJECT.md contract.
func RegisterTiles(imgA, imgB image.Image, nominal geometry.Vector) (RegistrationResult, error) {
	cfg := DefaultConfig()
	res, err := ComputePhaseCorrelation(imgA, imgB, cfg)
	if err != nil && cfg.EnableNCCFallback {
		return MatchTemplateNCC(imgA, imgB, nominal)
	}
	return res, err
}

// PhaseCorrEstimator implements capture.TranslationEstimator using phase correlation.
type PhaseCorrEstimator struct {
	Config PhaseCorrelationConfig
}

// NewPhaseCorrEstimator returns a PhaseCorrEstimator with default configuration.
func NewPhaseCorrEstimator() *PhaseCorrEstimator {
	return &PhaseCorrEstimator{Config: DefaultConfig()}
}

// EstimateTranslation calculates translation delta and confidence between two frames.
func (e *PhaseCorrEstimator) EstimateTranslation(before, after image.Image) (geometry.Vector, float64, error) {
	res, err := ComputePhaseCorrelation(before, after, e.Config)
	if err != nil {
		return geometry.Vector{}, 0, err
	}
	return res.Delta, res.Confidence, nil
}
