# Capacity and Soak Assurance

Capacity and soak evidence is evaluated against fail-closed policies in `config/capacity-policy.json` and `config/soak-policy.json`.

Capacity checks cover duration, requests, concurrency, throughput, latency, error rate, CPU, memory, and unknown outcomes. Soak checks cover duration, transaction count, error rate, memory growth, file-descriptor growth, queue lag, and unknown outcomes.

Example evidence is explicitly synthetic local conformance data. Replace it with measured production-like evidence before external certification.
