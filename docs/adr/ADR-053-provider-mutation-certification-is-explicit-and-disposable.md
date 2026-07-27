# ADR-053: Provider mutation certification is explicit and disposable

GitHub and Slack mutation certification requires an exact confirmation phrase, dedicated test resources, and cleanup. GitHub certification creates an unreachable test commit, a temporary `futurediff-cert/*` branch, and a draft PR, then closes/deletes them. Slack certification posts a marked test message and deletes it. Missing credentials produce BLOCKED, never PASS.
