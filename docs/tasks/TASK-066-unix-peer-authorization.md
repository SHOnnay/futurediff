# Task 066 — Kernel-authenticated Unix peer authorization

## Goal
Prevent an arbitrary local process from using the FutureDiff daemon merely because it can locate the Unix socket.

## Implemented

- Linux `SO_PEERCRED` extraction for PID, UID and GID.
- Request identity is attached through `http.Server.ConnContext`.
- The daemon defaults to the effective UID that started it.
- `--allowed-peer-uids` supports an explicit comma-separated allowlist.
- `--disable-peer-auth` is available only as an explicit compatibility escape hatch.
- Unauthorized or unavailable identities receive HTTP 403 before handlers execute.
- The socket remains mode `0600` as a second independent control.

## Security boundary

The peer UID comes from the kernel, not an HTTP header. This is local principal authentication, not multi-user RBAC. Non-Linux platforms do not claim kernel peer-credential enforcement in this version.

## Validation

- Missing peer identity is rejected.
- An allowlisted UID is accepted.
- A real Unix-socket daemon request from the starting UID succeeds.
- A daemon configured with a different UID rejects the same client.
