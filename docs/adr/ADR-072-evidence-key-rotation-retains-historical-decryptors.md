# ADR-072: Evidence-key rotation retains historical decryptors

## Decision
An evidence keyring has one active write key and zero or more enabled historical decrypt keys.

## Rationale
Immediate key rotation must not require rewriting every historical artifact in one risky operation.

## Consequences
Old keys may be disabled explicitly, after which artifacts encrypted with them are intentionally unreadable until the key is restored.
