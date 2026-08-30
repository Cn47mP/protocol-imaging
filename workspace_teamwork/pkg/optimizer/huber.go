package optimizer

import (
	"math"
	"sort"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
)

// HuberWeight computes the robust re-weighting multiplier: min(1.0, delta / r).
func HuberWeight(residual, delta float64) float64 {
	if residual <= delta || delta <= 0 || math.IsNaN(residual) {
		return 1.0
	}
	return delta / residual
}

// ComputeResiduals calculates Euclidean edge residuals for all edges in the graph.
func (g *PoseGraph) ComputeResiduals() []float64 {
	residuals := make([]float64, len(g.edges))
	for i, e := range g.edges {
		u := g.nodeList[e.FromIndex]
		v := g.nodeList[e.ToIndex]

		diffX := v.OptimizedPose.X - u.OptimizedPose.X
		diffY := v.OptimizedPose.Y - u.OptimizedPose.Y

		errX := diffX - e.Translation.X
		errY := diffY - e.Translation.Y

		res := math.Hypot(errX, errY)
		e.Residual = res
		residuals[i] = res
	}
	return residuals
}

// ComputeResidualStats aggregates summary error statistics over non-rejected edges.
func ComputeResidualStats(residuals []float64, rejectedCount, totalEdges, iters int, converged bool) ResidualStats {
	if len(residuals) == 0 {
		return ResidualStats{
			TotalEdges:    totalEdges,
			ActiveEdges:   0,
			RejectedEdges: rejectedCount,
			Iterations:    iters,
			Converged:     converged,
		}
	}

	sorted := make([]float64, len(residuals))
	copy(sorted, residuals)
	sort.Float64s(sorted)

	n := len(sorted)
	var sum, sqSum float64
	for _, r := range sorted {
		sum += r
		sqSum += r * r
	}

	mean := sum / float64(n)
	rmse := math.Sqrt(sqSum / float64(n))
	maxVal := sorted[n-1]

	// Median (50th percentile)
	var median float64
	if n%2 == 1 {
		median = sorted[n/2]
	} else {
		median = 0.5 * (sorted[n/2-1] + sorted[n/2])
	}

	// 95th percentile
	var p95 float64
	if n == 1 {
		p95 = sorted[0]
	} else {
		idx := 0.95 * float64(n-1)
		i0 := int(math.Floor(idx))
		i1 := int(math.Ceil(idx))
		frac := idx - float64(i0)
		if i1 >= n {
			i1 = n - 1
		}
		p95 = (1.0-frac)*sorted[i0] + frac*sorted[i1]
	}

	return ResidualStats{
		Residuals:      sorted,
		MedianResidual: median,
		P95Residual:    p95,
		MaxResidual:    maxVal,
		MeanResidual:   mean,
		RMSE:           rmse,
		TotalEdges:     totalEdges,
		ActiveEdges:    n,
		RejectedEdges:  rejectedCount,
		Iterations:     iters,
		Converged:      converged,
	}
}

// SolveIRLS executes Iteratively Reweighted Least Squares with Huber robust loss and outlier rejection.
func (g *PoseGraph) SolveIRLS(opts SolverOptions) (map[string]geometry.Point, ResidualStats, error) {
	if err := g.Validate(); err != nil {
		return nil, ResidualStats{}, err
	}

	if len(g.nodeList) == 1 {
		resMap := map[string]geometry.Point{
			g.nodeList[0].ID: g.nodeList[0].InitialPose,
		}
		return resMap, ResidualStats{
			TotalEdges:  0,
			ActiveEdges: 0,
			Converged:   true,
		}, nil
	}

	// Reset node poses to initial
	for _, node := range g.nodeList {
		node.OptimizedPose = node.InitialPose
	}
	for _, edge := range g.edges {
		edge.CurrentWeight = edge.Weight
		edge.Rejected = false
		edge.Residual = 0
	}

	// Initial residual calculation
	g.ComputeResiduals()

	converged := false
	iterations := 0

	for iter := 0; iter < opts.MaxIRLSIterations; iter++ {
		iterations++

		// 1. Update weights with Huber loss
		for _, edge := range g.edges {
			if edge.Rejected {
				continue
			}
			hw := HuberWeight(edge.Residual, opts.HuberDelta)
			edge.CurrentWeight = edge.Weight * hw
		}

		// 2. Build reduced system
		sys, err := g.BuildReducedSystem()
		if err != nil {
			return nil, ResidualStats{}, err
		}

		// Prepare warm start initial guesses
		initialX := make([]float64, sys.Size)
		initialY := make([]float64, sys.Size)
		for redIdx, origIdx := range sys.IndexMap {
			initialX[redIdx] = g.nodeList[origIdx].OptimizedPose.X
			initialY[redIdx] = g.nodeList[origIdx].OptimizedPose.Y
		}

		// 3. Solve for X and Y
		xSolved, ySolved, err := sys.Solve(opts, initialX, initialY)
		if err != nil {
			return nil, ResidualStats{}, err
		}

		// 4. Update poses and track max shift
		maxShift := 0.0
		for redIdx, origIdx := range sys.IndexMap {
			node := g.nodeList[origIdx]
			newX := xSolved[redIdx]
			newY := ySolved[redIdx]
			shift := math.Hypot(newX-node.OptimizedPose.X, newY-node.OptimizedPose.Y)
			if shift > maxShift {
				maxShift = shift
			}
			node.OptimizedPose = geometry.Point{X: newX, Y: newY}
		}

		// Ensure anchor node remains at anchor pose
		anchorNode := g.nodeList[sys.AnchorIdx]
		anchorNode.OptimizedPose = sys.AnchorPose

		// 5. Update residuals
		g.ComputeResiduals()

		// 6. Outlier rejection (if enabled)
		if opts.EnableOutlierRejection && iter >= 1 {
			for _, edge := range g.edges {
				if !edge.Rejected && edge.Residual > opts.OutlierThreshold {
					edge.Rejected = true
					// Validate graph connectivity after provisional rejection
					if err := g.Validate(); err != nil {
						// Revert rejection if it disconnected the graph
						edge.Rejected = false
					}
				}
			}
		}

		// 7. Check convergence
		if maxShift < opts.IRLSTolerance {
			converged = true
			break
		}
	}

	// Final active residuals and stats
	var activeResiduals []float64
	rejectedCount := 0
	for _, edge := range g.edges {
		if edge.Rejected {
			rejectedCount++
		} else {
			activeResiduals = append(activeResiduals, edge.Residual)
		}
	}

	stats := ComputeResidualStats(activeResiduals, rejectedCount, len(g.edges), iterations, converged)

	resMap := make(map[string]geometry.Point, len(g.nodeList))
	for _, node := range g.nodeList {
		resMap[node.ID] = node.OptimizedPose
	}

	return resMap, stats, nil
}
