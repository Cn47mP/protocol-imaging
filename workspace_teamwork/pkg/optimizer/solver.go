package optimizer

import (
	"errors"
	"fmt"
	"math"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
)

// LinearSystem holds the reduced Laplacian L_rr and RHS vectors Bx, By.
type LinearSystem struct {
	Size          int
	L             [][]float64
	Bx            []float64
	By            []float64
	IndexMap      []int // maps reduced index k -> original node index i
	OrigToReduced []int // maps original node index i -> reduced index k (anchor is -1)
	AnchorIdx     int
	AnchorPose    geometry.Point
}

// BuildReducedSystem constructs the decoupled SPD Laplacian system with the anchor node pinned.
func (g *PoseGraph) BuildReducedSystem() (*LinearSystem, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}

	n := len(g.nodeList)
	if n == 1 {
		return &LinearSystem{
			Size:          0,
			IndexMap:      []int{},
			OrigToReduced: []int{-1},
			AnchorIdx:     0,
			AnchorPose:    g.nodeList[0].InitialPose,
		}, nil
	}

	// Determine anchor node
	anchorIdx := -1
	for _, node := range g.nodeList {
		if node.ID == g.anchorNodeID {
			anchorIdx = node.Index
			break
		}
	}
	if anchorIdx == -1 {
		anchorIdx = 0
		g.anchorNodeID = g.nodeList[0].ID
		g.nodeList[0].Fixed = true
	}
	anchorPose := g.nodeList[anchorIdx].InitialPose

	// Map reduced indices
	reducedSize := n - 1
	indexMap := make([]int, reducedSize)
	origToReduced := make([]int, n)
	redIdx := 0
	for i := 0; i < n; i++ {
		if i == anchorIdx {
			origToReduced[i] = -1
		} else {
			origToReduced[i] = redIdx
			indexMap[redIdx] = i
			redIdx++
		}
	}

	// Allocate Laplacian and RHS vectors
	L := make([][]float64, reducedSize)
	for i := range L {
		L[i] = make([]float64, reducedSize)
	}
	bx := make([]float64, reducedSize)
	by := make([]float64, reducedSize)

	// Assemble edge contributions
	for _, e := range g.edges {
		if e.Rejected || e.CurrentWeight <= 0 {
			continue
		}
		w := e.CurrentWeight
		u := e.FromIndex
		v := e.ToIndex
		tx := e.Translation.X
		ty := e.Translation.Y

		uRed := origToReduced[u]
		vRed := origToReduced[v]

		if uRed >= 0 && vRed >= 0 {
			// Both nodes are unpinned
			L[uRed][uRed] += w
			L[vRed][vRed] += w
			L[uRed][vRed] -= w
			L[vRed][uRed] -= w

			bx[uRed] -= w * tx
			bx[vRed] += w * tx
			by[uRed] -= w * ty
			by[vRed] += w * ty
		} else if uRed == -1 && vRed >= 0 {
			// u is anchor, v is unpinned: (pV - pA) ~ t => pV ~ pA + t
			L[vRed][vRed] += w
			bx[vRed] += w*tx + w*anchorPose.X
			by[vRed] += w*ty + w*anchorPose.Y
		} else if uRed >= 0 && vRed == -1 {
			// u is unpinned, v is anchor: (pA - pU) ~ t => pU ~ pA - t
			L[uRed][uRed] += w
			bx[uRed] += -w*tx + w*anchorPose.X
			by[uRed] += -w*ty + w*anchorPose.Y
		}
		// If both u and v are anchor (loop constraint on anchor), no variable affected
	}

	return &LinearSystem{
		Size:          reducedSize,
		L:             L,
		Bx:            bx,
		By:            by,
		IndexMap:      indexMap,
		OrigToReduced: origToReduced,
		AnchorIdx:     anchorIdx,
		AnchorPose:    anchorPose,
	}, nil
}

// SolveCholesky factorizes SPD matrix A = G G^T and solves A x = b via forward/back substitution.
func SolveCholesky(A [][]float64, b []float64) ([]float64, error) {
	n := len(A)
	if n == 0 {
		return []float64{}, nil
	}
	if len(b) != n {
		return nil, errors.New("dimension mismatch in SolveCholesky")
	}

	G := make([][]float64, n)
	for i := range G {
		G[i] = make([]float64, n)
	}

	// Cholesky-Banachiewicz factorization
	for i := 0; i < n; i++ {
		for j := 0; j <= i; j++ {
			sum := A[i][j]
			for k := 0; k < j; k++ {
				sum -= G[i][k] * G[j][k]
			}
			if i == j {
				if sum <= 1e-12 || math.IsNaN(sum) {
					return nil, fmt.Errorf("%w: non-positive pivot at index %d (val=%g)", ErrSingularSystem, i, sum)
				}
				G[i][i] = math.Sqrt(sum)
			} else {
				G[i][j] = sum / G[j][j]
			}
		}
	}

	// Forward substitution: G y = b
	y := make([]float64, n)
	for i := 0; i < n; i++ {
		sum := b[i]
		for k := 0; k < i; k++ {
			sum -= G[i][k] * y[k]
		}
		y[i] = sum / G[i][i]
	}

	// Back substitution: G^T x = y
	x := make([]float64, n)
	for i := n - 1; i >= 0; i-- {
		sum := y[i]
		for k := i + 1; k < n; k++ {
			sum -= G[k][i] * x[k]
		}
		x[i] = sum / G[i][i]
	}

	return x, nil
}

// SolvePCG performs Preconditioned Conjugate Gradient with a Jacobi diagonal preconditioner.
func SolvePCG(A [][]float64, b []float64, x0 []float64, tol float64, maxIter int) ([]float64, bool, error) {
	n := len(A)
	if n == 0 {
		return []float64{}, true, nil
	}
	if len(b) != n {
		return nil, false, errors.New("dimension mismatch in SolvePCG")
	}

	x := make([]float64, n)
	if len(x0) == n {
		copy(x, x0)
	}

	// Initial residual: r = b - A*x
	r := make([]float64, n)
	for i := 0; i < n; i++ {
		var ax float64
		for j := 0; j < n; j++ {
			ax += A[i][j] * x[j]
		}
		r[i] = b[i] - ax
	}

	// Jacobi preconditioner: M_inv[i] = 1 / A[i][i]
	mInv := make([]float64, n)
	for i := 0; i < n; i++ {
		if A[i][i] <= 1e-12 {
			mInv[i] = 1.0
		} else {
			mInv[i] = 1.0 / A[i][i]
		}
	}

	// z = M_inv * r
	z := make([]float64, n)
	for i := 0; i < n; i++ {
		z[i] = mInv[i] * r[i]
	}

	// p = z
	p := make([]float64, n)
	copy(p, z)

	rzOld := dot(r, z)
	bNorm := norm(b)
	if bNorm < 1e-12 {
		bNorm = 1.0
	}

	if math.Sqrt(dot(r, r))/bNorm < tol {
		return x, true, nil
	}

	for iter := 0; iter < maxIter; iter++ {
		// Ap = A * p
		ap := make([]float64, n)
		for i := 0; i < n; i++ {
			var sum float64
			for j := 0; j < n; j++ {
				sum += A[i][j] * p[j]
			}
			ap[i] = sum
		}

		pAp := dot(p, ap)
		if math.Abs(pAp) < 1e-15 {
			break
		}
		alpha := rzOld / pAp

		// x = x + alpha * p
		// r = r - alpha * ap
		for i := 0; i < n; i++ {
			x[i] += alpha * p[i]
			r[i] -= alpha * ap[i]
		}

		rNorm := math.Sqrt(dot(r, r))
		if rNorm/bNorm < tol {
			return x, true, nil
		}

		// z = M_inv * r
		for i := 0; i < n; i++ {
			z[i] = mInv[i] * r[i]
		}

		rzNew := dot(r, z)
		beta := rzNew / rzOld
		rzOld = rzNew

		// p = z + beta * p
		for i := 0; i < n; i++ {
			p[i] = z[i] + beta*p[i]
		}
	}

	return x, false, nil
}

func dot(u, v []float64) float64 {
	var s float64
	for i := range u {
		s += u[i] * v[i]
	}
	return s
}

func norm(u []float64) float64 {
	return math.Sqrt(dot(u, u))
}

// Solve executes the linear solve for both X and Y dimensions using Cholesky or PCG.
func (sys *LinearSystem) Solve(opts SolverOptions, initialX, initialY []float64) (xSolved, ySolved []float64, err error) {
	if sys.Size == 0 {
		return []float64{}, []float64{}, nil
	}

	solverType := opts.SolverType
	if solverType == SolverAuto {
		if sys.Size <= 200 {
			solverType = SolverDirect
		} else {
			solverType = SolverIterative
		}
	}

	if solverType == SolverDirect {
		xSolved, err = SolveCholesky(sys.L, sys.Bx)
		if err != nil {
			// Fallback to PCG if Cholesky encountered ill-conditioning
			xSolved, _, err = SolvePCG(sys.L, sys.Bx, initialX, opts.CGTolerance, opts.MaxCGIterations)
			if err != nil {
				return nil, nil, fmt.Errorf("direct and PCG fallback failed for X: %w", err)
			}
		}
		ySolved, err = SolveCholesky(sys.L, sys.By)
		if err != nil {
			ySolved, _, err = SolvePCG(sys.L, sys.By, initialY, opts.CGTolerance, opts.MaxCGIterations)
			if err != nil {
				return nil, nil, fmt.Errorf("direct and PCG fallback failed for Y: %w", err)
			}
		}
	} else {
		xSolved, _, err = SolvePCG(sys.L, sys.Bx, initialX, opts.CGTolerance, opts.MaxCGIterations)
		if err != nil {
			return nil, nil, fmt.Errorf("PCG failed for X: %w", err)
		}
		ySolved, _, err = SolvePCG(sys.L, sys.By, initialY, opts.CGTolerance, opts.MaxCGIterations)
		if err != nil {
			return nil, nil, fmt.Errorf("PCG failed for Y: %w", err)
		}
	}

	return xSolved, ySolved, nil
}
