# ADR-044: Credential-bearing provider egress is exact and DNS-pinned

Status: accepted

FutureDiff's built-in HTTP adapters use a daemon-owned transport that validates HTTPS scheme, exact hostname, port 443, allowed methods, and path prefixes. DNS answers are resolved before dialing and private, loopback, link-local, multicast, carrier-grade NAT, benchmark, and documentation ranges are rejected. Redirects and environment proxies are disabled.

This policy applies to GitHub API and Slack API traffic. Git smart-HTTP branch publication remains a separate exact-host subprocess boundary and must not be described as routed through the HTTP egress transport.
