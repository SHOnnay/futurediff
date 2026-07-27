# Task 060 — Bounded daemon drain and shutdown

## Delivered
- Active mutation tracking
- New-mutation rejection during drain
- Health-visible drain status
- Graceful `http.Server.Shutdown`
- Configurable shutdown timeout
- Forced close after timeout
- Secure PID file with stale/live-process detection
- `futurediff-drain` confirmation-gated operator command
- Unix socket and PID cleanup

## Security boundary
The drain command requires `DRAIN_FUTUREDIFF_DAEMON`. Read operations may remain available while active mutations finish.

## Validation
A live daemon accepted health checks, received SIGTERM through the drain command, exited cleanly, and removed both socket and PID file.
