package optimizer

import (
	"errors"
	"fmt"
	"math"

	"github.com/Cn47mP/protocol-imaging/workspace_teamwork/pkg/geometry"
)

var (
	ErrEmptyGraph        = errors.New("pose graph is empty")
	ErrNodeNotFound      = errors.New("node not found in graph")
	ErrDuplicateNode     = errors.New("node already exists in graph")
	ErrDisconnectedGraph = errors.New("pose graph contains unreachable nodes from anchor")
	ErrNonFiniteValue    = errors.New("pose graph contains non-finite coordinate or weight")
	ErrInvalidWeight     = errors.New("edge weight must be positive")
	ErrSingularSystem    = errors.New("linear system is singular or not positive definite")
)

type EdgeType string

const (
	EdgeTypeNominal     EdgeType = "nominal"
	EdgeTypeMeasured    EdgeType = "measured"
	EdgeTypeLoopClosure EdgeType = "loop_closure"
)

// Node represents a vertex in the 2D translation pose graph.
type Node struct {
	ID            string         `json:"id"`
	Index         int            `json:"index"`
	InitialPose   geometry.Point `json:"initial_pose"`
	OptimizedPose geometry.Point `json:"optimized_pose"`
	Fixed         bool           `json:"fixed"`
}

// Edge represents a directed relative 2D translation constraint from FromNode to ToNode.
type Edge struct {
	ID            string          `json:"id"`
	FromNode      string          `json:"from_node"`
	ToNode        string          `json:"to_node"`
	FromIndex     int             `json:"from_index"`
	ToIndex       int             `json:"to_index"`
	Translation   geometry.Vector `json:"translation"`
	Weight        float64         `json:"weight"`
	CurrentWeight float64         `json:"current_weight"`
	Type          EdgeType        `json:"type"`
	Residual      float64         `json:"residual"`
	Rejected      bool            `json:"rejected"`
	Confidence    float64         `json:"confidence"`
}

type SolverType string

const (
	SolverAuto      SolverType = "auto"
	SolverDirect    SolverType = "direct"    // Cholesky LL^T decomposition
	SolverIterative SolverType = "iterative" // Preconditioned Conjugate Gradient (PCG)
)

// SolverOptions defines parameters for pose graph optimization and robust IRLS.
type SolverOptions struct {
	HuberDelta             float64
	MaxIRLSIterations      int
	IRLSTolerance          float64
	OutlierThreshold       float64
	EnableOutlierRejection bool
	MaxCGIterations        int
	CGTolerance            float64
	SolverType             SolverType
}

// DefaultSolverOptions returns standard solver parameters.
func DefaultSolverOptions() SolverOptions {
	return SolverOptions{
		HuberDelta:             3.0,
		MaxIRLSIterations:      15,
		IRLSTolerance:          1e-4,
		OutlierThreshold:       12.0,
		EnableOutlierRejection: true,
		MaxCGIterations:        300,
		CGTolerance:            1e-7,
		SolverType:             SolverAuto,
	}
}

// ResidualStats aggregates error metrics after optimization.
type ResidualStats struct {
	Residuals      []float64 `json:"residuals"`
	MedianResidual float64   `json:"median_residual"`
	P95Residual    float64   `json:"p95_residual"`
	MaxResidual    float64   `json:"max_residual"`
	MeanResidual   float64   `json:"mean_residual"`
	RMSE           float64   `json:"rmse"`
	TotalEdges     int       `json:"total_edges"`
	ActiveEdges    int       `json:"active_edges"`
	RejectedEdges  int       `json:"rejected_edges"`
	Iterations     int       `json:"iterations"`
	Converged      bool      `json:"converged"`
}

// PoseGraphSolver is the interface contract specified in PROJECT.md.
type PoseGraphSolver interface {
	AddNode(id string, initialPose geometry.Point)
	AddEdge(idA, idB string, relativeTranslation geometry.Vector, weight float64)
	Solve() (map[string]geometry.Point, ResidualStats, error)
}

// PoseGraph holds the vertices and constraints for global 2D translation optimization.
type PoseGraph struct {
	nodes        map[string]*Node
	nodeList     []*Node
	edges        []*Edge
	anchorNodeID string
}

// NewPoseGraph initializes an empty PoseGraph.
func NewPoseGraph() *PoseGraph {
	return &PoseGraph{
		nodes:    make(map[string]*Node),
		nodeList: make([]*Node, 0),
		edges:    make([]*Edge, 0),
	}
}

// AddNode adds or updates a node in the pose graph.
func (g *PoseGraph) AddNode(id string, initialPose geometry.Point) {
	if existing, found := g.nodes[id]; found {
		existing.InitialPose = initialPose
		existing.OptimizedPose = initialPose
		return
	}
	idx := len(g.nodeList)
	node := &Node{
		ID:            id,
		Index:         idx,
		InitialPose:   initialPose,
		OptimizedPose: initialPose,
		Fixed:         false,
	}
	if len(g.nodeList) == 0 {
		g.anchorNodeID = id
		node.Fixed = true
	}
	g.nodes[id] = node
	g.nodeList = append(g.nodeList, node)
}

// SetAnchor explicitly sets which node acts as the fixed translation gauge anchor.
func (g *PoseGraph) SetAnchor(id string) error {
	node, exists := g.nodes[id]
	if !exists {
		return fmt.Errorf("%w: anchor node %q", ErrNodeNotFound, id)
	}
	for _, n := range g.nodeList {
		n.Fixed = false
	}
	g.anchorNodeID = id
	node.Fixed = true
	return nil
}

// AddEdge adds a relative translation constraint between idA and idB: (pB - pA) ~ relativeTranslation.
func (g *PoseGraph) AddEdge(idA, idB string, relativeTranslation geometry.Vector, weight float64) {
	if weight <= 0 {
		weight = 1.0
	}
	u, uExists := g.nodes[idA]
	v, vExists := g.nodes[idB]
	if !uExists {
		g.AddNode(idA, geometry.Point{})
		u = g.nodes[idA]
	}
	if !vExists {
		g.AddNode(idB, geometry.Point{})
		v = g.nodes[idB]
	}

	edgeID := fmt.Sprintf("%s->%s", idA, idB)
	edge := &Edge{
		ID:            edgeID,
		FromNode:      idA,
		ToNode:        idB,
		FromIndex:     u.Index,
		ToIndex:       v.Index,
		Translation:   relativeTranslation,
		Weight:        weight,
		CurrentWeight: weight,
		Type:          EdgeTypeMeasured,
		Residual:      0,
		Rejected:      false,
		Confidence:    1.0,
	}
	g.edges = append(g.edges, edge)
}

// AddEdgeDetailed adds an edge with full metadata.
func (g *PoseGraph) AddEdgeDetailed(edge Edge) error {
	if edge.Weight <= 0 {
		return ErrInvalidWeight
	}
	u, uExists := g.nodes[edge.FromNode]
	if !uExists {
		return fmt.Errorf("%w: from node %q", ErrNodeNotFound, edge.FromNode)
	}
	v, vExists := g.nodes[edge.ToNode]
	if !vExists {
		return fmt.Errorf("%w: to node %q", ErrNodeNotFound, edge.ToNode)
	}
	edge.FromIndex = u.Index
	edge.ToIndex = v.Index
	if edge.CurrentWeight <= 0 {
		edge.CurrentWeight = edge.Weight
	}
	dup := edge
	g.edges = append(g.edges, &dup)
	return nil
}

func (g *PoseGraph) GetNode(id string) (*Node, bool) {
	n, exists := g.nodes[id]
	return n, exists
}

func (g *PoseGraph) GetNodes() []*Node {
	return g.nodeList
}

func (g *PoseGraph) GetEdges() []*Edge {
	return g.edges
}

func (g *PoseGraph) NodeCount() int {
	return len(g.nodeList)
}

func (g *PoseGraph) EdgeCount() int {
	return len(g.edges)
}

// Validate checks that the graph is non-empty, contains finite coordinates and weights,
// and that all nodes are connected to the anchor.
func (g *PoseGraph) Validate() error {
	if len(g.nodeList) == 0 {
		return ErrEmptyGraph
	}
	for _, n := range g.nodeList {
		if err := n.InitialPose.Validate(); err != nil {
			return fmt.Errorf("%w: node %q initial pose", ErrNonFiniteValue, n.ID)
		}
	}
	for _, e := range g.edges {
		if err := e.Translation.Validate(); err != nil {
			return fmt.Errorf("%w: edge %q translation", ErrNonFiniteValue, e.ID)
		}
		if math.IsNaN(e.Weight) || math.IsInf(e.Weight, 0) || e.Weight <= 0 {
			return fmt.Errorf("%w: edge %q weight %g", ErrInvalidWeight, e.ID, e.Weight)
		}
	}

	if len(g.nodeList) == 1 {
		return nil
	}

	// Find anchor index
	anchorIdx := -1
	for _, n := range g.nodeList {
		if n.ID == g.anchorNodeID {
			anchorIdx = n.Index
			break
		}
	}
	if anchorIdx == -1 {
		anchorIdx = 0
		g.anchorNodeID = g.nodeList[0].ID
		g.nodeList[0].Fixed = true
	}

	// BFS connectivity check over active edges
	adj := make([][]int, len(g.nodeList))
	for _, e := range g.edges {
		if !e.Rejected {
			adj[e.FromIndex] = append(adj[e.FromIndex], e.ToIndex)
			adj[e.ToIndex] = append(adj[e.ToIndex], e.FromIndex)
		}
	}

	visited := make([]bool, len(g.nodeList))
	queue := []int{anchorIdx}
	visited[anchorIdx] = true
	visitedCount := 1

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		for _, neighbor := range adj[curr] {
			if !visited[neighbor] {
				visited[neighbor] = true
				visitedCount++
				queue = append(queue, neighbor)
			}
		}
	}

	if visitedCount < len(g.nodeList) {
		return ErrDisconnectedGraph
	}

	return nil
}

// Solve executes pose graph optimization with default options.
func (g *PoseGraph) Solve() (map[string]geometry.Point, ResidualStats, error) {
	return g.SolveWithOptions(DefaultSolverOptions())
}

// SolveWithOptions executes pose graph optimization with specified options.
func (g *PoseGraph) SolveWithOptions(opts SolverOptions) (map[string]geometry.Point, ResidualStats, error) {
	return g.SolveIRLS(opts)
}
