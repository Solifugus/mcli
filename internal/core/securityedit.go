package core

import (
	"github.com/Solifugus/mcli/internal/core/adapter"
)

// BuildGrant returns a GRANT (or REVOKE, when revoke is true) statement for the
// connected dialect. With a non-empty object it grants privileges on that object;
// with an empty object items are role names for a role grant. It only BUILDS the
// statement — callers run it through the normal guarded execution path
// (GuardStatement + RunStatement), so read-only mode and production guards apply
// identically in every front-end. Gate on Supports(adapter.CapSecurityEdit).
func (c *Core) BuildGrant(items []string, object, principal string, revoke bool) (string, error) {
	if c.conn == nil {
		return "", ErrNotConnected
	}
	if !c.Supports(adapter.CapSecurityEdit) {
		return "", adapter.ErrUnsupported
	}
	return adapter.GrantStatement(c.dialect, items, object, principal, revoke)
}

// BuildCreateUser returns a dialect-correct CREATE USER/LOGIN statement with a
// password. Build-only (see BuildGrant).
func (c *Core) BuildCreateUser(name, password string) (string, error) {
	if c.conn == nil {
		return "", ErrNotConnected
	}
	if !c.Supports(adapter.CapSecurityEdit) {
		return "", adapter.ErrUnsupported
	}
	return adapter.CreateUserStatement(c.dialect, name, password)
}

// BuildDropUser returns a dialect-correct DROP USER/LOGIN/ROLE statement.
// Build-only (see BuildGrant); DROP is flagged dangerous by the safety guard, so
// executing it still requires confirmation or is blocked on production.
func (c *Core) BuildDropUser(name string) (string, error) {
	if c.conn == nil {
		return "", ErrNotConnected
	}
	if !c.Supports(adapter.CapSecurityEdit) {
		return "", adapter.ErrUnsupported
	}
	return adapter.DropUserStatement(c.dialect, name)
}
