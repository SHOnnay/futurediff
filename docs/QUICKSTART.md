# FutureDiff Quick Start

## 1. Install or build

Before the first public release, build from source:

```bash
git clone https://github.com/SHOnnay/futurediff.git
cd futurediff
make check
make build
```

Add `bin/` to your `PATH` or invoke the binaries by path.

## 2. Check the environment

```bash
fdif doctor
fdif demo --yes
```

## 3. Start a change

```bash
cd /path/to/your/repository
fdif start
```

Open the displayed safe working copy in your editor or coding agent.

## 4. Review

```bash
fdif status
fdif review --full
```

## 5. Publish locally

```bash
fdif finish
```

FutureDiff creates `futurediff/<transaction-id>` and leaves the current source branch unchanged.

## 6. Optional GitHub review

After configuring GitHub credentials and an allowlist:

```bash
fdif finish --github
```

The result is a draft pull request, not a merge.

## Next reading

- [Guided workflow](FDIF_GUIDED_CLI.md)
- [GitHub publication](FDIF_GITHUB_PUBLICATION.md)
- [Installation](FDIF_INSTALLATION.md)
- [Limitations](LIMITATIONS.md)
