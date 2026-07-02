package adapter

import "context"

// JobRef is the list-level view of a scheduled job: a SQL Server Agent job, an
// Oracle Scheduler job, or a MySQL event. It carries only what a listing needs —
// the name, whether it is enabled, and a best-effort one-line recurrence — so
// .jobs / list_jobs render cheaply without describing every job.
type JobRef struct {
	Name     string
	Enabled  bool
	Schedule string // best-effort recurrence text; may be "" at list level
}

// JobStep is one action a job runs. SQL Server Agent jobs have several ordered
// steps; Oracle Scheduler jobs and MySQL events have a single action, which is
// still represented as one step so front-ends render every engine uniformly.
type JobStep struct {
	Name    string
	Command string
}

// Job is a scheduled job's design — what DescribeJob returns. LastRun/NextRun and
// Comment are best-effort ("" when the engine does not expose them); Steps holds
// the action(s) the job runs.
type Job struct {
	Ref     JobRef
	Owner   string
	LastRun string
	NextRun string
	Comment string
	Steps   []JobStep
}

// JobRun is one execution-history record, newest-first as returned by JobHistory.
// Fields are text (already formatted by the adapter) so front-ends need no
// per-engine date/interval handling. Status is the engine's own outcome word,
// normalized to lower case where practical ("succeeded", "failed", "running").
type JobRun struct {
	Start   string
	End     string
	Status  string
	Message string
}

// AdapterJobs is the optional interface for scheduler / agent introspection
// (design §29, Scheduling area). An adapter that implements it must advertise
// CapJobs; the core probes for the interface and returns ErrUnsupported when an
// adapter does not implement it, so a front-end that checks Supports(CapJobs)
// first never reaches an unsupported call. Not every engine has a scheduler
// (Postgres has none natively), so this stays optional. All three methods are
// read-only catalog queries.
type AdapterJobs interface {
	// ListJobs returns scheduled jobs whose name contains substr (case-insensitive;
	// empty = all), ordered by name.
	ListJobs(ctx context.Context, substr string) ([]JobRef, error)

	// DescribeJob returns the design of a single job by name (its owner, schedule,
	// last/next run, comment, and steps). It returns a not-found error when nothing
	// matches.
	DescribeJob(ctx context.Context, name string) (Job, error)

	// JobHistory returns recent run records for a job, newest first, up to limit
	// (limit <= 0 means the adapter's default). An engine that keeps no run history
	// (e.g. MySQL events) returns an empty slice, not an error.
	JobHistory(ctx context.Context, name string, limit int) ([]JobRun, error)
}
