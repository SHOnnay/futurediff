# ADR-058: Security fuzz seeds are release tests

Fuzz targets cover provider-host/path boundaries, credential destination matching, and release archive path safety. Normal `go test` executes the seed corpus; extended fuzzing can be run separately. Property tests also enforce terminal transaction-state behavior.
