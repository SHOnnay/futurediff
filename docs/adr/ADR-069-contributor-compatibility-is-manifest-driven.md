# ADR-069: Contributor compatibility is manifest-driven

Status: accepted

Compatibility checks use a strict, path-contained manifest. The harness validates API baselines against the current daemon contract, verification contracts, EffectSpec descriptors, policy bundles, and supported configuration profiles. Any failed or escaped path causes a nonzero result. This is a local contributor gate, not a substitute for live provider certification.
