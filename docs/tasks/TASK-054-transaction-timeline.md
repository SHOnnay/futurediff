# Task 054 — Transaction timeline export

## Objective

Provide a human-readable, payload-minimized chronology of a FutureDiff transaction for review and incident response.

## Implemented

- `futurediff-timeline` command.
- JSON, Markdown, and Mermaid flowchart formats.
- Sequence, UTC timestamp, event type, effect identity, safe summary, and event hash.
- Deterministic timeline digest.
- Events sorted by durable ledger sequence.
- Provider message bodies, patch bodies, and event payload JSON are excluded.

## Example

```bash
futurediff-timeline --root ~/.futurediff \
  --transaction tx_123 --format markdown --output timeline.md

futurediff-timeline --root ~/.futurediff \
  --transaction tx_123 --format mermaid
```

## Limitation

The timeline is a durable sequence view, not a globally synchronized distributed trace. It describes recorded FutureDiff events and does not prove when an external human or provider observed an effect beyond the stored receipt timestamps.

## Validation

- Timeline generated from an actual ledger transaction.
- JSON digest is populated.
- Markdown and Mermaid renderers produce stable output.
