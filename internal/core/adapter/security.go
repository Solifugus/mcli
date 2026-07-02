package adapter

import "context"

// Principal kinds. A front-end filters ListPrincipals by one of these, or passes
// "" for both. The user/role split is engine-mapped: in Postgres a "user" is a
// role that can log in and a "role" is one that cannot; SQL Server and Oracle have
// first-class users and roles; MySQL accounts are users (its roles are fuzzy to
// separate from the catalog and are reported best-effort).
const (
	PrincipalKindUser = "user"
	PrincipalKindRole = "role"
)

// PrincipalRef is the list-level view of a security principal (a user or role).
// Enabled is best-effort — login capability where the engine exposes it, else
// true.
type PrincipalRef struct {
	Name    string
	Kind    string // PrincipalKindUser or PrincipalKindRole
	Enabled bool
}

// Grant is one privilege a principal holds: a privilege name and the object or
// scope it applies to. On is "" for cluster/system-wide privileges.
type Grant struct {
	Privilege string
	On        string // schema.object, database, or "" for a system/cluster privilege
}

// Principal is a security principal's configuration — what DescribePrincipal
// returns. Attributes are engine flags (e.g. Postgres SUPERUSER/CREATEDB);
// MemberOf lists the roles this principal belongs to; Members lists the principals
// that belong to it (populated for roles); Grants lists object/system privileges.
// All are best-effort — an engine (or the current login's catalog privileges) may
// leave any of them empty.
type Principal struct {
	Ref        PrincipalRef
	Attributes []string
	MemberOf   []string
	Members    []string
	Grants     []Grant
	Comment    string
}

// AdapterSecurity is the optional interface for the read side of security
// introspection (design §29, Security area). An adapter that implements it must
// advertise CapSecurity; the core probes for the interface and returns
// ErrUnsupported when an adapter does not implement it. A method may still return
// ErrUnauthorized when the connected login lacks the catalog privileges to read
// the security catalog — distinct from ErrUnsupported (the engine can, but you may
// not). Both methods are read-only; the editing side (GRANT/REVOKE/CREATE USER)
// is a separate, guarded capability (CapSecurityEdit).
type AdapterSecurity interface {
	// ListPrincipals returns principals whose kind matches kind (PrincipalKindUser,
	// PrincipalKindRole, or "" for both) and whose name contains substr
	// (case-insensitive; empty = all), ordered by name.
	ListPrincipals(ctx context.Context, kind, substr string) ([]PrincipalRef, error)

	// DescribePrincipal returns a single principal's configuration by name. It
	// returns a not-found error when nothing matches.
	DescribePrincipal(ctx context.Context, name string) (Principal, error)
}
