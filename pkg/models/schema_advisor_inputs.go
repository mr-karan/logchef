package models

// AdvisorInputState is how one schema advisor read on ClickHouse ended.
type AdvisorInputState string

const (
	AdvisorInputOK      AdvisorInputState = "ok"
	AdvisorInputDenied  AdvisorInputState = "denied"  // ACCESS_DENIED
	AdvisorInputLimited AdvisorInputState = "limited" // a budget stopped it; no rule may use its partial answer
	AdvisorInputMissing AdvisorInputState = "missing" // UNKNOWN_TABLE, as system.query_log with logging off
	AdvisorInputError   AdvisorInputState = "error"
	AdvisorInputSkipped AdvisorInputState = "skipped"
)

// AdvisorInput reports one read and the work ClickHouse counted for it.
type AdvisorInput struct {
	Name      string            `json:"name"` // "system.columns", "sample", ...
	State     AdvisorInputState `json:"state"`
	Detail    string            `json:"detail,omitempty"`
	ReadRows  uint64            `json:"read_rows"`
	ReadBytes uint64            `json:"read_bytes"`
}

// Kinds of AdvisorTarget.
const (
	AdvisorTargetTable            = "table"
	AdvisorTargetDistributedLocal = "distributed_local"
	AdvisorTargetViewBase         = "view_base"
)

// AdvisorTarget is the table that owns a source's data: metadata and DDL
// refer to it, while usage comes from the source.
type AdvisorTarget struct {
	Kind     string `json:"kind"`
	Database string `json:"database"`
	Table    string `json:"table"`
	Cluster  string `json:"cluster,omitempty"`
	Engine   string `json:"engine"`
}
