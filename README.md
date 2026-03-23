# agent-cli-generator

Generate agent-first CLIs from OpenAPI specs.

Builds a regular, low-ambiguity interface for LLMs: canonical operation IDs, JSON in/out, schema introspection, local validation, dry-run planning, and a single skill that teaches the agent everything it needs.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/jescalan/agent-cli-generator/main/scripts/install.sh | sh
```

Or via Homebrew, Go, or source:

```bash
brew install jescalan/tap/agent-cli-generator   # Homebrew
go install github.com/jescalan/agent-cli-generator@latest  # Go
go build .                                       # source
```

## Quick start

### 1. Create a config file

```yaml
# agent-cli.yml
spec: ./openapi.yaml
publish: acme/myapi-cli
# repo: acme/myapi-cli           # legacy alias, still supported
```

That's the minimum. The generator infers `name` from the spec title, `module` from the publish repo, and defaults `output` to `./<repo-name>`. It also defaults `overwrite` to `true`, which makes regeneration easy while still refusing to overwrite directories it did not create.

Optional fields:

```yaml
name: myapi                        # override the binary name
module: github.com/acme/myapi-cli  # override the Go module path
output: ./out                      # generate into a different directory
homebrew_tap: acme/homebrew-tap    # enable Homebrew publishing
build: true                        # produce a native binary
overwrite: false                   # disable regeneration into the same output dir
```

### 2. Generate

```bash
agent-cli-generator generate
```

Or skip the config file and pass flags directly:

```bash
agent-cli-generator generate \
  --spec ./openapi.yaml \
  --output ./out/myapi-cli \
  --publish acme/myapi-cli \
  --build
```

### 3. Publish

Push the generated project to GitHub and tag a release. The included `.goreleaser.yaml` and GitHub Actions workflow handle cross-platform binaries, checksums, and optionally Homebrew.

### 4. Your users install one skill

```bash
npx skills add https://github.com/acme/myapi-cli
```

That single skill bootstraps the CLI binary, teaches the agent the schema-first workflow, lists auth requirements, and catalogs every operation. No further setup needed.

## Status

Usable today for JSON-first APIs. Exercised against real specs from Clerk, Tailscale, Supabase, Fly Machines, Resend, GitHub, Nylas, Customer.io, Kinde, and YNAB.

## How it works

The generated CLI exposes six commands:

```
operations                                       # list all operations
schema <id>                                      # inspect inputs, outputs, auth
example <id> --kind body|params|response         # concrete payload shape
call <id> --params '{...}' --body '{...}'        # make a request
auth                                             # show required env vars
spec                                             # dump the embedded OpenAPI doc
```

Params use location-aware JSON: `{"path": {}, "query": {}, "header": {}, "cookie": {}}`. Flat params work when a name is unambiguous.

Auth is environment-driven. OAuth2 `client_credentials` flows get native token acquisition. Everything else uses bearer tokens or API keys via generated env vars.

## Reference

### Generator flags

| Flag | Description |
|------|-------------|
| `--spec` | Path or URL to an OpenAPI 3.0/3.1 or Swagger 2.0 spec |
| `--output` | Output directory (default `./<repo-name>` when `publish` is set, otherwise `./generated`) |
| `--name` | Binary name (default: derived from spec title) |
| `--module` | Go module path (default: inferred from `--publish`) |
| `--publish` | GitHub `owner/name` where the generated CLI will be published |
| `--repo` | Deprecated alias for `--publish` |
| `--homebrew-tap` | Homebrew tap `owner/name` for formula publishing |
| `--build` | Run `go build` after generation |
| `--overwrite` | Allow writing into an existing directory (default `true` with config file) |

### Incomplete specs

Use `*_HEADERS_JSON` for undeclared headers (e.g. version pinning):

```bash
CLERK_HEADERS_JSON='{"Clerk-API-Version":"2025-11-10"}' clerk call GetUsersCount
```

Use `*_OVERRIDES_JSON` when the spec is missing auth or has conditional input rules:

```bash
export FLY_API_TOKEN=...
export FLYMACHINES_OVERRIDES_JSON='{"auth":{"headers":[{"name":"Authorization","env":"FLY_API_TOKEN","prefix":"Bearer ","required":true,"secret":true}]}}'
flymachines call apps.list --params '{"query":{"org_slug":"personal"}}'
```

The override layer also supports per-operation requirements for conditional params.

### Current limits

- JSON APIs are the best-tested path. Multipart, binary upload/download need more hardening.
- Native token acquisition covers OAuth2 `client_credentials` only. Other flows require pre-minted tokens.
- Pagination is heuristic — works when the spec exposes a common token-based contract.
- Swagger 2.0 inputs are converted to OpenAPI 3 before generation.
- The loader strips bad examples/defaults if they are the only validation failures.

## Development

```bash
go test ./...
go test -race ./...
go vet ./...
```

Most behavior lives under `internal/generator`. The test suite leans heavily on end-to-end tests that build and execute generated CLIs.

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md) for contributor and AI agent guidance.

## License

MIT. See [LICENSE](LICENSE).
