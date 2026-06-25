package adapter

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ErrUnsupported is returned by adapter methods a given database cannot support
// (e.g. lineage on a driver that has no catalog for it).
var ErrUnsupported = errors.New("operation not supported by this adapter")

// Factory builds a fresh, unconnected adapter.
type Factory func() Adapter

var (
	mu       sync.RWMutex
	registry = map[string]Factory{}
)

// Register associates a server type (e.g. "postgres") with its adapter factory.
// Adapters call this from an init function in their own package, so that adding a
// driver is an import, and CGo drivers can register only under their build tag.
// Registering the same type twice panics — it indicates a build wiring bug.
func Register(typ string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[typ]; dup {
		panic(fmt.Sprintf("adapter: type %q registered twice", typ))
	}
	registry[typ] = f
}

// New returns a fresh adapter for the given server type.
func New(typ string) (Adapter, error) {
	mu.RLock()
	f, ok := registry[typ]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no adapter registered for database type %q", typ)
	}
	return f(), nil
}

// Registered reports whether a type has an adapter.
func Registered(typ string) bool {
	mu.RLock()
	_, ok := registry[typ]
	mu.RUnlock()
	return ok
}

// Types returns all registered server types, sorted.
func Types() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for t := range registry {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
