package core

import (
	"errors"

	"github.com/Solifugus/mcli/internal/core/assist"
)

// ErrNoLiveSession is returned by Guide when no front-end is subscribed to the
// assist bus — i.e. the ui_* tools were invoked against a headless server with
// no attached TUI/GUI to render the guidance (design §26).
var ErrNoLiveSession = errors.New("no live session attached: UI guidance is only available when an mcli TUI or GUI is running and attached")

// Guide publishes a guidance event to the active front-end. It returns
// ErrNoLiveSession when nothing is attached, so the caller (an MCP ui_* tool)
// can tell the AI its guidance had no surface to render on rather than silently
// dropping it. Guidance is advisory and never bypasses the safety guards: a
// Prefill only fills an input, it does not execute.
func (c *Core) Guide(e assist.Event) error {
	if !c.assist.Publish(e) {
		return ErrNoLiveSession
	}
	return nil
}

// LiveSession reports whether a front-end is attached to the assist bus.
func (c *Core) LiveSession() bool { return c.assist.HasSubscribers() }
