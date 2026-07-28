# Service-Level Objectives

The default production policy requires:

- availability at least 99.9%;
- error rate no more than 0.1%;
- p95 local control-plane latency no more than 250 ms;
- zero unresolved unknown outcomes at the release gate;
- backup recovery point no older than 300 seconds;
- verified restore completed within 900 seconds.

Evaluate a metrics snapshot with `scripts/slo-check.sh <metrics.json>`.
