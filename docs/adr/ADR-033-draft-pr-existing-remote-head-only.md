# ADR-033: Task 010 draft PR supports existing remote head only

**Status:** Accepted as a temporary bounded capability

## Decision

Task 010 creates a GitHub draft pull request only when both head and base branches already exist remotely and their exact SHAs have been prepared and rechecked.

It does not push the local FutureDiff patch or claim that the remote head represents the approved local tree.

## Consequences

- the first provider-effect coordinator can be validated without embedding Git credential handling in a subprocess;
- the preview must expose `local_patch_published=false`;
- Task 011 must add controlled remote branch publication and an explicit dependency before this becomes the canonical code-to-PR workflow.
