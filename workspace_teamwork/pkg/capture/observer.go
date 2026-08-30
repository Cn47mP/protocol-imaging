package capture

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
)

type TranslationEstimator interface {
	EstimateTranslation(before, after image.Image) (delta geometry.Vector, confidence float64, err error)
}

type MotionObserver struct {
	config    FrontierConfig
	estimator TranslationEstimator
}

func NewMotionObserver(config FrontierConfig, estimator TranslationEstimator) *MotionObserver {
	return &MotionObserver{
		config:    config,
		estimator: estimator,
	}
}

func (observer *MotionObserver) Classify(before, after Frame, intent MoveIntent, evidenceID string) (MotionObservation, error) {
	if err := intent.Validate(); err != nil {
		return MotionObservation{}, fmt.Errorf("invalid move intent: %w", err)
	}
	if evidenceID == "" {
		return MotionObservation{}, errors.New("evidence id is required")
	}
	if before.Image == nil || after.Image == nil {
		return MotionObservation{
			Kind:        MotionUncertain,
			Direction:   intent.Direction,
			Delta:       geometry.Vector{},
			Confidence:  0,
			EvidenceIDs: []string{evidenceID},
		}, nil
	}

	if observer.estimator == nil {
		return MotionObservation{
			Kind:        MotionUncertain,
			Direction:   intent.Direction,
			Delta:       geometry.Vector{},
			Confidence:  0,
			EvidenceIDs: []string{evidenceID},
		}, nil
	}

	delta, conf, err := observer.estimator.EstimateTranslation(before.Image, after.Image)
	if err != nil || math.IsNaN(conf) || math.IsInf(conf, 0) || conf < observer.config.MinimumConfidence {
		return MotionObservation{
			Kind:        MotionUncertain,
			Direction:   intent.Direction,
			Delta:       geometry.Vector{},
			Confidence:  math.Max(0, math.Min(1, conf)),
			EvidenceIDs: []string{evidenceID},
		}, nil
	}

	directed := intent.Direction.DirectedDelta(delta)
	orthogonal := math.Abs(intent.Direction.OrthogonalDelta(delta))

	if orthogonal > observer.config.CrossAxisTolerance {
		return MotionObservation{
			Kind:        MotionUncertain,
			Direction:   intent.Direction,
			Delta:       geometry.Vector{},
			Confidence:  conf,
			EvidenceIDs: []string{evidenceID},
		}, nil
	}

	if math.Abs(directed) <= observer.config.ClampTolerance {
		return MotionObservation{
			Kind:        MotionClamped,
			Direction:   intent.Direction,
			Delta:       geometry.Vector{},
			Confidence:  conf,
			EvidenceIDs: []string{evidenceID},
		}, nil
	}

	fullThreshold := intent.Distance - observer.config.ClampTolerance
	if directed >= fullThreshold {
		return MotionObservation{
			Kind:        MotionMoved,
			Direction:   intent.Direction,
			Delta:       delta,
			Confidence:  conf,
			EvidenceIDs: []string{evidenceID},
		}, nil
	}

	if directed > observer.config.ClampTolerance {
		return MotionObservation{
			Kind:        MotionPartial,
			Direction:   intent.Direction,
			Delta:       delta,
			Confidence:  conf,
			EvidenceIDs: []string{evidenceID},
		}, nil
	}

	return MotionObservation{
		Kind:        MotionClamped,
		Direction:   intent.Direction,
		Delta:       geometry.Vector{},
		Confidence:  conf,
		EvidenceIDs: []string{evidenceID},
	}, nil
}

// SimpleTranslationEstimator provides a pure-Go template-matching / cross-correlation estimator.
type SimpleTranslationEstimator struct {
	SearchRadius int
	WindowSize   int
}

func NewSimpleTranslationEstimator(searchRadius, windowSize int) *SimpleTranslationEstimator {
	if searchRadius <= 0 {
		searchRadius = 64
	}
	if windowSize <= 0 {
		windowSize = 64
	}
	return &SimpleTranslationEstimator{
		SearchRadius: searchRadius,
		WindowSize:   windowSize,
	}
}

func (e *SimpleTranslationEstimator) EstimateTranslation(before, after image.Image) (geometry.Vector, float64, error) {
	if before == nil || after == nil {
		return geometry.Vector{}, 0, errors.New("nil image")
	}
	bBounds := before.Bounds()
	aBounds := after.Bounds()
	if bBounds.Empty() || aBounds.Empty() {
		return geometry.Vector{}, 0, errors.New("empty bounds")
	}

	// Center window
	w := e.WindowSize
	if w > bBounds.Dx() || w > bBounds.Dy() {
		w = int(math.Min(float64(bBounds.Dx()), float64(bBounds.Dy())))
	}
	if w <= 0 {
		return geometry.Vector{}, 0, errors.New("image too small")
	}

	centerX := bBounds.Min.X + bBounds.Dx()/2
	centerY := bBounds.Min.Y + bBounds.Dy()/2
	minX := centerX - w/2
	minY := centerY - w/2
	maxX := minX + w
	maxY := minY + w

	// Sample template grayscale
	tmpl := make([]float64, w*w)
	var tmplSum, tmplSqSum float64
	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			c := color.GrayModel.Convert(before.At(x, y)).(color.Gray)
			val := float64(c.Y)
			idx := (y-minY)*w + (x - minX)
			tmpl[idx] = val
			tmplSum += val
			tmplSqSum += val * val
		}
	}
	n := float64(w * w)
	tmplMean := tmplSum / n
	tmplVar := tmplSqSum - n*tmplMean*tmplMean
	if tmplVar < 1e-6 {
		// Uniform area, return identity with moderate confidence
		return geometry.Vector{X: 0, Y: 0}, 0.5, nil
	}
	tmplStd := math.Sqrt(tmplVar)

	bestScore := -2.0
	bestDX := 0
	bestDY := 0

	radius := e.SearchRadius
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			// Check if target window is within after bounds
			tMinX := minX + dx
			tMinY := minY + dy
			tMaxX := tMinX + w
			tMaxY := tMinY + w
			if tMinX < aBounds.Min.X || tMaxX > aBounds.Max.X || tMinY < aBounds.Min.Y || tMaxY > aBounds.Max.Y {
				continue
			}

			var targetSum, targetSqSum, dotProd float64
			for y := 0; y < w; y++ {
				for x := 0; x < w; x++ {
					c := color.GrayModel.Convert(after.At(tMinX+x, tMinY+y)).(color.Gray)
					val := float64(c.Y)
					targetSum += val
					targetSqSum += val * val
					dotProd += (val) * (tmpl[y*w+x] - tmplMean)
				}
			}
			targetMean := targetSum / n
			targetVar := targetSqSum - n*targetMean*targetMean
			if targetVar < 1e-6 {
				continue
			}
			targetStd := math.Sqrt(targetVar)
			ncc := dotProd / (tmplStd * targetStd)
			if ncc > bestScore {
				bestScore = ncc
				bestDX = dx
				bestDY = dy
			}
		}
	}

	if bestScore < 0 {
		return geometry.Vector{}, 0, nil
	}
	// Image feature shifted by (bestDX, bestDY), so camera displacement is (-bestDX, -bestDY)
	return geometry.Vector{X: -float64(bestDX), Y: -float64(bestDY)}, math.Max(0, math.Min(1, bestScore)), nil
}
