package core

import (
	"context"
	"errors"
	"testing"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

// stepFrom builds a one-hop lineage function from a name->neighbors map, so the
// pure graph walk can be tested without a database.
func stepFrom(m map[string][]adapter.ObjectRef) func(context.Context, string) ([]adapter.ObjectRef, error) {
	return func(_ context.Context, name string) ([]adapter.ObjectRef, error) {
		return m[name], nil
	}
}

func ref(schema, name, typ string) adapter.ObjectRef {
	return adapter.ObjectRef{Schema: schema, Name: name, Type: typ}
}

// edgeSet renders a graph's edges as a comparable set of "from->to" strings.
func edgeSet(g LineageGraph) map[string]bool {
	s := map[string]bool{}
	for _, e := range g.Edges {
		s[refKey(e.From)+"->"+refKey(e.To)] = true
	}
	return s
}

func TestBuildLineagePreMultiLevel(t *testing.T) {
	// v depends on a and b; a depends on base.
	steps := stepFrom(map[string][]adapter.ObjectRef{
		"s.v": {ref("s", "a", "table"), ref("s", "b", "view")},
		"s.b": {ref("s", "base", "table")},
	})
	g, err := buildLineage(context.Background(), refFromName("s.v"), LineagePre, 5, 100, steps)
	if err != nil {
		t.Fatalf("buildLineage: %v", err)
	}
	// Edges are normalized to data-flow direction (source -> consumer).
	want := map[string]bool{
		"s.a->s.v":    true,
		"s.b->s.v":    true,
		"s.base->s.b": true,
	}
	got := edgeSet(g)
	if len(got) != len(want) {
		t.Fatalf("edges = %v, want %v", got, want)
	}
	for e := range want {
		if !got[e] {
			t.Errorf("missing edge %s (got %v)", e, got)
		}
	}
	if g.Truncated {
		t.Error("graph should not be truncated")
	}
	if g.Direction != "pre" {
		t.Errorf("direction = %q", g.Direction)
	}
}

func TestBuildLineagePostDirection(t *testing.T) {
	// t is consumed by v1 and v2.
	steps := stepFrom(map[string][]adapter.ObjectRef{
		"s.t": {ref("s", "v1", "view"), ref("s", "v2", "view")},
	})
	g, err := buildLineage(context.Background(), refFromName("s.t"), LineagePost, 5, 100, steps)
	if err != nil {
		t.Fatalf("buildLineage: %v", err)
	}
	// Post walk: edges still source -> consumer, i.e. t -> v1, t -> v2.
	got := edgeSet(g)
	if !got["s.t->s.v1"] || !got["s.t->s.v2"] {
		t.Errorf("post edges = %v", got)
	}
}

func TestBuildLineageCycleTerminates(t *testing.T) {
	// a -> b -> a is a cycle; the walk must terminate and dedupe edges.
	steps := stepFrom(map[string][]adapter.ObjectRef{
		"s.a": {ref("s", "b", "view")},
		"s.b": {ref("s", "a", "view")},
	})
	g, err := buildLineage(context.Background(), refFromName("s.a"), LineagePre, 10, 100, steps)
	if err != nil {
		t.Fatalf("buildLineage: %v", err)
	}
	if len(g.Edges) != 2 {
		t.Errorf("cycle should yield 2 deduped edges, got %d: %v", len(g.Edges), edgeSet(g))
	}
}

func TestBuildLineageDepthBound(t *testing.T) {
	steps := stepFrom(map[string][]adapter.ObjectRef{
		"s.a": {ref("s", "b", "view")},
		"s.b": {ref("s", "c", "view")},
		"s.c": {ref("s", "d", "table")},
	})
	g, err := buildLineage(context.Background(), refFromName("s.a"), LineagePre, 1, 100, steps)
	if err != nil {
		t.Fatalf("buildLineage: %v", err)
	}
	// Depth 1: only a's direct input b is discovered; b is not expanded.
	got := edgeSet(g)
	if !got["s.b->s.a"] || len(got) != 1 {
		t.Errorf("depth-1 edges = %v, want only s.b->s.a", got)
	}
	if !g.Truncated {
		t.Error("hitting the depth bound should set Truncated")
	}
}

func TestBuildLineageNodeCap(t *testing.T) {
	steps := stepFrom(map[string][]adapter.ObjectRef{
		"s.a": {ref("s", "b", "table"), ref("s", "c", "table"), ref("s", "d", "table")},
	})
	// maxNodes=2: root + one neighbor fit, then the cap trips.
	g, err := buildLineage(context.Background(), refFromName("s.a"), LineagePre, 5, 2, steps)
	if err != nil {
		t.Fatalf("buildLineage: %v", err)
	}
	if !g.Truncated {
		t.Error("exceeding the node cap should set Truncated")
	}
}

func TestBuildLineagePropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	step := func(context.Context, string) ([]adapter.ObjectRef, error) { return nil, sentinel }
	if _, err := buildLineage(context.Background(), refFromName("x"), LineagePre, 5, 100, step); !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want sentinel", err)
	}
}

func TestLineageChildren(t *testing.T) {
	g := LineageGraph{
		Root:      ref("s", "v", ""),
		Direction: "pre",
		Edges: []LineageEdge{
			{From: ref("s", "a", "table"), To: ref("s", "v", "")},
			{From: ref("s", "b", "table"), To: ref("s", "v", "")},
		},
	}
	kids := g.Children(ref("s", "v", ""))
	if len(kids) != 2 {
		t.Fatalf("pre children = %v, want 2", kids)
	}
	// Post orientation: children follow From==ref.
	g.Direction = "post"
	g.Edges = []LineageEdge{{From: ref("s", "v", ""), To: ref("s", "c", "view")}}
	kids = g.Children(ref("s", "v", ""))
	if len(kids) != 1 || kids[0].Name != "c" {
		t.Errorf("post children = %v, want [c]", kids)
	}
}

func TestLineageDirString(t *testing.T) {
	if LineagePre.String() != "pre" || LineagePost.String() != "post" {
		t.Errorf("dir strings: %q %q", LineagePre.String(), LineagePost.String())
	}
}

func TestRefFromName(t *testing.T) {
	if r := refFromName("sales.orders"); r.Schema != "sales" || r.Name != "orders" {
		t.Errorf("qualified = %+v", r)
	}
	if r := refFromName("orders"); r.Schema != "" || r.Name != "orders" {
		t.Errorf("bare = %+v", r)
	}
}

func TestLineageRequiresConnection(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := c.Lineage(context.Background(), "t", LineagePre, 0); !errors.Is(err, ErrNotConnected) {
		t.Errorf("Lineage without connection = %v, want ErrNotConnected", err)
	}
}
