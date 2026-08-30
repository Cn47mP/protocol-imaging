package registration

import (
	"errors"
	"image"
	"image/color"
	"math"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
)

// MatchTemplateNCC performs Zero-mean Normalized Cross-Correlation around nominal search area.
func MatchTemplateNCC(imgA, imgB image.Image, nominal geometry.Vector) (RegistrationResult, error) {
	return MatchTemplateNCCWindow(imgA, imgB, nominal, 32, 128)
}

// MatchTemplateNCCWindow runs NCC with an explicit local search window. It is
// used when capture geometry already predicts the overlap and only a small
// residual correction is expected.
func MatchTemplateNCCWindow(imgA, imgB image.Image, nominal geometry.Vector, searchRadius, maxTemplateSize int) (RegistrationResult, error) {
	if imgA == nil || imgB == nil {
		return RegistrationResult{}, errors.New("nil image provided")
	}
	bA := imgA.Bounds()
	bB := imgB.Bounds()
	if bA.Empty() || bB.Empty() {
		return RegistrationResult{}, errors.New("empty image bounds")
	}

	// Select central template from imgA
	if maxTemplateSize < 8 {
		maxTemplateSize = 8
	}
	if searchRadius < 0 {
		searchRadius = 0
	}
	tmplW := int(math.Min(float64(maxTemplateSize), float64(bA.Dx())*0.6))
	tmplH := int(math.Min(float64(maxTemplateSize), float64(bA.Dy())*0.6))
	if tmplW < 8 || tmplH < 8 {
		return RegistrationResult{}, errors.New("image too small for NCC template matching")
	}

	tMinX := bA.Min.X + (bA.Dx()-tmplW)/2
	tMinY := bA.Min.Y + (bA.Dy()-tmplH)/2

	tmpl := make([]float64, tmplW*tmplH)
	var tSum, tSqSum float64
	for y := 0; y < tmplH; y++ {
		for x := 0; x < tmplW; x++ {
			c := color.GrayModel.Convert(imgA.At(tMinX+x, tMinY+y)).(color.Gray)
			val := float64(c.Y)
			tmpl[y*tmplW+x] = val
			tSum += val
			tSqSum += val * val
		}
	}
	n := float64(tmplW * tmplH)
	tMean := tSum / n
	tVar := tSqSum - n*tMean*tMean
	if tVar < 1e-6 {
		return RegistrationResult{
			Delta:      nominal,
			Confidence: 0.2,
			Method:     "ncc_low_var",
		}, nil
	}
	tStd := math.Sqrt(tVar)

	nomX := int(math.Round(nominal.X))
	nomY := int(math.Round(nominal.Y))

	bestScore := -2.0
	bestDX := nomX
	bestDY := nomY

	scores := make(map[[2]int]float64)

	for dy := nomY - searchRadius; dy <= nomY+searchRadius; dy++ {
		for dx := nomX - searchRadius; dx <= nomX+searchRadius; dx++ {
			targetX := tMinX + dx
			targetY := tMinY + dy
			if targetX < bB.Min.X || targetX+tmplW > bB.Max.X || targetY < bB.Min.Y || targetY+tmplH > bB.Max.Y {
				continue
			}

			var sSum, sSqSum, crossSum float64
			for y := 0; y < tmplH; y++ {
				for x := 0; x < tmplW; x++ {
					c := color.GrayModel.Convert(imgB.At(targetX+x, targetY+y)).(color.Gray)
					val := float64(c.Y)
					sSum += val
					sSqSum += val * val
					crossSum += val * (tmpl[y*tmplW+x] - tMean)
				}
			}

			sMean := sSum / n
			sVar := sSqSum - n*sMean*sMean
			if sVar < 1e-6 {
				continue
			}
			sStd := math.Sqrt(sVar)
			score := crossSum / (tStd * sStd)
			scores[[2]int{dx, dy}] = score

			if score > bestScore {
				bestScore = score
				bestDX = dx
				bestDY = dy
			}
		}
	}

	if bestScore < -1.0 {
		return RegistrationResult{
			Delta:      nominal,
			Confidence: 0.1,
			Method:     "ncc_no_match",
		}, nil
	}

	// 1D parabolic subpixel interpolation on NCC score surface
	subDX, subDY := 0.0, 0.0
	s0 := bestScore
	sLeft, okL := scores[[2]int{bestDX - 1, bestDY}]
	sRight, okR := scores[[2]int{bestDX + 1, bestDY}]
	if okL && okR {
		denom := 2.0 * (sLeft - 2.0*s0 + sRight)
		if math.Abs(denom) > 1e-9 && denom < 0 {
			subDX = math.Max(-0.5, math.Min(0.5, (sLeft-sRight)/denom))
		}
	}

	sUp, okU := scores[[2]int{bestDX, bestDY - 1}]
	sDown, okD := scores[[2]int{bestDX, bestDY + 1}]
	if okU && okD {
		denom := 2.0 * (sUp - 2.0*s0 + sDown)
		if math.Abs(denom) > 1e-9 && denom < 0 {
			subDY = math.Max(-0.5, math.Min(0.5, (sUp-sDown)/denom))
		}
	}

	conf := math.Max(0.0, math.Min(1.0, (bestScore+1.0)/2.0))
	return RegistrationResult{
		Delta:      geometry.Vector{X: float64(bestDX) + subDX, Y: float64(bestDY) + subDY},
		Confidence: conf,
		PeakRatio:  1.5,
		PSR:        math.Max(0, bestScore*10.0),
		Method:     "ncc",
	}, nil
}
