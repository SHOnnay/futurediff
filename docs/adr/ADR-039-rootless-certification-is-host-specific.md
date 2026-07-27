# ADR-039: Rootless certification is host-specific

Status: accepted

FutureDiff does not infer enforced-mode certification from source code or a Docker-compatible CLI alone. The `futurediff-certify` command executes isolation checks against the exact runtime, daemon configuration, image digest, UID mapping, and host where FutureDiff will run. Certification reports are content-digested and must be regenerated after changing any of those inputs.
