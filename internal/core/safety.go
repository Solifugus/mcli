package core

import (
	"github.com/Solifugus/mcli/internal/core/safety"
)

// Policy assembles the live guardrail policy from settings and the runtime
// read-only toggle. Both front-ends call through here so the rules are identical.
func (c *Core) Policy() safety.Policy {
	return safety.Policy{
		ConfirmDangerous:     c.settings.ConfirmDangerousSQL,
		ReadOnly:             c.readOnly,
		BlockDangerousOnProd: c.settings.BlockDangerousOnProd,
		Keywords:             c.settings.DangerousSQL,
	}
}

// ReadOnly reports whether read-only mode is currently engaged.
func (c *Core) ReadOnly() bool { return c.readOnly }

// SetReadOnly toggles read-only mode for the session.
func (c *Core) SetReadOnly(on bool) {
	c.readOnly = on
	if on {
		c.log("READONLY", "on")
	} else {
		c.log("READONLY", "off")
	}
}

// GuardStatement classifies a statement and decides what to do with it against
// the current policy and the connected server's environment. Front-ends use it
// to confirm or refuse before running. The verdict's reason accompanies a
// Confirm/Block action.
func (c *Core) GuardStatement(sql string) (safety.Action, safety.Verdict, string) {
	v := safety.Classify(sql, c.settings.DangerousSQL)
	action, reason := c.Policy().Decide(v, c.Environment())
	return action, v, reason
}
