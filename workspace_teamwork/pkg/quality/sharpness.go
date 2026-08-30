package quality

import (
	"errors"
	"fmt"
	"image"
)

// SharpnessConfig specifies parameters for image sharpness evaluation.
type SharpnessConfig struct {
	MinLaplacianVariance float64 `json:"min_laplacian_variance"`
	MinTenengrad         float64 `json:"min_tenengrad"`
	MinBrenner           float64 `json:"min_brenner"`
	BaselineVariance     float64 `json:"baseline_variance"`
	RelativeThreshold    float64 `json:"relative_threshold"`  // e.g. 0.5 (must be >= 50% of baseline)
	TenengradThreshold   float64 `json:"tenengrad_threshold"` // Gradient magnitude threshold (e.g. 100.0)
}

// DefaultSharpnessConfig provides sensible default thresholds for game overview screenshots.
func DefaultSharpnessConfig() SharpnessConfig {
	return SharpnessConfig{
		MinLaplacianVariance: 50.0,
		MinTenengrad:         100.0,
		MinBrenner:           20.0,
		BaselineVariance:     0.0,
		RelativeThreshold:    0.5,
		TenengradThreshold:   100.0,
	}
}

// SharpnessReport contains quantitative sharpness metrics and gate pass/fail decision.
type SharpnessReport struct {
	LaplacianVariance float64 `json:"laplacian_variance"`
	TenengradScore    float64 `json:"tenengrad_score"`
	BrennerScore      float64 `json:"brenner_score"`
	RelativeScore     float64 `json:"relative_score"`
	Passed            bool    `json:"passed"`
	Reason            string  `json:"reason,omitempty"`
}

// ToGrayscaleMatrix converts an image.Image into a 2D float64 slice of normalized luminance [0, 255].
func ToGrayscaleMatrix(img image.Image) [][]float64 {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	grid := make([][]float64, h)
	for y := 0; y < h; y++ {
		grid[y] = make([]float64, w)
	}

	switch src := img.(type) {
	case *image.RGBA:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				idx := src.PixOffset(bounds.Min.X+x, bounds.Min.Y+y)
				r := float64(src.Pix[idx])
				g := float64(src.Pix[idx+1])
				b := float64(src.Pix[idx+2])
				grid[y][x] = 0.299*r + 0.587*g + 0.114*b
			}
		}
	case *image.NRGBA:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				idx := src.PixOffset(bounds.Min.X+x, bounds.Min.Y+y)
				r := float64(src.Pix[idx])
				g := float64(src.Pix[idx+1])
				b := float64(src.Pix[idx+2])
				grid[y][x] = 0.299*r + 0.587*g + 0.114*b
			}
		}
	case *image.Gray:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				idx := src.PixOffset(bounds.Min.X+x, bounds.Min.Y+y)
				grid[y][x] = float64(src.Pix[idx])
			}
		}
	default:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r, g, b, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
				grid[y][x] = 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(b>>8)
			}
		}
	}
	return grid
}

// ComputeLaplacianVariance calculates the sample variance of the discrete 3x3 Laplacian operator.
func ComputeLaplacianVariance(img image.Image) float64 {
	gray := ToGrayscaleMatrix(img)
	h := len(gray)
	if h < 3 {
		return 0
	}
	w := len(gray[0])
	if w < 3 {
		return 0
	}

	var sum float64
	var sumSq float64
	count := float64((w - 2) * (h - 2))

	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			// Kernel: [0, 1, 0; 1, -4, 1; 0, 1, 0]
			lap := gray[y-1][x] + gray[y+1][x] + gray[y][x-1] + gray[y][x+1] - 4.0*gray[y][x]
			sum += lap
			sumSq += lap * lap
		}
	}

	mean := sum / count
	variance := (sumSq / count) - (mean * mean)
	if variance < 0 {
		return 0
	}
	return variance
}

// ComputeTenengrad calculates the mean Sobel gradient energy above threshold.
func ComputeTenengrad(img image.Image, threshold float64) float64 {
	gray := ToGrayscaleMatrix(img)
	h := len(gray)
	if h < 3 {
		return 0
	}
	w := len(gray[0])
	if w < 3 {
		return 0
	}

	var totalEnergy float64
	count := float64((w - 2) * (h - 2))
	threshSq := threshold * threshold

	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			// Sobel X: [-1, 0, 1; -2, 0, 2; -1, 0, 1]
			gx := (gray[y-1][x+1] + 2.0*gray[y][x+1] + gray[y+1][x+1]) -
				(gray[y-1][x-1] + 2.0*gray[y][x-1] + gray[y+1][x-1])
			// Sobel Y: [-1, -2, -1; 0, 0, 0; 1, 2, 1]
			gy := (gray[y+1][x-1] + 2.0*gray[y+1][x] + gray[y+1][x+1]) -
				(gray[y-1][x-1] + 2.0*gray[y-1][x] + gray[y-1][x+1])

			magSq := gx*gx + gy*gy
			if magSq >= threshSq {
				totalEnergy += magSq
			}
		}
	}
	return totalEnergy / count
}

// ComputeBrenner calculates the 2nd-order difference focus metric.
func ComputeBrenner(img image.Image) float64 {
	gray := ToGrayscaleMatrix(img)
	h := len(gray)
	if h < 3 {
		return 0
	}
	w := len(gray[0])
	if w < 3 {
		return 0
	}

	var total float64
	count := float64((w - 2) * (h - 2))

	for y := 0; y < h-2; y++ {
		for x := 0; x < w-2; x++ {
			dx := gray[y][x+2] - gray[y][x]
			dy := gray[y+2][x] - gray[y][x]
			total += dx*dx + dy*dy
		}
	}
	return total / count
}

// EvaluateSharpness computes all sharpness metrics and determines if image passes quality gating.
func EvaluateSharpness(img image.Image, config SharpnessConfig) (SharpnessReport, error) {
	if img == nil {
		return SharpnessReport{}, errors.New("image cannot be nil")
	}
	bounds := img.Bounds()
	if bounds.Dx() < 4 || bounds.Dy() < 4 {
		return SharpnessReport{}, fmt.Errorf("image dimensions too small: %dx%d", bounds.Dx(), bounds.Dy())
	}

	lapVar := ComputeLaplacianVariance(img)
	tenengrad := ComputeTenengrad(img, config.TenengradThreshold)
	brenner := ComputeBrenner(img)

	var relative float64 = 1.0
	if config.BaselineVariance > 1e-6 {
		relative = lapVar / config.BaselineVariance
	}

	report := SharpnessReport{
		LaplacianVariance: lapVar,
		TenengradScore:    tenengrad,
		BrennerScore:      brenner,
		RelativeScore:     relative,
		Passed:            true,
	}

	if lapVar < config.MinLaplacianVariance {
		report.Passed = false
		report.Reason = fmt.Sprintf("laplacian variance %0.2f below threshold %0.2f", lapVar, config.MinLaplacianVariance)
		return report, nil
	}
	if tenengrad < config.MinTenengrad {
		report.Passed = false
		report.Reason = fmt.Sprintf("tenengrad score %0.2f below threshold %0.2f", tenengrad, config.MinTenengrad)
		return report, nil
	}
	if config.BaselineVariance > 1e-6 && relative < config.RelativeThreshold {
		report.Passed = false
		report.Reason = fmt.Sprintf("relative sharpness %0.2f below session threshold %0.2f", relative, config.RelativeThreshold)
		return report, nil
	}

	return report, nil
}
