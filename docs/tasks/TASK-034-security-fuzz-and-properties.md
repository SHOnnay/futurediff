# Task 034 — Security fuzz and property tests

Added fuzz seed targets for:

- exact provider hostname/path enforcement;
- credential destination boundary matching;
- release archive/checksum path safety.

Added state-machine properties proving terminal transaction states have no outgoing transitions and every non-terminal state has at least one valid exit. Normal tests execute the seed corpus; longer fuzz campaigns can use `go test -fuzz`.
