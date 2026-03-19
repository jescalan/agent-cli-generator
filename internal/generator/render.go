package generator

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

//go:embed templates/*
var templateFS embed.FS

type templateData struct {
	Manifest         Manifest
	ModuleName       string
	ExampleOperation string
	Release          ReleaseConfig
	Skills           SkillConfig
}

type ReleaseConfig struct {
	Repo             string
	RepoOwner        string
	RepoName         string
	HasRepo          bool
	HomebrewTap      string
	HomebrewTapOwner string
	HomebrewTapName  string
	HasHomebrewTap   bool
}

type SkillConfig struct {
	Install     string
	Shared      string
	Tags        []string
	Core        []string
	Recommended []string
	All         []string
}

func newTemplateData(manifest Manifest, moduleName string, opts Options) templateData {
	exampleOperation := "operation-id"
	if len(manifest.Operations) > 0 {
		exampleOperation = manifest.Operations[0].ID
	}

	release := buildReleaseConfig(moduleName, opts.Repo, opts.HomebrewTap)
	skills := buildSkillConfig(manifest)

	return templateData{
		Manifest:         manifest,
		ModuleName:       moduleName,
		ExampleOperation: exampleOperation,
		Release:          release,
		Skills:           skills,
	}
}

func renderTemplate(targetPath, templatePath string, data any) error {
	tmpl, err := template.ParseFS(templateFS, "templates/*.tmpl")
	if err != nil {
		return fmt.Errorf("parse templates: %w", err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, filepath.Base(templatePath), data); err != nil {
		return fmt.Errorf("execute template %s: %w", templatePath, err)
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("create parent directory for %s: %w", targetPath, err)
	}
	if err := os.WriteFile(targetPath, out.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", targetPath, err)
	}
	return nil
}

func buildReleaseConfig(moduleName, repoOverride, homebrewTap string) ReleaseConfig {
	repo := normalizeRepoSlug(repoOverride)
	if repo == "" {
		repo = inferGitHubRepoFromModule(moduleName)
	}

	release := ReleaseConfig{
		Repo:        repo,
		HasRepo:     repo != "",
		HomebrewTap: normalizeRepoSlug(homebrewTap),
	}
	if release.HasRepo {
		release.RepoOwner, release.RepoName = splitRepoSlug(repo)
	}
	if release.HomebrewTap != "" {
		release.HasHomebrewTap = true
		release.HomebrewTapOwner, release.HomebrewTapName = splitRepoSlug(release.HomebrewTap)
	}
	return release
}

func inferGitHubRepoFromModule(moduleName string) string {
	parts := strings.Split(strings.TrimSpace(moduleName), "/")
	if len(parts) < 3 || parts[0] != "github.com" {
		return ""
	}
	return parts[1] + "/" + parts[2]
}

func normalizeRepoSlug(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimPrefix(trimmed, "https://github.com/")
	trimmed = strings.TrimPrefix(trimmed, "http://github.com/")
	trimmed = strings.Trim(trimmed, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

func splitRepoSlug(value string) (string, string) {
	parts := strings.SplitN(value, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func buildSkillConfig(manifest Manifest) SkillConfig {
	installName := manifest.Name + "-install"
	sharedName := manifest.Name + "-shared"
	tagSet := map[string]struct{}{}
	for _, op := range manifest.Operations {
		if len(op.Tags) == 0 {
			tagSet["general"] = struct{}{}
			continue
		}
		for _, tag := range op.Tags {
			tagSet[sanitizeSlug(tag)] = struct{}{}
		}
	}

	var tags []string
	for tag := range tagSet {
		tags = append(tags, manifest.Name+"-"+tag)
	}
	sort.Strings(tags)

	core := []string{installName}
	recommended := []string{installName, sharedName}
	all := append(append([]string{}, recommended...), tags...)
	return SkillConfig{
		Install:     installName,
		Shared:      sharedName,
		Tags:        tags,
		Core:        core,
		Recommended: recommended,
		All:         all,
	}
}

func writeSkills(outputDir string, manifest Manifest, release ReleaseConfig) error {
	skillsDir := filepath.Join(outputDir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return fmt.Errorf("create skills directory: %w", err)
	}

	skillConfig := buildSkillConfig(manifest)

	installName := filepath.Join(skillsDir, skillConfig.Install, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(installName), 0o755); err != nil {
		return fmt.Errorf("create install skill directory: %w", err)
	}
	if err := os.WriteFile(installName, []byte(renderInstallSkill(manifest, release)), 0o644); err != nil {
		return fmt.Errorf("write install skill: %w", err)
	}
	installScriptName := filepath.Join(skillsDir, skillConfig.Install, "scripts", "ensure-cli.sh")
	if err := os.MkdirAll(filepath.Dir(installScriptName), 0o755); err != nil {
		return fmt.Errorf("create install skill scripts directory: %w", err)
	}
	if err := os.WriteFile(installScriptName, []byte(renderInstallSkillBootstrapScript(manifest, release)), 0o755); err != nil {
		return fmt.Errorf("write install skill bootstrap script: %w", err)
	}

	sharedName := filepath.Join(skillsDir, skillConfig.Shared, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(sharedName), 0o755); err != nil {
		return fmt.Errorf("create shared skill directory: %w", err)
	}
	if err := os.WriteFile(sharedName, []byte(renderSharedSkill(manifest)), 0o644); err != nil {
		return fmt.Errorf("write shared skill: %w", err)
	}

	tagGroups := map[string][]OperationManifest{}
	for _, op := range manifest.Operations {
		if len(op.Tags) == 0 {
			tagGroups["general"] = append(tagGroups["general"], op)
			continue
		}
		for _, tag := range op.Tags {
			tagGroups[sanitizeSlug(tag)] = append(tagGroups[sanitizeSlug(tag)], op)
		}
	}

	var tagKeys []string
	for tag := range tagGroups {
		tagKeys = append(tagKeys, tag)
	}
	sort.Strings(tagKeys)

	for _, tag := range tagKeys {
		path := filepath.Join(skillsDir, manifest.Name+"-"+tag, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create tag skill directory: %w", err)
		}
		if err := os.WriteFile(path, []byte(renderTagSkill(manifest, tag, tagGroups[tag])), 0o644); err != nil {
			return fmt.Errorf("write tag skill: %w", err)
		}
	}

	return nil
}

func renderInstallSkill(manifest Manifest, release ReleaseConfig) string {
	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString("name: " + manifest.Name + "-install\n")
	builder.WriteString("description: Primary entrypoint for installing and bootstrapping the generated " + manifest.Name + " CLI.\n")
	builder.WriteString("---\n\n")
	builder.WriteString("# " + manifest.Name + " Install Skill\n\n")
	builder.WriteString("Use this skill as the primary entrypoint for the `" + manifest.Name + "` CLI.\n\n")
	builder.WriteString("## First step\n\n")
	builder.WriteString("Always make sure the CLI exists before using it:\n\n")
	builder.WriteString("```bash\n")
	builder.WriteString("sh scripts/ensure-cli.sh\n")
	builder.WriteString("```\n\n")
	builder.WriteString("That script exits cleanly if `" + manifest.Name + "` is already on `PATH`. If it is missing, the script installs it from the published release.\n\n")
	builder.WriteString("## If bootstrap cannot install the CLI\n\n")
	if release.HasRepo {
		builder.WriteString("1. Use the published installer directly:\n\n")
		builder.WriteString("   ```bash\n")
		builder.WriteString("   curl -fsSL https://raw.githubusercontent.com/" + release.Repo + "/main/scripts/install.sh | sh\n")
		builder.WriteString("   ```\n\n")
		if release.HasHomebrewTap {
			builder.WriteString("2. On macOS, Homebrew is also available:\n\n")
			builder.WriteString("   ```bash\n")
			builder.WriteString("   brew install " + release.HomebrewTap + "/" + manifest.Name + "\n")
			builder.WriteString("   ```\n\n")
		}
		builder.WriteString("3. If release binaries are unavailable, build from source in the generated project root:\n\n")
	} else {
		builder.WriteString("1. If this project has been published, set `REPO=owner/name` and use the installer:\n\n")
		builder.WriteString("   ```bash\n")
		builder.WriteString("   REPO=owner/name curl -fsSL https://raw.githubusercontent.com/owner/name/main/scripts/install.sh | sh\n")
		builder.WriteString("   ```\n\n")
		builder.WriteString("2. Otherwise build from source in the generated project root:\n\n")
	}
	builder.WriteString("   ```bash\n")
	builder.WriteString("   go build .\n")
	builder.WriteString("   ```\n\n")
	builder.WriteString("## Use the CLI\n\n")
	builder.WriteString("After the CLI exists:\n\n")
	builder.WriteString("1. Run `" + manifest.Name + " auth` to discover required env vars.\n")
	builder.WriteString("2. Set the auth env vars.\n")
	builder.WriteString("3. Run `" + manifest.Name + " operations`.\n")
	builder.WriteString("4. Run `" + manifest.Name + " schema <operation-id-or-alias>` before calling anything.\n")
	builder.WriteString("5. Run `" + manifest.Name + " call <operation-id-or-alias> --dry-run` before mutating requests.\n")
	builder.WriteString("\nInstall the shared skill or tag skills if you want more detailed operation guidance.\n")
	return builder.String()
}

func renderInstallSkillBootstrapScript(manifest Manifest, release ReleaseConfig) string {
	var builder strings.Builder
	builder.WriteString("#!/usr/bin/env sh\n")
	builder.WriteString("set -eu\n\n")
	builder.WriteString("BINARY=\"" + manifest.Name + "\"\n")
	builder.WriteString("if command -v \"$BINARY\" >/dev/null 2>&1; then\n")
	builder.WriteString("  echo \"found ${BINARY} at $(command -v \"$BINARY\")\"\n")
	builder.WriteString("  exit 0\n")
	builder.WriteString("fi\n\n")
	if release.HasRepo {
		builder.WriteString("curl -fsSL https://raw.githubusercontent.com/" + release.Repo + "/main/scripts/install.sh | sh\n")
		builder.WriteString("exit 0\n")
	} else {
		builder.WriteString("echo \"" + manifest.Name + " is not on PATH and this project has not been published with a configured repo yet.\" >&2\n")
		builder.WriteString("echo \"Install it manually or publish the generated project with --repo owner/name.\" >&2\n")
		builder.WriteString("exit 1\n")
	}
	return builder.String()
}

func renderSharedSkill(manifest Manifest) string {
	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString("name: " + manifest.Name + "-shared\n")
	builder.WriteString("description: Shared guidance for the generated " + manifest.Name + " CLI.\n")
	builder.WriteString("---\n\n")
	builder.WriteString("# " + manifest.Name + " Shared Skill\n\n")
	builder.WriteString("Use this CLI in a strict, schema-first flow:\n\n")
	builder.WriteString("1. Run `" + manifest.Name + " operations` to find the exact operation ID or alias.\n")
	builder.WriteString("2. Run `" + manifest.Name + " schema <operation-id-or-alias>` to inspect inputs, outputs, and auth.\n")
	builder.WriteString("3. Run `" + manifest.Name + " example <operation-id-or-alias> --kind body|params|response` to get a concrete payload shape.\n")
	builder.WriteString("4. Run `" + manifest.Name + " call <operation-id-or-alias> --dry-run` before any mutating request.\n")
	builder.WriteString("5. Run `" + manifest.Name + " call <operation-id-or-alias>` without `--dry-run` only after the request is valid.\n\n")
	builder.WriteString("## Rules\n\n")
	builder.WriteString("- Always send request inputs as JSON strings via `--params` and `--body`.\n")
	builder.WriteString("- Prefer location-aware params: `{\"path\": {...}, \"query\": {...}, \"header\": {...}, \"cookie\": {...}}`.\n")
	builder.WriteString("- Prefer `schema` and `example` over guessing payload shapes.\n")
	builder.WriteString("- Treat all outputs as machine-readable JSON.\n")
	builder.WriteString("- Use `" + manifest.Env.BaseURL + "` to override the API base URL when you want to bypass the spec's server defaults.\n")
	for _, serverVar := range manifest.ServerVars {
		line := "- Use `" + serverVar.EnvVar + "` to set the `" + serverVar.Name + "` server variable"
		if serverVar.Default != "" {
			line += " (default: `" + serverVar.Default + "`)"
		}
		if serverVar.Description != "" {
			line += ". " + serverVar.Description
		}
		builder.WriteString(line + ".\n")
	}
	builder.WriteString("- Use `" + manifest.Env.HeadersJSON + "` for version headers or undeclared non-auth headers, for example `{\"Clerk-API-Version\":\"2025-11-10\"}` or `{\"apikey\":\"...\"}`.\n")
	builder.WriteString("- Use `" + manifest.Env.OverridesJSON + "` for undeclared auth or live endpoint rules that the spec does not express.\n")
	for _, scheme := range manifest.Auth {
		builder.WriteString("- Use `" + scheme.EnvVar + "` for " + scheme.Description + ".\n")
		if scheme.ClientCredentials != nil {
			line := "- Or mint `" + scheme.Name + "` with OAuth2 client credentials via `" + scheme.ClientCredentials.ClientIDEnv + "`, `" + scheme.ClientCredentials.ClientSecretEnv + "`, and `" + scheme.ClientCredentials.TokenURLEnv + "`"
			if scheme.ClientCredentials.TokenURL != "" {
				line += " (default token URL: `" + scheme.ClientCredentials.TokenURL + "`)"
			}
			line += "."
			builder.WriteString(line + "\n")
			builder.WriteString("- Use `" + scheme.ClientCredentials.AudienceEnv + "` for an optional audience and `" + scheme.ClientCredentials.ScopesEnv + "` for optional extra scopes.\n")
		}
	}
	return builder.String()
}

func renderTagSkill(manifest Manifest, tag string, operations []OperationManifest) string {
	sort.Slice(operations, func(i, j int) bool {
		return operations[i].ID < operations[j].ID
	})

	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString("name: " + manifest.Name + "-" + tag + "\n")
	builder.WriteString("description: Operations for the " + tag + " area of " + manifest.Name + ".\n")
	builder.WriteString("---\n\n")
	builder.WriteString("# " + strings.ToUpper(tag[:1]) + tag[1:] + "\n\n")
	builder.WriteString("Relevant operation IDs:\n\n")
	for _, op := range operations {
		line := "- `" + op.ID + "`: " + op.Summary
		if line == "- `"+op.ID+"`: " {
			line = "- `" + op.ID + "`"
		}
		if len(op.Aliases) > 0 {
			line += " (aliases: `" + strings.Join(op.Aliases, "`, `") + "`)"
		}
		builder.WriteString(line + "\n")
	}
	builder.WriteString("\nUse `" + manifest.Name + " schema <operation-id-or-alias>` before calling any of these.\n")
	return builder.String()
}
