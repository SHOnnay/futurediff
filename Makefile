PUBLIC_COMMANDS := fdif futurediff futurediffd
COMMANDS := fdif futurediff futurediffd futurediff-mcp futurediff-certify futurediff-bench futurediff-sbom futurediff-admin futurediff-demo futurediff-integrate futurediff-provenance futurediff-cert-suite futurediff-install futurediff-platform futurediff-agent-bench futurediff-verify-release futurediff-provider-cert futurediff-audit futurediff-prune futurediff-doctor futurediff-api-contract futurediff-export futurediff-restore futurediff-replay futurediff-config-lint futurediff-api-diff futurediff-effectspec futurediff-policy-explain futurediff-recovery-drill futurediff-metrics futurediff-support-bundle futurediff-approval futurediff-policy-bundle futurediff-diff futurediff-upgrade-rehearsal futurediff-compat futurediff-maintenance futurediff-evidence futurediff-timeline futurediff-threat-test futurediff-config-snapshot futurediff-approval-quorum futurediff-incident futurediff-drain futurediff-operator-receipt futurediff-retention-policy futurediff-effect-graph futurediff-slo futurediff-readiness futurediff-secret-scan futurediff-quota futurediff-api-audit futurediff-daemon-lock futurediff-rate-policy futurediff-config-sign futurediff-root-audit futurediff-ledger-maintain futurediff-integrity-checkpoint futurediff-lease-cleanup futurediff-repository-policy futurediff-expire futurediff-idempotency-gc futurediff-storage-check futurediff-openapi futurediff-backup-catalog futurediff-authz futurediff-capability futurediff-authz-audit futurediff-authz-conformance futurediff-access-audit futurediff-tenant-conformance

.PHONY: fmt test race vet build build-public public-package verify-public-package public-alpha-test check demo benchmark sbom release clean

fmt:
	gofmt -w ./cmd ./effectspec ./internal

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

build:
	mkdir -p bin
	@for cmd in $(COMMANDS); do go build -trimpath -o bin/$$cmd ./cmd/$$cmd; done

build-public:
	mkdir -p bin
	@for cmd in $(PUBLIC_COMMANDS); do go build -trimpath -o bin/$$cmd ./cmd/$$cmd; done

public-package:
	./scripts/build-public-release.sh "$${VERSION:-$$(cat VERSION)}" "$${OUT:-dist/public}"

verify-public-package:
	@set -eu; \
	out="$${OUT:-dist/public}"; \
	test -d "$$out" || { echo "public package directory not found: $$out" >&2; exit 1; }; \
	cd "$$out"; \
	count=$$(find . -maxdepth 1 -type f -name '*.sha256' -print | wc -l | tr -d ' '); \
	test "$$count" -gt 0 || { echo "no checksum sidecars found in $$out" >&2; exit 1; }; \
	if command -v sha256sum >/dev/null 2>&1; then \
		sha256sum -c ./*.sha256; \
	else \
		shasum -a 256 -c ./*.sha256; \
	fi

public-alpha-test:
	python3 -m unittest discover -s tests -p 'test_public_alpha.py' -v

check:
	$(MAKE) public-alpha-test
	test -z "$$(gofmt -l ./cmd ./effectspec ./internal)"
	go vet ./...
	go test -race ./...
	go build ./cmd/...

demo:
	./scripts/demo.sh

benchmark:
	go run ./cmd/futurediff-bench --scenarios examples/benchmark --json benchmark-report.json --markdown benchmark-report.md

sbom:
	go run ./cmd/futurediff-sbom --root . --output futurediff.spdx.json

release:
	./scripts/release.sh

clean:
	rm -rf bin dist coverage.out benchmark-report.json benchmark-report.md futurediff.spdx.json
