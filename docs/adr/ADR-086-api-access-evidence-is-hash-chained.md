# ADR-086 — API access evidence is hash-chained

Payload-free API access rows are linked by canonical SHA-256 digests. This detects ordinary row modification, deletion, insertion, and reordering. The local chain is not described as tamper-proof because it is not externally signed or anchored.
