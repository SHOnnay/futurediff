# Post-Deployment Health and Rollback

Promotion approval authorizes deployment of one exact release digest. It does not prove that the deployment is healthy. FutureDiff therefore requires a separate observation result.

## Post-deployment health

The default policy requires a minimum 15-minute observation window, non-synthetic evidence, required subsystem health checks, availability and latency targets, zero unknown outcomes, and zero unreconciled effects.

## Rollback decision

Rollback readiness is evaluated independently from current health. Required evidence includes a verified backup digest, rollback-plan digest, recent successful non-synthetic drill, and tested RPO/RTO values. Current metrics are checked against automatic rollback triggers.

A result can be rollback-ready while still deciding `rollback` because live trigger thresholds were exceeded. Production launch requires both readiness and a `continue` decision.
