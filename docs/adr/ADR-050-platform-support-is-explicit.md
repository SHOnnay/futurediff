# ADR-050: Platform support is explicit

Linux amd64 is the supported primary target. Linux arm64 and macOS amd64/arm64 are experimental native build targets. Windows is explicitly unsupported until named pipes, service management, SQLite packaging, and enforced credential isolation are implemented. Unsupported platforms must fail rather than silently weakening guarantees.
