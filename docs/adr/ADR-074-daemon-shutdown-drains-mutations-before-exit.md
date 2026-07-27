# ADR-074: Daemon shutdown drains mutations before exit

## Decision
SIGTERM starts a bounded drain: new mutations are rejected, active mutations may finish, then the HTTP server shuts down. Timeout forces closure.

## Rationale
Immediate process termination can interrupt write-ahead and provider operations at unsafe points.

## Consequences
Read-only health remains available during the drain window. The PID and Unix socket are removed on exit.
