CREATE TABLE transaction_workspaces (
    transaction_id TEXT PRIMARY KEY REFERENCES transactions(transaction_id) ON DELETE CASCADE,
    repository_root TEXT NOT NULL,
    git_common_dir TEXT NOT NULL,
    source_head_ref TEXT,
    base_oid TEXT NOT NULL,
    object_format TEXT NOT NULL,
    workspace_path TEXT NOT NULL UNIQUE,
    artifacts_path TEXT NOT NULL UNIQUE,
    dirty_policy TEXT NOT NULL,
    source_status_digest TEXT NOT NULL,
    created_at TEXT NOT NULL,
    aborted_at TEXT
);

CREATE TABLE staged_patches (
    transaction_id TEXT PRIMARY KEY REFERENCES transactions(transaction_id) ON DELETE CASCADE,
    patch_path TEXT NOT NULL UNIQUE,
    patch_sha256 TEXT NOT NULL,
    patch_size_bytes INTEGER NOT NULL CHECK (patch_size_bytes >= 0),
    staged_tree_oid TEXT NOT NULL,
    changed_path_count INTEGER NOT NULL CHECK (changed_path_count >= 0),
    changed_paths_json TEXT NOT NULL,
    approval_material_digest TEXT NOT NULL,
    generated_at TEXT NOT NULL
);

CREATE TABLE materialized_repository_refs (
    transaction_id TEXT PRIMARY KEY REFERENCES transactions(transaction_id) ON DELETE CASCADE,
    ref_name TEXT NOT NULL UNIQUE,
    commit_oid TEXT NOT NULL,
    resulting_tree_oid TEXT NOT NULL,
    materialized_at TEXT NOT NULL
);

CREATE TABLE daemon_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
