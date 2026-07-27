# ADR-049: Installation is a declarative plan

FutureDiff installation first produces a JSON plan containing every binary and service-file write. The same plan is then applied atomically. This supports review, dry-run automation, and future package-manager integrations. Provider credentials are never enabled automatically.
