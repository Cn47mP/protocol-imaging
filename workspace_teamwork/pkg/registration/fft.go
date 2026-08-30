package registration

import (
	"errors"
	"math"
	"math/bits"
)

var ErrInvalidDimensions = errors.New("dimensions must be positive and power of two")

// ComplexMatrix represents a 2D matrix of complex128 numbers with contiguous row-major storage.
type ComplexMatrix struct {
	Rows int
	Cols int
	Data []complex128
}

// NewComplexMatrix allocates a new ComplexMatrix with the specified rows and columns.
func NewComplexMatrix(rows, cols int) *ComplexMatrix {
	return &ComplexMatrix{
		Rows: rows,
		Cols: cols,
		Data: make([]complex128, rows*cols),
	}
}

// At returns the value at (r, c).
func (m *ComplexMatrix) At(r, c int) complex128 {
	return m.Data[r*m.Cols+c]
}

// Set sets the value at (r, c).
func (m *ComplexMatrix) Set(r, c int, val complex128) {
	m.Data[r*m.Cols+c] = val
}

// Clone creates a deep copy of the ComplexMatrix.
func (m *ComplexMatrix) Clone() *ComplexMatrix {
	dup := NewComplexMatrix(m.Rows, m.Cols)
	copy(dup.Data, m.Data)
	return dup
}

// NextPowerOf2 computes the smallest power of 2 >= n.
func NextPowerOf2(n int) int {
	if n <= 1 {
		return 1
	}
	return 1 << bits.Len(uint(n-1))
}

// IsPowerOf2 checks if n is a positive power of 2.
func IsPowerOf2(n int) bool {
	return n > 0 && (n&(n-1)) == 0
}

// FFT1D computes the in-place 1D Radix-2 Cooley-Tukey FFT/IFFT.
func FFT1D(data []complex128, inverse bool) error {
	n := len(data)
	if !IsPowerOf2(n) {
		return ErrInvalidDimensions
	}
	if n <= 1 {
		return nil
	}

	// Bit reversal permutation
	shift := 64 - bits.Len(uint(n-1))
	for i := 0; i < n; i++ {
		j := int(bits.Reverse64(uint64(i)) >> shift)
		if i < j {
			data[i], data[j] = data[j], data[i]
		}
	}

	// In-place Cooley-Tukey butterfly stages
	for length := 2; length <= n; length <<= 1 {
		halfLen := length >> 1
		angle := -2.0 * math.Pi / float64(length)
		if inverse {
			angle = 2.0 * math.Pi / float64(length)
		}
		wStep := complex(math.Cos(angle), math.Sin(angle))

		for i := 0; i < n; i += length {
			w := complex(1.0, 0.0)
			for j := 0; j < halfLen; j++ {
				u := data[i+j]
				v := data[i+j+halfLen] * w
				data[i+j] = u + v
				data[i+j+halfLen] = u - v
				w *= wStep
			}
		}
	}

	// Scale by 1/N for inverse transform
	if inverse {
		scale := 1.0 / float64(n)
		for i := 0; i < n; i++ {
			data[i] = complex(real(data[i])*scale, imag(data[i])*scale)
		}
	}
	return nil
}

// FFT2D computes in-place 2D FFT / IFFT on ComplexMatrix.
func FFT2D(m *ComplexMatrix, inverse bool) error {
	if m == nil || !IsPowerOf2(m.Rows) || !IsPowerOf2(m.Cols) {
		return ErrInvalidDimensions
	}

	// Transform rows
	rowBuf := make([]complex128, m.Cols)
	for r := 0; r < m.Rows; r++ {
		offset := r * m.Cols
		copy(rowBuf, m.Data[offset:offset+m.Cols])
		if err := FFT1D(rowBuf, inverse); err != nil {
			return err
		}
		copy(m.Data[offset:offset+m.Cols], rowBuf)
	}

	// Transform columns
	colBuf := make([]complex128, m.Rows)
	for c := 0; c < m.Cols; c++ {
		for r := 0; r < m.Rows; r++ {
			colBuf[r] = m.Data[r*m.Cols+c]
		}
		if err := FFT1D(colBuf, inverse); err != nil {
			return err
		}
		for r := 0; r < m.Rows; r++ {
			m.Data[r*m.Cols+c] = colBuf[r]
		}
	}
	return nil
}
