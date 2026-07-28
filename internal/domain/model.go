package domain

import "time"

type Transaction struct {
	ID                string           `json:"transaction_id"`
	OwnerPrincipalID  string           `json:"owner_principal_id"`
	ProtocolVersion   string           `json:"protocol_version"`
	Mode              string           `json:"mode"`
	AgentAdapter      string           `json:"agent_adapter,omitempty"`
	AgentSessionID    string           `json:"agent_session_id,omitempty"`
	WorkspaceIdentity string           `json:"workspace_identity,omitempty"`
	BaseRevision      string           `json:"base_revision,omitempty"`
	Status            TransactionState `json:"status"`
	PolicyVersion     string           `json:"policy_version"`
	Revision          int64            `json:"revision"`
	MaterialRevision  int64            `json:"material_revision"`
	ApprovalDigest    string           `json:"approval_digest,omitempty"`
	CreatedAt         time.Time        `json:"created_at"`
	SealedAt          *time.Time       `json:"sealed_at,omitempty"`
	UpdatedAt         time.Time        `json:"updated_at"`
}

type Workspace struct {
	TransactionID      string     `json:"transaction_id"`
	RepositoryRoot     string     `json:"repository_root"`
	GitCommonDir       string     `json:"git_common_dir"`
	SourceHeadRef      string     `json:"source_head_ref,omitempty"`
	BaseOID            string     `json:"base_oid"`
	ObjectFormat       string     `json:"object_format"`
	WorkspacePath      string     `json:"workspace_path"`
	ArtifactsPath      string     `json:"artifacts_path"`
	DirtyPolicy        string     `json:"dirty_policy"`
	SourceStatusDigest string     `json:"source_status_digest"`
	CreatedAt          time.Time  `json:"created_at"`
	AbortedAt          *time.Time `json:"aborted_at,omitempty"`
}

type Patch struct {
	TransactionID          string    `json:"transaction_id"`
	PatchPath              string    `json:"patch_path"`
	PatchSHA256            string    `json:"patch_sha256"`
	PatchSizeBytes         int64     `json:"patch_size_bytes"`
	StagedTreeOID          string    `json:"staged_tree_oid"`
	ChangedPaths           []string  `json:"changed_paths"`
	ApprovalMaterialDigest string    `json:"approval_material_digest"`
	GeneratedAt            time.Time `json:"generated_at"`
}

type VerificationCheckResult struct {
	CheckID         string `json:"check_id"`
	Required        bool   `json:"required"`
	Status          string `json:"status"`
	CheckSpecDigest string `json:"check_spec_digest"`
	CacheKey        string `json:"cache_key"`
	EvidenceDigest  string `json:"evidence_digest"`
	Message         string `json:"message,omitempty"`
}

type VerificationReport struct {
	VerificationID     string                    `json:"verification_id"`
	TransactionID      string                    `json:"transaction_id"`
	ContractID         string                    `json:"contract_id"`
	ContractDigest     string                    `json:"contract_digest"`
	MaterialDigest     string                    `json:"material_digest"`
	Outcome            string                    `json:"outcome"`
	Results            []VerificationCheckResult `json:"results"`
	VerificationDigest string                    `json:"verification_digest"`
	PolicyVersion      string                    `json:"policy_version"`
	CreatedAt          time.Time                 `json:"created_at"`
}

type MaterializedRef struct {
	TransactionID    string    `json:"transaction_id"`
	RefName          string    `json:"ref_name"`
	CommitOID        string    `json:"commit_oid"`
	ResultingTreeOID string    `json:"resulting_tree_oid"`
	MaterializedAt   time.Time `json:"materialized_at"`
}

type RuntimeExecution struct {
	ExecutionID           string    `json:"execution_id"`
	TransactionID         string    `json:"transaction_id"`
	Purpose               string    `json:"purpose"`
	CommandDigest         string    `json:"command_digest"`
	EnvironmentDigest     string    `json:"environment_digest"`
	PolicyDigest          string    `json:"policy_digest"`
	Image                 string    `json:"image"`
	ImageDigest           string    `json:"image_digest"`
	RuntimeKind           string    `json:"runtime_kind"`
	RuntimeVersion        string    `json:"runtime_version"`
	ExitCode              int       `json:"exit_code"`
	TerminationReason     string    `json:"termination_reason"`
	StdoutPath            string    `json:"stdout_path,omitempty"`
	StderrPath            string    `json:"stderr_path,omitempty"`
	EvidencePath          string    `json:"evidence_path"`
	WorkspaceSynchronized bool      `json:"workspace_synchronized"`
	StartedAt             time.Time `json:"started_at"`
	FinishedAt            time.Time `json:"finished_at"`
}
