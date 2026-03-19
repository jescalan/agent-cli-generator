# Contributing

## Setup

- Install Go `1.25.1` or newer.
- Clone the repo and work from the project root.
- Read [README.md](README.md) for the product shape.
- Read [AGENTS.md](AGENTS.md) if you are contributing with an AI agent.

## Common commands

```bash
go test ./...
go test -race ./...
go vet ./...
go test ./... -coverprofile=/tmp/agent-cli-generator.cover
go tool cover -func=/tmp/agent-cli-generator.cover
```

## Project shape

- `main.go` is the small generator entrypoint.
- `internal/generator/spec.go` loads and sanitizes source specs.
- `internal/generator/normalize.go` rewrites real-world OpenAPI quirks into shapes `kin-openapi` can validate.
- `internal/generator/manifest.go` derives the operation manifest and auth metadata.
- `internal/generator/render.go` writes the generated project from templates.
- `internal/generator/templates/` contains the emitted runtime and generated project files.

## Testing guidance

- Prefer end-to-end generation tests when behavior crosses manifest, rendering, and runtime boundaries.
- Add unit tests when a normalization or manifest rule can be isolated cleanly.
- When a live API exposes a new spec quirk, add a small regression fixture that captures the shape without embedding secrets.

## Best Contribution Path

The highest-value work in this repo is hardening against real APIs.

The usual flow is:

1. Start with a public or private OpenAPI spec.
2. Generate a CLI into a temporary directory.
3. Build and exercise the generated CLI.
4. Use read-only operations first.
5. If a mutation is required, keep it narrow and safe.
6. Fix the generator when the spec or runtime breaks.
7. Add a regression test that captures the new failure mode.

## What Humans Should Give Agents

If you are asking an AI agent to contribute here, give it:

- this repo
- [README.md](README.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)
- [AGENTS.md](AGENTS.md)
- the target spec URL or file
- credentials for live testing, preferably disposable
- exact mutation limits

That is enough for most hardening passes.

## Live API Safety

- Prefer disposable tenants, projects, apps, and users.
- Do not touch real customer data without explicit approval.
- For email, messaging, or calendar APIs, default to read-only work until the human approves a narrow mutation plan.
- Clean up temporary resources when it is safe to do so.

## Scope

This project is optimized for agent-oriented JSON CLIs, not human-oriented shell ergonomics. Keep the surface area small and regular unless a change clearly improves reliability for agent callers.
