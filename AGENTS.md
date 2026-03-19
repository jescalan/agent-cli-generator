# Agent Instructions

Use this file when contributing to `agent-cli-generator` with an AI agent.

This project is hardened by running real OpenAPI specs through the generator, finding where generation or runtime behavior breaks, and then turning those failures into code, tests, and documentation. The highest-value contributions usually come from that loop.

## Read First

Before making changes, read:

- [README.md](README.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)

## Main Contribution Loop

1. Start with a real OpenAPI spec.
2. Generate a CLI into a temporary output directory.
3. Build the generated project and inspect the emitted README, skills, and release scaffolding.
4. Exercise the generated CLI against the target API.
5. When a spec quirk or runtime mismatch appears, fix the generator, not just the sample output.
6. Add the smallest regression test that captures the new failure mode.
7. Re-run formatting, tests, vet, and race checks.

## What To Ask The Human For

When testing against a real service, ask for:

- the OpenAPI spec URL or local file path
- authentication docs if the spec is weak or incomplete
- disposable credentials when possible
- exact mutation boundaries
- app, org, project, or tenant identifiers when the API needs them
- cleanup expectations for anything you create

If the API is not disposable, prefer read-only coverage first.

## Safety Rules For Live APIs

- Do not delete or modify real user data unless the human explicitly allows it.
- Prefer throwaway accounts, apps, projects, tenants, messages, and records.
- For email or messaging APIs, use self-addressed or clearly synthetic test traffic when permitted.
- For calendars, contacts, billing, or production-like resources, pause before mutations unless the human has already approved a narrow plan.
- Clean up temporary resources when it is safe to do so.

## What Good Contributions Look Like

A solid contribution usually includes all of these:

- a generator or runtime fix in `internal/generator/`
- a regression test in `main_test.go`, `internal/generator/*_test.go`, or both
- a README or contributor-doc update when the new behavior changes workflow

## Local Checks

Run these before you stop:

```bash
go test ./...
go test -race ./...
go vet ./...
go test ./... -coverprofile=/tmp/agent-cli-generator.cover
go tool cover -func=/tmp/agent-cli-generator.cover
```

## Suggested Prompt For Humans

If a human wants to point another agent at this repo, they should provide:

1. this repository
2. [AGENTS.md](AGENTS.md)
3. [CONTRIBUTING.md](CONTRIBUTING.md)
4. the target OpenAPI spec URL or file
5. any credentials needed for live testing
6. explicit rules about what may or may not be mutated

That is usually enough context for an agent to contribute effectively.
