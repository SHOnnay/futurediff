# Task 077 — Signed integrity checkpoint

## Goal
Bind a consistent ledger backup and the current tamper-evident chain heads to an operator signature.

## Implemented

`futurediff-integrity-checkpoint` creates an Ed25519-signed checkpoint containing:

- consistent SQLite backup filename, SHA-256 and size;
- ledger health counts;
- aggregate per-transaction event-chain heads;
- mutation API audit-chain head;
- optional signed operator-receipt chain head and count;
- checkpoint material digest, operator key identity and signature.

Creation requires the daemon to be stopped and uses the exclusive daemon lock. Verification checks the signature, backup bytes, SQLite semantic audit, event-chain aggregate, API audit head, and optional receipt chain.

## Security boundary

This is a locally signed integrity checkpoint. It does not provide trusted time or external transparency anchoring unless the checkpoint is later published to an independent system.

## Validation

A checkpoint round trip passed. Appending one byte to a copied ledger backup caused verification to fail.
