package core

import (
	"context"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

// LineageDir selects which way a lineage walk expands from its root.
type LineageDir int

const (
	// LineagePre walks upstream: the objects the root depends on (its inputs),
	// then their inputs, and so on.
	LineagePre LineageDir = iota
	// LineagePost walks downstream: the objects that depend on the root (its
	// consumers), then their consumers, and so on.
	LineagePost
)

func (d LineageDir) String() string {
	if d == LineagePost {
		return "post"
	}
	return "pre"
}

// Default bounds for a lineage walk. A graph can be large or cyclic (a view
// selecting from a view), so the walk is always bounded; the front-end reports
// LineageGraph.Truncated when a bound was hit.
const (
	defaultLineageDepth = 5
	maxLineageNodes     = 500
)

// LineageEdge is one dependency edge, normalized to data-flow direction: data
// flows From a source object To a consumer object (e.g. a table -> a view that
// selects from it), regardless of which way the walk traversed it.
type LineageEdge struct {
	From adapter.ObjectRef `json:"from"`
	To   adapter.ObjectRef `json:"to"`
}

// LineageGraph is the assembled dependency graph for a root object: the edges
// discovered by a bounded walk in one direction. The edge set is built here in
// the core so every front-end renders the same graph. Truncated is true when a
// depth or size bound stopped the walk before it ran dry.
type LineageGraph struct {
	Root      adapter.ObjectRef `json:"root"`
	Direction string            `json:"direction"`
	Edges     []LineageEdge     `json:"edges"`
	Truncated bool              `json:"truncated"`
}

// Lineage assembles the dependency graph for the named object by walking the
// adapter's one-hop GetPreLineage/GetPostLineage repeatedly, breadth-first, up
// to maxDepth hops (<= 0 uses the default). It is read-only, so no safety guard
// applies. Requires the connected engine to advertise CapLineage; without it
// the walk's first hop returns adapter.ErrUnsupported.
func (c *Core) Lineage(ctx context.Context, name string, dir LineageDir, maxDepth int) (LineageGraph, error) {
	if c.conn == nil {
		return LineageGraph{}, ErrNotConnected
	}
	if maxDepth <= 0 {
		maxDepth = defaultLineageDepth
	}
	step := c.conn.GetPreLineage
	if dir == LineagePost {
		step = c.conn.GetPostLineage
	}
	root := refFromName(name)
	return buildLineage(ctx, root, dir, maxDepth, maxLineageNodes, step)
}

// buildLineage is the pure graph-assembly used by Lineage, factored out so it
// can be tested with a canned one-hop step function and no database. It walks
// breadth-first from root, deduplicating both nodes (cycle-safe) and edges, and
// stops expanding at maxDepth hops or once maxNodes distinct nodes are seen.
func buildLineage(ctx context.Context, root adapter.ObjectRef, dir LineageDir, maxDepth, maxNodes int,
	step func(context.Context, string) ([]adapter.ObjectRef, error)) (LineageGraph, error) {

	g := LineageGraph{Root: root, Direction: dir.String()}
	type item struct {
		ref   adapter.ObjectRef
		depth int
	}
	visited := map[string]bool{refKey(root): true}
	edgeSeen := map[string]bool{}
	queue := []item{{root, 0}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= maxDepth {
			g.Truncated = true
			continue
		}
		neighbors, err := step(ctx, qualify(cur.ref))
		if err != nil {
			return g, err
		}
		for _, n := range neighbors {
			// Normalize to data-flow direction so a Pre walk and a Post walk over
			// the same objects produce identical edges.
			e := LineageEdge{From: n, To: cur.ref}
			if dir == LineagePost {
				e = LineageEdge{From: cur.ref, To: n}
			}
			ek := refKey(e.From) + "\x00" + refKey(e.To)
			if !edgeSeen[ek] {
				edgeSeen[ek] = true
				g.Edges = append(g.Edges, e)
			}
			nk := refKey(n)
			if visited[nk] {
				continue
			}
			if len(visited) >= maxNodes {
				g.Truncated = true
				continue
			}
			visited[nk] = true
			queue = append(queue, item{n, cur.depth + 1})
		}
	}
	return g, nil
}

// Children returns the objects reached from ref by following the graph's edges
// one hop in the walk's expansion direction — a view's inputs for a Pre graph,
// its consumers for a Post graph. It lets a front-end render the edge set as a
// tree rooted at Root without re-deriving the direction convention.
func (g LineageGraph) Children(ref adapter.ObjectRef) []adapter.ObjectRef {
	var out []adapter.ObjectRef
	for _, e := range g.Edges {
		switch g.Direction {
		case "post":
			if refKey(e.From) == refKey(ref) {
				out = append(out, e.To)
			}
		default: // "pre"
			if refKey(e.To) == refKey(ref) {
				out = append(out, e.From)
			}
		}
	}
	return out
}

// refKey identifies an object for dedup/cycle detection. Schema+name is the
// identity; two references to the same object carry the same schema.name.
func refKey(r adapter.ObjectRef) string {
	if r.Schema == "" {
		return r.Name
	}
	return r.Schema + "." + r.Name
}

// qualify renders an ObjectRef as the schema-qualified name adapters expect.
func qualify(r adapter.ObjectRef) string { return refKey(r) }

// refFromName parses a (possibly schema-qualified) name into a root ObjectRef.
// The object's kind is unknown until it appears as a neighbor in the walk.
func refFromName(name string) adapter.ObjectRef {
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			return adapter.ObjectRef{Schema: name[:i], Name: name[i+1:]}
		}
	}
	return adapter.ObjectRef{Name: name}
}
