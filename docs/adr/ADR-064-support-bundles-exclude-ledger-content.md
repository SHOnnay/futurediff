# ADR-064 — Support bundles exclude ledger content

Support bundles contain diagnostics, audit summaries, aggregate metrics, build metadata, and the public API contract. They never contain the SQLite database, transaction patches, provider payloads, or credential configuration contents.
