# ADR-060 — EffectSpec conformance is library-first

Third-party adapters use a reusable Go conformance suite in their own test process. FutureDiff does not load arbitrary plugin binaries into the trusted daemon. This preserves a stable lifecycle contract without weakening the adapter trust boundary.
