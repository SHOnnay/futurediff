package guidedcli

import "time"

// Transaction is the stable subset of the canonical CLI transaction response
// required by the guided user experience. Unknown canonical fields are ignored.
type Transaction struct {
	BaseRevision      string `json:"base_revision,omitempty"`
	CreatedAt         string `json:"created_at,omitempty"`
	MaterialRevision  int    `json:"material_revision,omitempty"`
	Mode              string `json:"mode,omitempty"`
	OwnerPrincipalID  string `json:"owner_principal_id,omitempty"`
	PolicyVersion     string `json:"policy_version,omitempty"`
	ProtocolVersion   string `json:"protocol_version,omitempty"`
	Revision          int    `json:"revision,omitempty"`
	ApprovalDigest    string `json:"approval_digest,omitempty"`
	SealedAt          string `json:"sealed_at,omitempty"`
	Status            string `json:"status,omitempty"`
	TransactionID     string `json:"transaction_id,omitempty"`
	UpdatedAt         string `json:"updated_at,omitempty"`
	WorkspaceIdentity string `json:"workspace_identity,omitempty"`
}

type Workspace struct {
	ArtifactsPath      string `json:"artifacts_path,omitempty"`
	BaseOID            string `json:"base_oid,omitempty"`
	CreatedAt          string `json:"created_at,omitempty"`
	DirtyPolicy        string `json:"dirty_policy,omitempty"`
	GitCommonDir       string `json:"git_common_dir,omitempty"`
	ObjectFormat       string `json:"object_format,omitempty"`
	RepositoryRoot     string `json:"repository_root,omitempty"`
	SourceHeadRef      string `json:"source_head_ref,omitempty"`
	SourceStatusDigest string `json:"source_status_digest,omitempty"`
	TransactionID      string `json:"transaction_id,omitempty"`
	WorkspacePath      string `json:"workspace_path,omitempty"`
}

type Patch struct {
	ApprovalMaterialDigest string   `json:"approval_material_digest,omitempty"`
	ChangedPaths           []string `json:"changed_paths,omitempty"`
	GeneratedAt            string   `json:"generated_at,omitempty"`
	PatchPath              string   `json:"patch_path,omitempty"`
	PatchSHA256            string   `json:"patch_sha256,omitempty"`
	PatchSizeBytes         int64    `json:"patch_size_bytes,omitempty"`
	StagedTreeOID          string   `json:"staged_tree_oid,omitempty"`
	TransactionID          string   `json:"transaction_id,omitempty"`
}

type Response struct {
	Patch         *Patch        `json:"patch,omitempty"`
	Transaction   *Transaction  `json:"transaction,omitempty"`
	Transactions  []Transaction `json:"transactions,omitempty"`
	Workspace     *Workspace    `json:"workspace,omitempty"`
	ResourceScope string        `json:"resource_scope,omitempty"`
}

type ApprovalMaterial struct {
	TransactionDigest string `json:"transaction_digest"`
	TransactionID     string `json:"transaction_id"`
}

type CurrentTransaction struct {
	TransactionID  string    `json:"transaction_id"`
	RepositoryRoot string    `json:"repository_root,omitempty"`
	SelectedAt     time.Time `json:"selected_at"`
}

type Config struct {
	Binary       string `json:"binary"`
	DaemonBinary string `json:"daemon_binary"`
	Socket       string `json:"socket"`
	StatePath    string `json:"state_path"`
	VerifyPolicy string `json:"verify_policy"`
	JSON         bool   `json:"json"`
	Interactive  bool   `json:"interactive"`
	Color        bool   `json:"color"`
	Unicode      bool   `json:"unicode"`
}
