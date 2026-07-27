# Task 025 — Installer and user-service management

## Objective

Provide a reviewable installation path for the complete FutureDiff command set without silently enabling provider credentials or enforced mode.

## Delivered

- `futurediff-install`
- Declarative JSON installation plan
- Atomic binary copying
- Linux systemd user service generation
- macOS launchd user service generation
- `0600` service files and `0700` data root
- Optional runtime image and credential-metadata path wiring
- Dry-run support
- Unit tests for copying, service rendering, and OS mismatch rejection

## Security properties

- No token value is accepted or written by the installer.
- Credentials are referenced only by an optional metadata-config path.
- The systemd unit applies `NoNewPrivileges`, `PrivateTmp`, `ProtectSystem=strict`, `ProtectHome=read-only`, and an explicit writable data path.
- Service formats are rejected on the wrong operating system.

## Executed result

The installer copied all 16 current binaries into an isolated test prefix and created a private data root.

## Limitations

Package-manager integration, automatic service enable/start, uninstall, and upgrade rollback are not included yet. The installer writes service definitions but does not invoke `systemctl` or `launchctl` automatically.
