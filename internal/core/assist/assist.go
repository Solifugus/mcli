// Package assist is the UI-agnostic guidance channel (design §26). An AI acting
// through the MCP front-end can guide the user in whichever surface they are
// looking at — highlight a control, pre-fill a field, or walk through a task —
// by publishing Events to a Bus. Each front-end (TUI, later GUI) subscribes and
// renders the same vocabulary its own way, keyed by semantic target ids.
//
// The core owns one Bus so both the MCP tools (publishers) and the active
// front-end (subscriber) share it. When no front-end is subscribed there is no
// "live session": Publish reports that nothing was delivered, and the ui_* tools
// surface a helpful message instead of silently dropping guidance.
package assist

import "sync"

// Kind is the guidance primitive. The set is intentionally small; richer
// walkthroughs are expressed as a Demo of Steps rather than new primitives.
type Kind string

const (
	KindHighlight Kind = "highlight" // draw attention to an element (pulse/blink)
	KindFocus     Kind = "focus"     // move focus to an element
	KindPrefill   Kind = "prefill"   // put text into an input (never submit it)
	KindAnnotate  Kind = "annotate"  // attach an explanatory callout to an element
	KindDemo      Kind = "demo"      // an ordered, narrated walkthrough
)

// Well-known semantic target ids. These form the stable contract between the AI
// and every front-end; each front-end maps them to its own renderable elements.
// Targets are never pixel coordinates or widget pointers.
const (
	TargetInputLine = "input-line" // the REPL input / a primary text entry
	TargetEditor    = "editor"     // the SQL editor surface
	TargetGrid      = "grid"       // the result grid
	TargetResults   = "results"    // the inline result region
)

// Step is one stage of a Demo.
type Step struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Target      string `json:"target,omitempty"` // element this step concerns
	Action      string `json:"action,omitempty"` // suggested action, e.g. text to type
}

// Event is a single guidance directive. One flat struct (rather than an
// interface) keeps it trivially serializable across the eventual live transport.
type Event struct {
	Kind   Kind   `json:"kind"`
	Target string `json:"target,omitempty"`
	Text   string `json:"text,omitempty"`  // callout text / prefill content
	Steps  []Step `json:"steps,omitempty"` // for KindDemo
}

// subBuffer bounds how many events a slow subscriber may queue before the oldest
// are dropped. Guidance is advisory, so dropping under backpressure is
// acceptable and keeps Publish non-blocking.
const subBuffer = 32

// Bus is a fan-out event channel. It is safe for concurrent use: MCP tool
// handlers Publish from their own goroutines while a front-end drains its
// subscription on another.
type Bus struct {
	mu   sync.RWMutex
	subs map[int]chan Event
	next int
}

// NewBus returns an empty Bus with no subscribers.
func NewBus() *Bus { return &Bus{subs: map[int]chan Event{}} }

// Subscribe registers a receiver and returns its channel plus an unsubscribe
// func. The channel is buffered; callers should drain it promptly. Calling the
// returned func closes the channel and removes the subscription.
func (b *Bus) Subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	ch := make(chan Event, subBuffer)
	b.subs[id] = ch
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if c, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(c)
		}
	}
}

// HasSubscribers reports whether a front-end is currently attached — i.e.
// whether there is a live session that can render guidance.
func (b *Bus) HasSubscribers() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs) > 0
}

// Publish fans e out to every subscriber and reports whether at least one
// subscriber was present. Delivery to each subscriber is non-blocking: if a
// subscriber's buffer is full the event is dropped for that subscriber (its
// receipt does not affect the reported result). A Publish with no subscribers
// returns false so the caller can tell the AI there is no live session.
func (b *Bus) Publish(e Event) (delivered bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		delivered = true
		select {
		case ch <- e:
		default: // subscriber is behind; drop this event for it
		}
	}
	return delivered
}
