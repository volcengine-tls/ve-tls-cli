package contract

const (
	CatalogV2SchemaVersion   = "v2"
	CatalogV2DigestAlgorithm = "sha256/go-json-v1"

	CodecJSON        = "json"
	CodecPutLogs     = "putlogs"
	CodecWebTracks   = "webtracks"
	CodecConsumeLogs = "consumelogs"

	PaginationPageNumber PaginationMode = "page-number"
	PaginationCursor     PaginationMode = "cursor"
)

type OperationID string
type CodecID string
type PaginationMode string

// JSONSchema is the JSON-compatible schema representation used by generated
// operation contracts. Numbers intentionally retain encoding/json's float64
// semantics after loading.
type JSONSchema map[string]any

type Catalog struct {
	SchemaVersion   string      `json:"schema_version"`
	ContractVersion string      `json:"contract_version"`
	DigestAlgorithm string      `json:"digest_algorithm"`
	ContextSchema   JSONSchema  `json:"context_schema"`
	ExecutionSchema JSONSchema  `json:"execution_schema"`
	Operations      []Operation `json:"operations"`
}

type Operation struct {
	ID          OperationID     `json:"id"`
	Group       string          `json:"group"`
	GroupTitle  string          `json:"group_title"`
	Action      string          `json:"action"`
	Resource    string          `json:"resource"`
	Verb        string          `json:"verb"`
	Family      string          `json:"family"`
	Visibility  string          `json:"visibility"`
	Wire        WireSpec        `json:"wire"`
	InputSchema JSONSchema      `json:"input_schema"`
	Pagination  *PaginationSpec `json:"pagination,omitempty"`
	Runtime     RuntimeSpec     `json:"runtime"`
	Output      OutputSpec      `json:"output"`
	Docs        DocsSpec        `json:"docs"`
	Risk        RiskSpec        `json:"risk"`
}

type WireSpec struct {
	Method        string  `json:"method"`
	Path          string  `json:"path"`
	RequestFormat string  `json:"request_format"`
	Codec         CodecID `json:"codec"`
}

type PaginationSpec struct {
	Mode            PaginationMode `json:"mode"`
	PageNumberParam string         `json:"page_number_param,omitempty"`
	PageSizeParam   string         `json:"page_size_param,omitempty"`
	CursorParam     string         `json:"cursor_param,omitempty"`
	NextCursorField string         `json:"next_cursor_field,omitempty"`
	ItemsField      string         `json:"items_field"`
	TotalField      string         `json:"total_field,omitempty"`
	DefaultPageSize int            `json:"default_page_size,omitempty"`
	MaxPages        int            `json:"max_pages"`
}

type RuntimeSpec struct {
	SupportsDryRun bool `json:"supports_dry_run"`
}

type OutputSpec struct {
	Policy           string `json:"policy"`
	IsEnvelopeOutput bool   `json:"is_envelope_output"`
}

type DocsSpec struct {
	Summary          string `json:"summary"`
	Source           string `json:"source"`
	UsageConstraints string `json:"usage_constraints"`
}

type RiskSpec struct {
	Level         string `json:"level"`
	ErrorRecovery string `json:"error_recovery"`
}

// LegacyToolV1 mirrors the legacy CLI tool JSON field order. Do not reorder
// fields: encoding/json struct order is part of LegacyToolDigestV1.
type LegacyToolV1 struct {
	ID               string     `json:"id"`
	Group            string     `json:"group"`
	Action           string     `json:"action"`
	Resource         string     `json:"resource"`
	Verb             string     `json:"verb"`
	Family           string     `json:"family"`
	Method           string     `json:"method"`
	Path             string     `json:"path"`
	Visibility       string     `json:"visibility"`
	Summary          string     `json:"summary"`
	InputSchema      JSONSchema `json:"input_schema"`
	ContextSchema    JSONSchema `json:"context_schema"`
	ExecutionSchema  JSONSchema `json:"execution_schema"`
	OutputPolicy     string     `json:"output_policy"`
	ErrorRecovery    string     `json:"error_recovery"`
	DocSource        string     `json:"doc_source"`
	UsageConstraints string     `json:"usage_constraints"`
	RiskLevel        string     `json:"risk_level"`
	SupportsDryRun   bool       `json:"supports_dry_run"`
	SupportsAll      bool       `json:"supports_all"`
	IsEnvelopeOutput bool       `json:"is_envelope_output"`
}
