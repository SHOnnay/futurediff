# FutureDiff Task 122 — Offline detached signatures

## Status

Complete.

## Delivered

OpenSSL-backed detached SHA-256 signatures can be created with a private key and verified with a public key. The tool reports only file and signature digests, never key material.

## Acceptance evidence

Tests generate a temporary RSA key pair, verify a valid signature, and reject a modified payload.
