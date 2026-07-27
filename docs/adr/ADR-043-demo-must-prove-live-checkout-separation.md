# ADR-043: The default demo must prove live-checkout separation

Status: accepted

The launch demonstration does not depend on a model or external provider. It must deterministically prove the central safety property: a verified and approved future is published to a FutureDiff ref while the live checkout remains unchanged. The demo emits a machine-readable report and exits unsuccessfully when this property is not true.
