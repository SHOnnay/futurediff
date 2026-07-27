# ADR-067: Transaction diff is a non-secret projection

Status: accepted

The transaction diff summarizes durable snapshot metadata rather than rendering provider response bodies or credential material. It includes changed paths, patch and tree identities, verification outcome, effect lifecycle states, approval and receipt counts, warnings, and a summary digest. The summary is suitable for review interfaces and command-line approval workflows.
