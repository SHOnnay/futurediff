COMMANDS := futurediff futurediffd futurediff-mcp futurediff-certify futurediff-certify-providers futurediff-bench futurediff-sbom futurediff-admin futurediff-demo futurediff-integrate futurediff-provenance

.PHONY: fmt test race vet build check demo benchmark provider-smoke provider-certify-live rootless-certify rootless-certify-live sbom release clean




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

check:
	test -z "$$(gofmt -l ./cmd ./effectspec ./internal)"
	go vet ./...
	go test -race ./...
	go build ./cmd/...

demo:
	./scripts/demo.sh

benchmark:
	go run ./cmd/futurediff-bench --scenarios examples/benchmark --json benchmark-report.json --markdown benchmark-report.md

provider-smoke:
	go run ./cmd/futurediff-certify-providers $(ARGS)

provider-certify-live:
	bash ./scripts/certify-providers.sh

rootless-certify-live:
	bash ./scripts/certify-rootless-oci.sh

rootless-certify:
	go run ./cmd/futurediff-certify $(ARGS)

sbom:
	go run ./cmd/futurediff-sbom --root . --output futurediff.spdx.json

release:
	./scripts/release.sh

clean:
	rm -rf bin dist coverage.out benchmark-report.json benchmark-report.md futurediff.spdx.json
