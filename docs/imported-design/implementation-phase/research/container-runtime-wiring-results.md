# Container Runtime Wiring Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This result records the first real wiring of the Docker-compatible runtime seam into staged command execution.

## Implemented

- `futurediff/control-plane/gateway/spike.go`
- `futurediff/control-plane/gateway/spike_test.go`
- `futurediff/integrations/mvpflow/flow.go`
- `futurediff/adapters/runtime/dockerrun/gatewayexecutor.go`

## Proven behavior

- staged command execution now accepts injected command executors;
- the default path still uses host-shell execution;
- Docker-backed execution can now be selected for staged command runs;
- the staged patch capture path still works when the command runs through the Docker executor seam.

## Verification

- `go test ./...` passes, including the gateway test that stages a patch through the Docker executor path.

## Why this matters

This closes the last big honesty gap in the runtime story: the container seam is no longer only a hardening plan, it is now wired into the actual staged execution boundary.

## Next useful move

Promote the Docker-backed executor from optional seam to one of the supported default runtime modes once local bootstrap and CI policy are ready for it.
