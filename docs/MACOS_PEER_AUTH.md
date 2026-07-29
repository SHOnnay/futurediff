# macOS local peer authentication

FutureDiff's local daemon authorizes requests made over its Unix-domain socket.
The public alpha must keep this authorization enabled by default on both Linux
and macOS.

## Platform behavior

| Platform/build | Peer identity source | UID | GID | PID | Secure default |
|---|---|---:|---:|---:|---|
| Linux | `SO_PEERCRED` | yes | yes | yes | supported |
| macOS with CGO | `getpeereid(3)` | yes | yes | unavailable (`-1`) | supported |
| macOS without CGO | none | no | no | no | startup rejected |
| Other platforms | none | no | no | no | startup rejected |

Apple documents `getpeereid(3)` as returning the effective user and group IDs
of the peer connected to a Unix-domain stream socket:

- https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man3/getpeereid.3.html

macOS does not return a peer process ID through `getpeereid`. FutureDiff stores
`PID: -1` to represent that the value is unavailable. Authorization uses the
kernel-reported UID, not the PID.

## Security behavior

- Peer authentication remains enabled by default.
- Unsupported builds fail during daemon startup rather than starting an
  unusable or silently unauthenticated service.
- Failure to read a connection's peer identity remains fail-closed: the API
  request is rejected by the peer guard.
- `--disable-peer-auth` remains an explicit unsafe escape hatch for disposable
  development only. It must not be used in public demonstrations, release
  evidence, or normal operation.
- The Unix socket remains permission-restricted independently of peer identity.

## Build requirement on macOS

The secure macOS implementation calls the platform `getpeereid` function
through CGO. Build public macOS binaries with:

```bash
CGO_ENABLED=1 go build ./cmd/futurediffd
```

The Apple Command Line Tools or Xcode toolchain must be available. The normal
FutureDiff macOS build already requires a C toolchain for SQLite.

## Developer verification

Run the package tests:

```bash
CGO_ENABLED=1 go test ./internal/peerauth
CGO_ENABLED=1 go test -race ./internal/peerauth
```

Run the authenticated daemon smoke test:

```bash
./scripts/validate-macos-peer-auth.sh
```

The smoke test deliberately starts `futurediffd` without
`--disable-peer-auth`, connects through its Unix socket, and requires the
health endpoint to report that peer authentication is enabled.

## Release gate

A macOS release asset must not be published unless all of these pass on a real
macOS runner:

1. `CGO_ENABLED=1 go test ./internal/peerauth`
2. `CGO_ENABLED=1 go test -race ./internal/peerauth`
3. `go test ./...`
4. `go test -race ./...`
5. `go build ./cmd/...`
6. `./scripts/validate-macos-peer-auth.sh`
7. `fdif doctor` against a daemon started with peer authentication enabled

The evidence must record the macOS version, architecture, Go version,
`CGO_ENABLED`, compiler version, source commit, and command exit statuses.
