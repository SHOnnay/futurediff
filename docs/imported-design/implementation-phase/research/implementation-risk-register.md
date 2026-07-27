# Implementation Risk Register

## Status

Initial research-phase register.

## Purpose

This register tracks build-critical risks discovered during Phase 7 research. It is not a generic backlog. Every item here should threaten correctness, recovery credibility, integration feasibility, or MVP timeline.

## Initial risks

| Risk ID | Description | Impact | Likelihood | Mitigation | Blocked area | Needs spike |
|---|---|---|---|---|---|---|
| R-001 | Wrapper/proxy path may not capture all agent effect surfaces cleanly. | High | Medium | Prove one end-to-end wrapper boundary early with real tool-call capture. | Interception strategy | Yes |
| R-002 | GitHub and Slack may only support weaker transactional guarantees than optimistic product language suggests. | High | High | Build adapter reality matrix early and downgrade support levels honestly. | Adapter design | Yes |
| R-003 | Postgres-only worker claims may become messy if lease semantics are vague. | High | Medium | Design and spike strict claim/renew/steal rules before broad coordinator work. | Control plane | Yes |
| R-004 | Local staging environment may become too heavy for contributors if worktree + container + disposable DB setup is clumsy. | Medium | Medium | Freeze one simple local-dev stack and avoid optional-path explosion. | Developer experience | Yes |
| R-005 | Artifact and evidence storage could leak into Postgres if blob boundaries are not enforced early. | Medium | Medium | Define artifact size rules and storage abstraction before benchmark outputs expand. | Persistence | Yes |
| R-006 | Approval and drift semantics may be implemented inconsistently across adapters. | High | Medium | Force adapter testkit checks for resource URIs, fingerprints, and freshness handling. | Adapter conformance | No |
| R-007 | Recovery behavior may look correct in prose but fail under ambiguous provider outcomes. | High | Medium | Add early `UNKNOWN` and reconciliation spikes before full adapter expansion. | Recovery engine | Yes |
| R-008 | Team may drift into UI or broad integration work before the engine proves itself. | Medium | Medium | Keep bootstrap milestone scoped to specs, coordinator, testkit, and one vertical slice. | Delivery focus | No |

## Current priority

Research should attack these first:

1. `R-001`
2. `R-002`
3. `R-003`
4. `R-007`

Those four most directly affect whether FutureDiff can honestly enter implementation.

## Update rule

Every new research decision should either:
- reduce one risk;
- split one large risk into clearer sub-risks; or
- record that a risk remains and requires a spike.
