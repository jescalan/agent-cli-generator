# agent-cli-generator

Generate agent-first CLIs from OpenAPI specs.

This project does not try to build a pleasant human CLI. It builds a regular, low-ambiguity interface for LLMs and other agents:

- canonical operation IDs, with deterministic aliases when the spec supports them
- JSON in and JSON out
- location-aware params for path/query/header/cookie separation
- schema introspection from the embedded spec
- local request validation before send
- local auth validation before send
- native bearer-token acquisition for OAuth2 `client_credentials` flows
- dry-run request planning
- environment-driven auth
- generated `SKILL.md` files alongside the CLI

## Status

This project is usable today for JSON-first APIs and has been exercised against real OpenAPI specs from Clerk, Tailscale, Supabase, Fly Machines, Resend, GitHub, Nylas, Customer.io, Kinde, and YNAB.

The best-tested path today is:

- JSON request and response bodies
- standard header, query, cookie, and bearer auth
- OAuth2 `client_credentials`
- standalone GitHub-distributed generated CLIs with release binaries and skills

## Why this shape

Human CLIs optimize for memorability and hand typing. Agents do not need that. Agents need:

- a small, regular grammar
- stable identifiers
- exact schemas
- concrete examples
- machine-readable errors

The generated CLI exposes six commands:

- `operations`
- `schema <operation-id-or-alias>`
- `example <operation-id-or-alias> --kind body|params|response`
- `call <operation-id-or-alias> --params '{...}' --body '{...}'`
- `auth`
- `spec`

That is the whole surface area.

For request params, the preferred shape is:

```json
{
  "path": {},
  "query": {},
  "header": {},
  "cookie": {}
}
```

Flat params still work when a parameter name is unique. If the same name exists in multiple locations, the CLI rejects flat input and forces disambiguation.

Canonical operation IDs remain the source of truth. Aliases are additive: they help models discover or recall the right operation name, but the CLI still reports and stores the canonical ID.

## Install

Recommended direct install:

```bash
curl -fsSL https://raw.githubusercontent.com/jescalan/agent-cli-generator/main/scripts/install.sh | sh
```

Homebrew, after the first tagged release:

```bash
brew install jescalan/tap/agent-cli-generator
```

Or install from Go:

```bash
go install github.com/jescalan/agent-cli-generator@latest
```

Or build from source:

```bash
go build .
```

This repo also includes `.goreleaser.yaml`, `.github/workflows/release.yml`, and [RELEASING.md](RELEASING.md), so the generator itself ships the same release shape it generates for other APIs.

## Quick start

Install the generator, then generate a standalone CLI project:

```bash
agent-cli-generator generate \
  --spec https://example.com/openapi.yaml \
  --output ./out/my-api-cli \
  --name myapi \
  --repo acme/myapi-cli \
  --homebrew-tap acme/homebrew-tap \
  --build
```

That command writes a standalone project into `./out/my-api-cli`. If you pass `--build`, it also produces a native binary at `./out/my-api-cli/bin/myapi`.

Typical next steps:

1. Read the generated `README.md`.
2. Run the generated CLI's `auth`, `operations`, and `schema` commands.
3. Publish the generated project in its own GitHub repo so users can install its binary and skills.

When contributing locally from a checkout, the equivalent development command is:

```bash
go run . generate ...
```

## Generator usage

Once installed, the generator interface is:

```bash
agent-cli-generator generate --spec <path-or-url> --output <dir> [flags]
```

Flags:

- `--spec`: local path, `file://` URL, or `http(s)://` URL for an OpenAPI 3.0, practical 3.1, or Swagger 2.0 spec in JSON or YAML
- `--output`: directory for the generated project
- `--name`: binary name for the generated CLI
- `--module`: optional Go module name for the generated project
- `--repo`: optional GitHub repository in `owner/name` form for generated release/install scaffolding. If omitted, the generator infers it from `--module` when the module path is a GitHub repo.
- `--homebrew-tap`: optional Homebrew tap repository in `owner/name` form. When set, the generated GoReleaser config publishes a Homebrew formula.
- `--build`: run `go mod tidy` and `go build` in the generated project
- `--overwrite`: allow writing into a non-empty output directory

## Generated project

The output is a standalone Go project for the target API, not a dependency on this repo.

It includes:

- an embedded normalized OpenAPI document
- an operation manifest
- a runtime that validates params and request bodies with the schema
- a README for the generated CLI
- a `skills/` directory with shared and tag-level skills
- release scaffolding: `.goreleaser.yaml`, `scripts/install.sh`, `scripts/install-skills.sh`, `RELEASING.md`, and a GitHub Actions release workflow
- an install/bootstrap skill for agents alongside the operation skills

If you pass `--build`, the generated project also produces a single native binary at `bin/<name>`.

Remote specs work directly, including relative remote `$ref`s. If the source spec lives at a public URL, the generator can ingest it without a separate download step.

For bearer-style APIs, the generated CLI can now work in two modes:

- direct token mode via the generated `*_BEARER_TOKEN` env var
- token acquisition mode via generated `*_TOKEN_URL`, `*_CLIENT_ID`, `*_CLIENT_SECRET`, optional `*_AUDIENCE`, and optional `*_SCOPES`

If the OpenAPI spec declares an OAuth2 `clientCredentials` flow, the token URL comes from the spec automatically. If it does not, the generated CLI still exposes the same env contract so you can configure token acquisition manually for bearer-only specs such as Kinde.

## Publishing a generated CLI

If you are adding this to your own API project, the intended path is:

1. Generate a standalone CLI project from your OpenAPI spec.
2. Put that generated project in its own repository, or a clearly separated subdirectory with its own release process.
3. Pass `--repo owner/name` so the generated install script and README point at the final GitHub repository.
4. Pass `--homebrew-tap owner/homebrew-tap` if you want Homebrew publishing.
5. Tag releases in the generated project. The included release workflow and `.goreleaser.yaml` publish native binaries, checksums, and optionally a Homebrew formula.

That makes the generated CLI the stable distribution artifact for your API.

If you want the generated skills to be easy to install for agent users, publish the generated repo on GitHub and keep the generated `skills/` directory in the repository. The generated project includes `scripts/install-skills.sh` and README instructions for that flow.

## What your users should do

The message to your users should be simple:

1. Install the generated install skill with `scripts/install-skills.sh`, or manually with `npx skills add https://github.com/owner/repo --skill <cli>-install`.
2. Let that install skill ensure the CLI binary exists. It should install the binary if it is missing and reuse it if it is already on `PATH`.
3. Run `<cli> auth` to see required env vars.
4. Use `<cli> operations`, `<cli> schema`, and `<cli> example` before making calls.
5. Use `<cli> call --dry-run` before mutating requests.

Your users should not need to read the raw OpenAPI spec. The generated CLI and skills should become the agent-facing contract.

If you want to hand your own users a copy-paste onboarding path, the generated project already contains it:

- the generated `README.md`
- `scripts/install.sh`
- `scripts/install-skills.sh`
- `RELEASING.md`
- `skills/<cli>-install/SKILL.md`
- `skills/<cli>-install/scripts/ensure-cli.sh`
- `skills/<cli>-shared/SKILL.md`

## Contributing With AI Agents

If you want an AI agent to contribute to this repo, point it at:

- [README.md](README.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)
- [AGENTS.md](AGENTS.md)

Then give it:

- the OpenAPI spec URL or file
- any auth docs or live API quirks the spec does not describe
- credentials, ideally for a disposable account
- a clear statement of what it may mutate

The most useful contribution pattern is still spec hardening:

1. generate a CLI from a real API
2. run it against the live service
3. fix generator or runtime issues exposed by that API
4. add regression coverage so the same class of spec never breaks again

## Development

Useful local checks:

```bash
go test ./...
go test -race ./...
go vet ./...
go test ./... -coverprofile=/tmp/agent-cli-generator.cover
go tool cover -func=/tmp/agent-cli-generator.cover
```

The root CLI is intentionally small. Most behavior lives under `internal/generator`, and the test suite leans heavily on end-to-end generation tests that build and execute generated CLIs.

## When specs are incomplete

Use the generated `*_HEADERS_JSON` env var for plain extra headers such as version pinning or undeclared non-auth headers.

For example, Clerk accepts `Clerk-API-Version`:

```bash
CLERK_BEARER_TOKEN=sk_test_... \
CLERK_HEADERS_JSON='{"Clerk-API-Version":"2025-11-10"}' \
/tmp/generated-clerk-cli/bin/clerk call GetUsersCount
```

This is still useful when the generated CLI was built from the right spec but the target instance defaults to an older API version.

The same path is useful when a live API requires undeclared headers. Supabase’s PostgREST introspection route is one example: it requires `apikey`, but its generated schema does not advertise a security scheme.

Use the generated `*_OVERRIDES_JSON` env var when the machine-readable spec is missing auth metadata or when a live endpoint has conditional input rules the spec does not express.

For example, Fly Machines requires bearer auth even though its public spec does not declare a security scheme:

```bash
export FLY_API_TOKEN=...
export FLYMACHINES_OVERRIDES_JSON='{"auth":{"headers":[{"name":"Authorization","env":"FLY_API_TOKEN","prefix":"Bearer ","required":true,"secret":true}]}}'
/tmp/generated-fly-cli/bin/flymachines call apps.list --params '{"query":{"org_slug":"personal"}}'
```

The same override layer can enforce live endpoint preconditions before the network call. Fly’s `Machines_wait` endpoint is one example:

```bash
export FLYMACHINES_OVERRIDES_JSON='{"operations":{"Machines_wait":{"requirements":[{"when":[{"location":"query","name":"state","oneOf":["stopped","destroyed"]}],"require":[{"location":"query","name":"instance_id"}],"message":"query.instance_id is required when query.state is stopped or destroyed"}]}}}'
```

Some APIs also expose operation scope requirements outside the standard OAuth security block. The generator carries those through into the operation manifest and generated schema output, and the runtime preflights JWT-backed bearer tokens when it can decode their granted scopes.

## Current limits

- The loader normalizes common OpenAPI 3.1 features that `kin-openapi` still handles unevenly, including numeric `exclusiveMinimum`, numeric `exclusiveMaximum`, `["type", "null"]` unions, schema-level `examples`, array unions without top-level `items`, and empty top-level `webhooks`.
- Swagger 2.0 inputs are converted to OpenAPI 3 before manifest generation and runtime embedding.
- If a spec only fails validation because of bad examples or bad defaults, the loader strips those fields and keeps the validated structure.
- HTTP basic auth env vars accept either raw `username:password` or pre-encoded `base64(username:password)`.
- Native token acquisition currently covers OAuth2 `client_credentials`. Authorization-code, PKCE, and device-flow APIs still require pre-minted tokens or future auth-flow support.
- JSON APIs are the best-tested path today. Multipart, binary upload/download, and non-JSON request bodies need more hardening.
- Pagination is heuristic. When the spec exposes a common token-based contract, the generated CLI enables `--page-all`.
- Validation errors are structured for agents, not for terminal users.

## License

MIT. See [LICENSE](LICENSE).
