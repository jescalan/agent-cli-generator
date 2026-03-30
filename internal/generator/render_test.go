package generator

import (
	"strings"
	"testing"
)

func TestBuildReleaseConfigUsesOverrideOrInfersFromModule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		moduleName  string
		repo        string
		homebrewTap string
		wantRepo    string
		wantTap     string
	}{
		{
			name:        "uses explicit override",
			moduleName:  "generated/example",
			repo:        "https://github.com/acme/example-cli",
			homebrewTap: "https://github.com/acme/homebrew-tap",
			wantRepo:    "acme/example-cli",
			wantTap:     "acme/homebrew-tap",
		},
		{
			name:       "infers from github module",
			moduleName: "github.com/acme/example-cli",
			wantRepo:   "acme/example-cli",
		},
		{
			name:       "non github module does not infer",
			moduleName: "generated/example",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			release := buildReleaseConfig(tc.moduleName, tc.repo, tc.homebrewTap)
			if release.Repo != tc.wantRepo {
				t.Fatalf("Repo = %q, want %q", release.Repo, tc.wantRepo)
			}
			if release.HomebrewTap != tc.wantTap {
				t.Fatalf("HomebrewTap = %q, want %q", release.HomebrewTap, tc.wantTap)
			}
			if (tc.wantRepo != "") != release.HasRepo {
				t.Fatalf("HasRepo = %v, want %v", release.HasRepo, tc.wantRepo != "")
			}
			if (tc.wantTap != "") != release.HasHomebrewTap {
				t.Fatalf("HasHomebrewTap = %v, want %v", release.HasHomebrewTap, tc.wantTap != "")
			}
		})
	}
}

func TestRenderSkillContainsAllSections(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		Name:              "example",
		Title:             "Example API",
		WhoAmIOperationID: "whoami.get",
		Env: EnvConfig{
			BaseURL:       "EXAMPLE_BASE_URL",
			HeadersJSON:   "EXAMPLE_HEADERS_JSON",
			OverridesJSON: "EXAMPLE_OVERRIDES_JSON",
		},
		Operations: []OperationManifest{
			{ID: "users.list", Summary: "List users", Tags: []string{"Users"}},
			{ID: "admin.reset", Summary: "Reset admin", Tags: []string{"Admin"}},
		},
	}
	skill := renderSkill(manifest, ReleaseConfig{
		Repo:             "acme/example-cli",
		HasRepo:          true,
		HomebrewTap:      "acme/homebrew-tap",
		HasHomebrewTap:   true,
		RepoOwner:        "acme",
		RepoName:         "example-cli",
		HomebrewTapOwner: "acme",
		HomebrewTapName:  "homebrew-tap",
	})

	for _, snippet := range []string{
		"name: example\n",
		"## Setup",
		"sh scripts/ensure-cli.sh",
		"curl -fsSL https://raw.githubusercontent.com/acme/example-cli/main/scripts/install.sh | sh",
		"brew install acme/homebrew-tap/example",
		"## Usage",
		"schema-first flow",
		"`example whoami`",
		"## Auth",
		"EXAMPLE_BASE_URL",
		"## Operations",
		"### Users",
		"`users.list`: List users",
		"### Admin",
		"`admin.reset`: Reset admin",
	} {
		if !strings.Contains(skill, snippet) {
			t.Fatalf("skill missing %q:\n%s", snippet, skill)
		}
	}
}

func TestBuildSkillConfigReturnsName(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		Name: "example",
		Operations: []OperationManifest{
			{Tags: []string{"Users"}},
			{Tags: []string{"Admin Tools"}},
			{},
		},
	}

	skills := buildSkillConfig(manifest)
	if skills.Name != "example" {
		t.Fatalf("Name = %q, want %q", skills.Name, "example")
	}
}

func TestShellQuote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: "''"},
		{input: "plain", want: "'plain'"},
		{input: "owner/repo", want: "'owner/repo'"},
		{input: "weird'quote", want: `'weird'"'"'quote'`},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			if got := shellQuote(tc.input); got != tc.want {
				t.Fatalf("shellQuote(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
