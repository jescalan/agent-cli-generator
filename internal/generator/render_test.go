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

func TestRenderInstallSkillMentionsPreferredInstallPaths(t *testing.T) {
	t.Parallel()

	manifest := Manifest{Name: "example"}
	skill := renderInstallSkill(manifest, ReleaseConfig{
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
		"curl -fsSL https://raw.githubusercontent.com/acme/example-cli/main/scripts/install.sh | sh",
		"brew install acme/homebrew-tap/example",
		"Run `example auth`",
		"call <operation-id-or-alias> --dry-run",
	} {
		if !strings.Contains(skill, snippet) {
			t.Fatalf("install skill missing %q:\n%s", snippet, skill)
		}
	}
}

func TestBuildSkillConfigIncludesCoreAndTagSkills(t *testing.T) {
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
	if skills.Install != "example-install" {
		t.Fatalf("Install = %q", skills.Install)
	}
	if skills.Shared != "example-shared" {
		t.Fatalf("Shared = %q", skills.Shared)
	}
	for _, want := range []string{
		"example-general",
		"example-users",
		"example-admin-tools",
	} {
		if !containsString(skills.Tags, want) {
			t.Fatalf("Tags missing %q: %#v", want, skills.Tags)
		}
		if !containsString(skills.All, want) {
			t.Fatalf("All missing %q: %#v", want, skills.All)
		}
	}
	for _, want := range []string{"example-install", "example-shared"} {
		if !containsString(skills.Core, want) {
			t.Fatalf("Core missing %q: %#v", want, skills.Core)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
