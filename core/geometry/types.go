package geometry

import (
	"errors"
	"fmt"
	"math"
)

var ErrNonFinite = errors.New("geometry contains a non-finite value")

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func (p Point) Add(v Vector) Point {
	return Point{X: p.X + v.X, Y: p.Y + v.Y}
}

func (p Point) Validate() error {
	if !finite(p.X) || !finite(p.Y) {
		return ErrNonFinite
	}
	return nil
}

type Vector struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func (v Vector) Length() float64 {
	return math.Hypot(v.X, v.Y)
}

func (v Vector) ManhattanLength() float64 {
	return math.Abs(v.X) + math.Abs(v.Y)
}

func (v Vector) Validate() error {
	if !finite(v.X) || !finite(v.Y) {
		return ErrNonFinite
	}
	return nil
}

type Size struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

func (s Size) Validate() error {
	if !finite(s.Width) || !finite(s.Height) {
		return ErrNonFinite
	}
	if s.Width <= 0 || s.Height <= 0 {
		return fmt.Errorf("size must be positive: %gx%g", s.Width, s.Height)
	}
	return nil
}

type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

func (r Rect) Validate() error {
	if !finite(r.X) || !finite(r.Y) || !finite(r.Width) || !finite(r.Height) {
		return ErrNonFinite
	}
	if r.Width <= 0 || r.Height <= 0 {
		return fmt.Errorf("rectangle size must be positive: %gx%g", r.Width, r.Height)
	}
	return nil
}

func (r Rect) Right() float64 {
	return r.X + r.Width
}

func (r Rect) Bottom() float64 {
	return r.Y + r.Height
}

// Affine2D stores the first two rows of a homogeneous 3x3 transform:
//
//	x' = A*x + C*y + TX
//	y' = B*x + D*y + TY
type Affine2D struct {
	A  float64 `json:"a"`
	B  float64 `json:"b"`
	C  float64 `json:"c"`
	D  float64 `json:"d"`
	TX float64 `json:"tx"`
	TY float64 `json:"ty"`
}

func IdentityAffine2D() Affine2D {
	return Affine2D{A: 1, D: 1}
}

func (a Affine2D) Apply(p Point) Point {
	return Point{
		X: a.A*p.X + a.C*p.Y + a.TX,
		Y: a.B*p.X + a.D*p.Y + a.TY,
	}
}

func (a Affine2D) Validate() error {
	values := [...]float64{a.A, a.B, a.C, a.D, a.TX, a.TY}
	for _, value := range values {
		if !finite(value) {
			return ErrNonFinite
		}
	}
	if math.Abs(a.A*a.D-a.B*a.C) < 1e-12 {
		return errors.New("affine transform is singular")
	}
	return nil
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
