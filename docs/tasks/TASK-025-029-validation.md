# Tasks 025–029 Validation

## Static and Go checks

- `gofmt`: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- `go test -cover ./...`: PASS

## Build

Sixteen commands built successfully:

```text
futurediff
futurediffd
futurediff-mcp
futurediff-admin
futurediff-agent-bench
futurediff-bench
futurediff-cert-suite
futurediff-certify
futurediff-demo
futurediff-install
futurediff-integrate
futurediff-platform
futurediff-provenance
futurediff-provider-cert
futurediff-sbom
futurediff-verify-release
```

## Task-specific execution

- Platform matrix generated on Linux amd64: PASS
- Two-run agent benchmark report generated: PASS
- Installer copied 16 binaries into an isolated prefix: PASS
- v0.29.0 Linux amd64 release generated: PASS
- Release directory offline verification: 21 PASS, 0 FAIL, 1 SKIP
- Release archive offline verification: 21 PASS, 0 FAIL, 1 SKIP
- Provider certification with missing credential: correctly BLOCKED and nonzero
- Fake GitHub mutation cleanup: PASS
- Fake Slack message cleanup: PASS
- Archive traversal rejection test: PASS

## External limitations

The following were not executed because the required systems were unavailable:

- Real GitHub disposable repository mutation certification
- Real Slack disposable-channel mutation certification
- GitHub-signed attestation verification
- Native macOS CI jobs
- Real OpenCode/Hermes token and latency runs
