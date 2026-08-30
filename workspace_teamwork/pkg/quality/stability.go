package quality

import (
	"errors"
	"fmt"
	"image"
	"math"
)

// StabilityConfig specifies motion stability gating thresholds across consecutive frames.
type StabilityConfig struct {
	MaxMAD             float64 `json:"max_mad"`              // Max mean absolute difference [0, 255] (default: 2.5)
	MaxDynamicRatio    float64 `json:"max_dynamic_ratio"`    // Max moving pixel ratio [0, 1] (default: 0.03)
	PixelDiffThreshold float64 `json:"pixel_diff_threshold"` // Min intensity diff to count as dynamic pixel (default: 15.0)
	MinPSNR            float64 `json:"min_psnr"`             // Min PSNR in dB (default: 32.0)
}

// DefaultStabilityConfig returns default stability parameters.
func DefaultStabilityConfig() StabilityConfig {
	return StabilityConfig{
		MaxMAD:             2.5,
		MaxDynamicRatio:    0.03,
		PixelDiffThreshold: 15.0,
		MinPSNR:            32.0,
	}
}

// FrameDiffReport captures difference metrics between two frames.
type FrameDiffReport struct {
	MAD          float64 `json:"mad"`
	MSE          float64 `json:"mse"`
	PSNR         float64 `json:"psnr"`
	DynamicRatio float64 `json:"dynamic_ratio"`
	IsStable     bool    `json:"is_stable"`
	Reason       string  `json:"reason,omitempty"`
}

// ComputeFrameDifference evaluates difference metrics between two same-dimension frames.
func ComputeFrameDifference(imgA, imgB image.Image, config StabilityConfig) (FrameDiffReport, error) {
	if imgA == nil || imgB == nil {
		return FrameDiffReport{}, errors.New("input frames cannot be nil")
	}
	bA := imgA.Bounds()
	bB := imgB.Bounds()
	if bA.Dx() != bB.Dx() || bA.Dy() != bB.Dy() {
		return FrameDiffReport{}, fmt.Errorf("frame size mismatch: %dx%d vs %dx%d", bA.Dx(), bA.Dy(), bB.Dx(), bB.Dy())
	}

	grayA := ToGrayscaleMatrix(imgA)
	grayB := ToGrayscaleMatrix(imgB)
	h := len(grayA)
	w := len(grayA[0])
	totalPixels := float64(w * h)
	if totalPixels <= 0 {
		return FrameDiffReport{}, errors.New("empty frame dimensions")
	}

	var sumAbsDiff float64
	var sumSqDiff float64
	var dynamicCount float64

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			diff := math.Abs(grayA[y][x] - grayB[y][x])
			sumAbsDiff += diff
			sumSqDiff += diff * diff
			if diff >= config.PixelDiffThreshold {
				dynamicCount++
			}
		}
	}

	mad := sumAbsDiff / totalPixels
	mse := sumSqDiff / totalPixels
	var psnr float64
	if mse < 1e-9 {
		psnr = 100.0
	} else {
		psnr = 10.0 * math.Log10((255.0*255.0)/mse)
	}
	dynamicRatio := dynamicCount / totalPixels

	report := FrameDiffReport{
		MAD:          mad,
		MSE:          mse,
		PSNR:         psnr,
		DynamicRatio: dynamicRatio,
		IsStable:     true,
	}

	if mad > config.MaxMAD {
		report.IsStable = false
		report.Reason = fmt.Sprintf("MAD %0.2f exceeds max %0.2f", mad, config.MaxMAD)
	} else if dynamicRatio > config.MaxDynamicRatio {
		report.IsStable = false
		report.Reason = fmt.Sprintf("dynamic pixel ratio %0.3f exceeds max %0.3f", dynamicRatio, config.MaxDynamicRatio)
	} else if psnr < config.MinPSNR {
		report.IsStable = false
		report.Reason = fmt.Sprintf("PSNR %0.2fdB below min %0.2fdB", psnr, config.MinPSNR)
	}

	return report, nil
}

// SelectBestFrame chooses the sharpest stable frame from a burst sequence.
func SelectBestFrame(frames []image.Image, stabCfg StabilityConfig, sharpCfg SharpnessConfig) (int, FrameDiffReport, error) {
	if len(frames) == 0 {
		return -1, FrameDiffReport{}, errors.New("empty frame burst")
	}
	if len(frames) == 1 {
		rep, err := EvaluateSharpness(frames[0], sharpCfg)
		if err != nil {
			return 0, FrameDiffReport{}, err
		}
		return 0, FrameDiffReport{IsStable: rep.Passed, PSNR: 100.0}, nil
	}

	bestIdx := 0
	var bestScore float64 = -1.0
	var lastReport FrameDiffReport

	for i := 0; i < len(frames); i++ {
		sharpReport, err := EvaluateSharpness(frames[i], sharpCfg)
		if err != nil {
			continue
		}

		var dynamicRatio float64 = 0.0
		var isStable = true

		if i > 0 {
			diffReport, err := ComputeFrameDifference(frames[i-1], frames[i], stabCfg)
			if err == nil {
				lastReport = diffReport
				dynamicRatio = diffReport.DynamicRatio
				isStable = diffReport.IsStable
			}
		}

		if isStable && sharpReport.Passed {
			score := sharpReport.LaplacianVariance * (1.0 - dynamicRatio)
			if score > bestScore {
				bestScore = score
				bestIdx = i
			}
		}
	}

	return bestIdx, lastReport, nil
}
