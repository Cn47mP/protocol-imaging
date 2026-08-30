package registration

import "math"

// RefineParabolic fits 1D parabolas around the 3x3 neighborhood of (peakY, peakX)
// to estimate subpixel peak offset (subDX, subDY) within [-0.5, 0.5].
func RefineParabolic(surf []float64, rows, cols, peakY, peakX int) (subDX, subDY float64) {
	if rows < 3 || cols < 3 || len(surf) < rows*cols {
		return 0, 0
	}

	get := func(r, c int) float64 {
		rr := (r + rows) % rows
		cc := (c + cols) % cols
		return surf[rr*cols+cc]
	}

	c0 := get(peakY, peakX)

	// X axis parabolic refinement
	cLeft := get(peakY, peakX-1)
	cRight := get(peakY, peakX+1)
	denomX := 2.0 * (cLeft - 2.0*c0 + cRight)
	if math.Abs(denomX) > 1e-9 && denomX < 0 {
		subDX = (cLeft - cRight) / denomX
		subDX = math.Max(-0.5, math.Min(0.5, subDX))
	}

	// Y axis parabolic refinement
	cUp := get(peakY-1, peakX)
	cDown := get(peakY+1, peakX)
	denomY := 2.0 * (cUp - 2.0*c0 + cDown)
	if math.Abs(denomY) > 1e-9 && denomY < 0 {
		subDY = (cUp - cDown) / denomY
		subDY = math.Max(-0.5, math.Min(0.5, subDY))
	}

	return subDX, subDY
}

// RefineSinc implements Foroosh et al.'s analytical sinc interpolation for phase correlation peaks.
func RefineSinc(surf []float64, rows, cols, peakY, peakX int) (subDX, subDY float64) {
	if rows < 3 || cols < 3 || len(surf) < rows*cols {
		return 0, 0
	}

	get := func(r, c int) float64 {
		rr := (r + rows) % rows
		cc := (c + cols) % cols
		return surf[rr*cols+cc]
	}

	c0 := get(peakY, peakX)
	if c0 <= 0 {
		return RefineParabolic(surf, rows, cols, peakY, peakX)
	}

	// X axis
	cLeft := get(peakY, peakX-1)
	cRight := get(peakY, peakX+1)
	if cRight > cLeft && (cRight+c0) > 1e-9 {
		subDX = cRight / (cRight + c0)
	} else if cLeft > cRight && (cLeft+c0) > 1e-9 {
		subDX = -cLeft / (cLeft + c0)
	}

	// Y axis
	cUp := get(peakY-1, peakX)
	cDown := get(peakY+1, peakX)
	if cDown > cUp && (cDown+c0) > 1e-9 {
		subDY = cDown / (cDown + c0)
	} else if cUp > cDown && (cUp+c0) > 1e-9 {
		subDY = -cUp / (cUp + c0)
	}

	subDX = math.Max(-0.5, math.Min(0.5, subDX))
	subDY = math.Max(-0.5, math.Min(0.5, subDY))
	return subDX, subDY
}

// RefineCentroid calculates center-of-mass subpixel shift over the 3x3 window around (peakY, peakX).
func RefineCentroid(surf []float64, rows, cols, peakY, peakX int) (subDX, subDY float64) {
	if rows < 3 || cols < 3 || len(surf) < rows*cols {
		return 0, 0
	}

	get := func(r, c int) float64 {
		rr := (r + rows) % rows
		cc := (c + cols) % cols
		return surf[rr*cols+cc]
	}

	minVal := math.MaxFloat64
	for dr := -1; dr <= 1; dr++ {
		for dc := -1; dc <= 1; dc++ {
			v := get(peakY+dr, peakX+dc)
			if v < minVal {
				minVal = v
			}
		}
	}

	var totalW, sumX, sumY float64
	for dr := -1; dr <= 1; dr++ {
		for dc := -1; dc <= 1; dc++ {
			w := get(peakY+dr, peakX+dc) - minVal
			if w > 0 {
				totalW += w
				sumX += float64(dc) * w
				sumY += float64(dr) * w
			}
		}
	}

	if totalW > 1e-12 {
		subDX = math.Max(-0.5, math.Min(0.5, sumX/totalW))
		subDY = math.Max(-0.5, math.Min(0.5, sumY/totalW))
	}
	return subDX, subDY
}
