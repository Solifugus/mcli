package adapter

import "sort"

// Capability names an optional adapter feature that a front-end can probe before
// offering it. Adapters advertise the set they support via Capabilities(); the
// core re-exposes it (Core.Capabilities), so the GUI can grey out an unsupported
// area in its nav, the CLI's .caps can say so, and the MCP server reports the
// same thing. This is the "advertising" half of the capability model: heavier
// method groups (jobs, security) are added as optional interfaces an adapter
// implements, but Capabilities() is the single source of truth a front-end reads
// up front instead of discovering support by calling and catching ErrUnsupported.
//
// A capability reflects what the *engine* can do, not what the current login is
// allowed to do. A method may still return ErrUnauthorized at call time when the
// connected user lacks the catalog privileges — that is a distinct condition a
// front-end should report differently ("you lack permission") from an
// unadvertised capability ("this database can't").
type Capability string

const (
	// CapExplain: ExplainQuery returns a real plan rather than ErrUnsupported.
	CapExplain Capability = "explain"
	// CapLineage: GetPreLineage/GetPostLineage return real dependency edges.
	CapLineage Capability = "lineage"
	// CapSource: the adapter can return an object's CREATE/definition text
	// (implements AdapterSource; added in a later phase).
	CapSource Capability = "source"
	// CapTableFunctions: table-valued functions are classified and queryable as a
	// tabular data source (added in a later phase).
	CapTableFunctions Capability = "table_functions"
	// CapJobs: the adapter exposes scheduler/agent jobs (implements AdapterJobs;
	// added in a later phase).
	CapJobs Capability = "jobs"
	// CapSecurity: the adapter exposes principals (users/roles) and grants read
	// side (implements AdapterSecurity; added in a later phase).
	CapSecurity Capability = "security"
	// CapSecurityEdit: the adapter can generate principal/grant DDL (DCL). Every
	// such statement still passes through the core safety guard.
	CapSecurityEdit Capability = "security_edit"
)

// AllCapabilities lists every defined capability in a stable display order, so
// a front-end can render the full feature surface (supported and not) rather
// than only what a given engine advertises.
func AllCapabilities() []Capability {
	return []Capability{
		CapExplain, CapLineage, CapSource, CapTableFunctions,
		CapJobs, CapSecurity, CapSecurityEdit,
	}
}

// CapabilitySet is an adapter's advertised feature set. The zero value (nil map)
// advertises nothing, which is the correct answer when disconnected.
type CapabilitySet map[Capability]bool

// Has reports whether c is advertised.
func (s CapabilitySet) Has(c Capability) bool { return s[c] }

// Sorted returns the advertised capabilities as a sorted slice, for stable
// display in .caps / MCP output.
func (s CapabilitySet) Sorted() []Capability {
	out := make([]Capability, 0, len(s))
	for c, ok := range s {
		if ok {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Caps builds a CapabilitySet from the given capabilities (all set true). An
// adapter's Capabilities() implementation is typically a one-line Caps(...) call.
func Caps(cs ...Capability) CapabilitySet {
	set := make(CapabilitySet, len(cs))
	for _, c := range cs {
		set[c] = true
	}
	return set
}
