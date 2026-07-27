# ADR-047: Release provenance has local and signed layers

Status: accepted

Every release archive contains a deterministic in-toto statement with a SLSA provenance v1 predicate and SHA-256 subjects. This local statement is evidence but is not a cryptographic signature.

Tagged GitHub releases additionally use `actions/attest` to create a Sigstore-backed GitHub artifact attestation for the release archive. Consumers must verify the signed attestation and signer identity rather than trusting the embedded JSON alone.
