package envelope

// Policy domain
const (
	PolicyUnavailable   = "POLICY_UNAVAILABLE"
	PolicyViolation     = "POLICY_VIOLATION"
	PolicyIngestInvalid = "POLICY_INGEST_INVALID"
)

// Scanner domain
const (
	ScannerFailed  = "SCANNER_FAILED"
	ScannerTimeout = "SCANNER_TIMEOUT"
)

// Execution domain
const (
	ExecutionFailed  = "EXECUTION_FAILED"
	ExecutionTimeout = "EXECUTION_TIMEOUT"
	ExecutionDenied  = "EXECUTION_DENIED"
)

// Auth domain
const (
	Unauthorized = "UNAUTHORIZED"
	Forbidden    = "FORBIDDEN"
	TokenExpired = "TOKEN_EXPIRED"
)

// Agent domain
const (
	AgentFailed  = "AGENT_FAILED"
	AgentTimeout = "AGENT_TIMEOUT"
)

// General domain
const (
	InternalError = "INTERNAL_ERROR"
	NotFound      = "NOT_FOUND"
	BadRequest    = "BAD_REQUEST"
	RateLimited   = "RATE_LIMITED"
	Conflict      = "CONFLICT"
)

// EE (Enterprise Edition) domain
const (
	LicenseRequired  = "LICENSE_REQUIRED"
	LicenseExpired   = "LICENSE_EXPIRED"
	QuotaExceeded    = "QUOTA_EXCEEDED"
	CrossOrgDenied   = "CROSS_ORG_DENIED"
)

// Resource domain
const (
	TaskNotFound        = "TASK_NOT_FOUND"
	TaskAlreadyApproved = "TASK_ALREADY_APPROVED"
	PlanNotFound        = "PLAN_NOT_FOUND"
)
