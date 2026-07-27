# ADR-085 — One daemon writer per data root

FutureDiff uses a kernel-held exclusive file lock as the authoritative single-instance boundary. PID files are informational and support signalling, but file existence or PID parsing cannot safely provide mutual exclusion. A second writer must fail before SQLite is opened.
