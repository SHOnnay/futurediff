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
	Patch         *Patch           `json:"patch,omitempty"`
	Transaction   *Transaction     `json:"transaction,omitempty"`
	Transactions  []Transaction    `json:"transactions,omitempty"`
	Workspace     *Workspace       `json:"workspace,omitempty"`
	Effects       []ExternalEffect `json:"effects,omitempty"`
	Receipts      []EffectReceipt  `json:"receipts,omitempty"`
	ResourceScope string           `json:"resource_scope,omitempty"`
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

type ExternalEffect struct {
	EffectID        string   `json:"effect_id"`
	TransactionID   string   `json:"transaction_id"`
	AdapterIdentity string   `json:"adapter_identity"`
	CredentialID    string   `json:"credential_id"`
	Destination     string   `json:"destination"`
	InputJSON       string   `json:"input_json"`
	PreviewJSON     string   `json:"preview_json"`
	Status          string   `json:"status"`
	DependsOn       []string `json:"depends_on,omitempty"`
}

type EffectReceipt struct {
	ReceiptID           string `json:"receipt_id"`
	EffectID            string `json:"effect_id"`
	ProviderOperationID string `json:"provider_operation_id,omitempty"`
	ProviderResourceID  string `json:"provider_resource_id,omitempty"`
	RequestDigest       string `json:"request_digest"`
	ResponseDigest      string `json:"response_digest,omitempty"`
	StatusQueryRef      string `json:"status_query_ref,omitempty"`
}

type githubBranchInput struct {
	Owner     string `json:"owner"`
	Repo      string `json:"repo"`
	Branch    string `json:"branch"`
	RemoteURL string `json:"remote_url"`
}

type githubDraftInput struct {
	Owner             string `json:"owner"`
	Repo              string `json:"repo"`
	Title             string `json:"title"`
	Body              string `json:"body,omitempty"`
	Head              string `json:"head"`
	Base              string `json:"base"`
	DependsOnEffectID string `json:"depends_on_effect_id,omitempty"`
}

type GitHubPublishResult struct {
	Requested          bool   `json:"requested"`
	Owner              string `json:"owner"`
	Repo               string `json:"repo"`
	Branch             string `json:"branch"`
	Base               string `json:"base"`
	Draft              bool   `json:"draft"`
	PullRequestURL     string `json:"pull_request_url,omitempty"`
	URLIsFallback      bool   `json:"url_is_fallback,omitempty"`
	EffectID           string `json:"effect_id,omitempty"`
	ProviderResourceID string `json:"provider_resource_id,omitempty"`
}

type Config struct {
	Binary             string `json:"binary"`
	DaemonBinary       string `json:"daemon_binary"`
	Socket             string `json:"socket"`
	StatePath          string `json:"state_path"`
	VerifyPolicy       string `json:"verify_policy"`
	CredentialConfig   string `json:"credential_config,omitempty"`
	GitHubCredentialID string `json:"github_credential_id,omitempty"`
	JSON               bool   `json:"json"`
	Interactive        bool   `json:"interactive"`
	Color              bool   `json:"color"`
	Unicode            bool   `json:"unicode"`
}
