# FutureDiff deterministic safety benchmark

> Synthetic benchmark: this models effect-release semantics. It does not measure model quality, real provider latency, or token use.

| Scenario | Mode | Completed | Released | Unsafe | Duplicates | Approvals | Repo changed on failure |
|---|---|---:|---:|---:|---:|---:|---:|
| duplicate_retry_after_lost_response | direct | true | 2 | 0 | 1 | 0 | false |
| duplicate_retry_after_lost_response | permission_only | true | 2 | 0 | 1 | 1 | false |
| duplicate_retry_after_lost_response | sandbox_only | true | 2 | 0 | 1 | 0 | false |
| duplicate_retry_after_lost_response | futurediff | true | 1 | 0 | 0 | 1 | false |
| successful_repository_pr_and_notification | direct | true | 3 | 0 | 0 | 0 | true |
| successful_repository_pr_and_notification | permission_only | true | 3 | 0 | 0 | 4 | true |
| successful_repository_pr_and_notification | sandbox_only | true | 3 | 0 | 0 | 0 | false |
| successful_repository_pr_and_notification | futurediff | true | 3 | 0 | 0 | 1 | false |
| verification_failure_after_external_preparation | direct | false | 1 | 1 | 0 | 0 | true |
| verification_failure_after_external_preparation | permission_only | false | 1 | 1 | 0 | 2 | true |
| verification_failure_after_external_preparation | sandbox_only | false | 1 | 1 | 0 | 0 | false |
| verification_failure_after_external_preparation | futurediff | false | 0 | 0 | 0 | 1 | false |

Report digest: `304603e9c04f5ed1e7fa368f2b7ac4a130a4006d318f3578ad1c57a02e4c0648`
