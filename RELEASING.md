# Releasing agent-cli-generator

This repo includes release scaffolding for native binaries, checksums, and a one-line installer.

## What gets published

- Darwin binaries for `amd64` and `arm64`
- Linux binaries for `amd64` and `arm64`
- `checksums.txt`
- GitHub release assets built from `.goreleaser.yaml`
- `scripts/install.sh` for one-line installs
- Homebrew formula updates pushed to `jescalan/homebrew-tap`

## First-time setup

1. Push this repo to GitHub.
2. Create a tag like `v0.1.0` and push it.
3. GitHub Actions runs `.github/workflows/release.yml`.
4. Keep the `HOMEBREW_TAP_GITHUB_TOKEN` repository secret configured with push access to `jescalan/homebrew-tap`.

## Local release

```bash
go install github.com/goreleaser/goreleaser/v2@latest
goreleaser release --clean
```

## Install path for users

```bash
curl -fsSL https://raw.githubusercontent.com/jescalan/agent-cli-generator/main/scripts/install.sh | sh
```

```bash
brew install jescalan/tap/agent-cli-generator
```
