# Disaster Recovery

FutureDiff recovery prioritizes evidence preservation and prevention of duplicate effects.

- RPO target: 300 seconds or less.
- RTO target: 900 seconds or less.
- Unknown outcomes after recovery: zero before dependent effects resume.
- Every backup contains a canonical file manifest.
- Restore rejects traversal paths, symlinks, hard links, devices, and corrupt archives.
- A restored directory is accepted only after complete manifest verification.

Run `scripts/recovery-drill.sh` in CI and on the operational schedule defined by the deployment owner.
After recovery, verify both evidence layers before resuming high-risk mutation:

```bash
futurediff-audit --root ~/.futurediff
futurediff-audit --root ~/.futurediff --operator-events
```
