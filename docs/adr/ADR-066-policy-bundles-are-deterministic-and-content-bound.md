# ADR-066: Policy bundles are deterministic and content-bound

Status: accepted

A `.fdpolicy` archive contains exactly a manifest and one verification contract. Archive ordering, timestamps, modes, labels, and JSON representations are normalized. The manifest binds the policy identity, policy version, and verification-contract digest. Verification rejects additional entries, unsafe entry types, oversized content, or digest disagreement.
