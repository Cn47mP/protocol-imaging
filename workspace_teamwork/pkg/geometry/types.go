package geometry

import (
	"errors"
	"fmt"
	"math"
)

var (
	ErrNonFinite      = errors.New("geometry contains a non-finite value")
	ErrSingularAffine = errors.New("affine transform is singular")
)

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func (p Point) Add(v Vector) Point {
	return Point{X: p.X + v.X, Y: p.Y + v.Y}
}

func (p Point) Sub(other Point) Vector {
	return Vector{X: p.X - other.X, Y: p.Y - other.Y}
}

func (p Point) SubVector(v Vector) Point {
	return Point{X: p.X - v.X, Y: p.Y - v.Y}
}

func (p Point) DistanceTo(other Point) float64 {
	return math.Hypot(p.X-other.X, p.Y-other.Y)
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

func (v Vector) Add(other Vector) Vector {
	return Vector{X: v.X + other.X, Y: v.Y + other.Y}
}

func (v Vector) Sub(other Vector) Vector {
	return Vector{X: v.X - other.X, Y: v.Y - other.Y}
}

func (v Vector) Scale(factor float64) Vector {
	return Vector{X: v.X * factor, Y: v.Y * factor}
}

func (v Vector) Dot(other Vector) float64 {
	return v.X*other.X + v.Y*other.Y
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

func (s Size) Area() float64 {
	return s.Width * s.Height
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

func (r Rect) Right() float64 {
	return r.X + r.Width
}

func (r Rect) Bottom() float64 {
	return r.Y + r.Height
}

func (r Rect) TopLeft() Point {
	return Point{X: r.X, Y: r.Y}
}

func (r Rect) BottomRight() Point {
	return Point{X: r.Right(), Y: r.Bottom()}
}

func (r Rect) Contains(p Point) bool {
	return p.X >= r.X && p.X <= r.Right() && p.Y >= r.Y && p.Y <= r.Bottom()
}

func (r Rect) Intersects(other Rect) bool {
	return r.X < other.Right() && r.Right() > other.X && r.Y < other.Bottom() && r.Bottom() > other.Y
}

func (r Rect) Intersection(other Rect) (Rect, bool) {
	minX := math.Max(r.X, other.X)
	maxX := math.Min(r.Right(), other.Right())
	minY := math.Max(r.Y, other.Y)
	maxY := math.Min(r.Bottom(), other.Bottom())
	if minX < maxX && minY < maxY {
		return Rect{
			X:      minX,
			Y:      minY,
			Width:  maxX - minX,
			Height: maxY - minY,
		}, true
	}
	return Rect{}, false
}

func (r Rect) Union(other Rect) Rect {
	minX := math.Min(r.X, other.X)
	maxX := math.Max(r.Right(), other.Right())
	minY := math.Min(r.Y, other.Y)
	maxY := math.Max(r.Bottom(), other.Bottom())
	return Rect{
		X:      minX,
		Y:      minY,
		Width:  maxX - minX,
		Height: maxY - minY,
	}
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

func Translation(tx, ty float64) Affine2D {
	return Affine2D{A: 1, D: 1, TX: tx, TY: ty}
}

func ScaleTransform(sx, sy float64) Affine2D {
	return Affine2D{A: sx, D: sy}
}

func (a Affine2D) Apply(p Point) Point {
	return Point{
		X: a.A*p.X + a.C*p.Y + a.TX,
		Y: a.B*p.X + a.D*p.Y + a.TY,
	}
}

func (a Affine2D) ApplyVector(v Vector) Vector {
	return Vector{
		X: a.A*v.X + a.C*v.Y,
		Y: a.B*v.X + a.D*v.Y,
	}
}

func (a Affine2D) Determinant() float64 {
	return a.A*a.D - a.B*a.C
}

// Multiply computes a * other (applying other first, then a).
//
//	[a.A a.C a.TX]   [other.A other.C other.TX]
//	[a.B a.D a.TY] * [other.B other.D other.TY]
//	[ 0   0    1 ]   [   0       0       1    ]
func (a Affine2D) Multiply(other Affine2D) Affine2D {
	return Affine2D{
		A:  a.A*other.A + a.C*other.B,
		B:  a.B*other.A + a.D*other.B,
		C:  a.A*other.C + a.C*other.D,
		D:  a.B*other.C + a.D*other.D,
		TX: a.A*other.TX + a.C*other.TY + a.TX,
		TY: a.B*other.TX + a.D*other.TY + a.TY,
	}
}

func (a Affine2D) Inverse() (Affine2D, error) {
	values := [...]float64{a.A, a.B, a.C, a.D, a.TX, a.TY}
	for _, v := range values {
		if !finite(v) {
			return Affine2D{}, ErrNonFinite
		}
	}
	det := a.Determinant()
	if !finite(det) || math.Abs(det) < 1e-12 {
		return Affine2D{}, ErrSingularAffine
	}
	invDet := 1.0 / det
	return Affine2D{
		A:  a.D * invDet,
		B:  -a.B * invDet,
		C:  -a.C * invDet,
		D:  a.A * invDet,
		TX: (a.C*a.TY - a.D*a.TX) * invDet,
		TY: (a.B*a.TX - a.A*a.TY) * invDet,
	}, nil
}

func (a Affine2D) Validate() error {
	values := [...]float64{a.A, a.B, a.C, a.D, a.TX, a.TY}
	for _, value := range values {
		if !finite(value) {
			return ErrNonFinite
		}
	}
	det := a.Determinant()
	if !finite(det) || math.Abs(det) < 1e-12 {
		return ErrSingularAffine
	}
	return nil
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
