package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunNoArgsPrintsUsage(t *testing.T) {
	output := captureStream(t, &os.Stdout, func() {
		if err := run(nil); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})

	if !strings.Contains(output, `"name": "agent-cli-generator"`) {
		t.Fatalf("usage output did not include CLI name: %s", output)
	}
	if !strings.Contains(output, `"generate"`) {
		t.Fatalf("usage output did not include generate command: %s", output)
	}
	for _, flag := range []string{`"--publish"`, `"--homebrew-tap"`} {
		if !strings.Contains(output, flag) {
			t.Fatalf("usage output did not include %s: %s", flag, output)
		}
	}
}

func TestRunUnknownCommandReturnsError(t *testing.T) {
	err := run([]string{"wat"})
	if err == nil {
		t.Fatal("expected unknown command error")
	}
	if !strings.Contains(err.Error(), `unknown command "wat"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGenerateRequiresFlags(t *testing.T) {
	if err := runGenerate(nil); err == nil || !strings.Contains(err.Error(), "missing required --spec") {
		t.Fatalf("expected missing --spec error, got: %v", err)
	}
	if err := runGenerate([]string{"--spec", "openapi.json"}); err == nil || !strings.Contains(err.Error(), "missing required --output") {
		t.Fatalf("expected missing --output error, got: %v", err)
	}
}

func TestRunGenerateWritesProject(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Example API", "version": "1.0.0" },
	  "paths": {
	    "/ping": {
	      "get": {
	        "operationId": "ping.get",
	        "responses": {
	          "200": { "description": "ok" }
	        }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := runGenerate([]string{"--spec", specPath, "--output", outputDir}); err != nil {
		t.Fatalf("runGenerate returned error: %v", err)
	}

	for _, path := range []string{
		filepath.Join(outputDir, "main.go"),
		filepath.Join(outputDir, "runtime.go"),
		filepath.Join(outputDir, "manifest.json"),
		filepath.Join(outputDir, "README.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated file %s: %v", path, err)
		}
	}
}

func TestConfigFileProvideDefaults(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Config API", "version": "1.0.0" },
	  "paths": {
	    "/ping": {
	      "get": {
	        "operationId": "ping.get",
	        "responses": { "200": { "description": "ok" } }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	configDir := t.TempDir()
	configContent := "spec: " + specPath + "\noutput: " + outputDir + "\nname: configapi\n"
	if err := os.WriteFile(filepath.Join(configDir, "agent-cli.yml"), []byte(configContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(configDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	if err := runGenerate(nil); err != nil {
		t.Fatalf("runGenerate with config file returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outputDir, "main.go")); err != nil {
		t.Fatalf("expected generated file main.go: %v", err)
	}
}

func TestMinimalConfigFileSpecAndRepo(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Minimal API", "version": "1.0.0" },
	  "paths": {
	    "/ping": {
	      "get": {
	        "operationId": "ping.get",
	        "responses": { "200": { "description": "ok" } }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	configDir := t.TempDir()
	configContent := "spec: " + specPath + "\npublish: acme/minimal-api\n"
	if err := os.WriteFile(filepath.Join(configDir, "agent-cli.yml"), []byte(configContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(configDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	if err := runGenerate(nil); err != nil {
		t.Fatalf("runGenerate with minimal config returned error: %v", err)
	}

	outputDir := filepath.Join(configDir, "minimal-api")
	if _, err := os.Stat(filepath.Join(outputDir, "main.go")); err != nil {
		t.Fatalf("expected generated file main.go: %v", err)
	}

	// The module should have been inferred from the repo.
	goModBytes, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if !strings.Contains(string(goModBytes), "github.com/acme/minimal-api") {
		t.Fatalf("expected module inferred from repo, got: %s", string(goModBytes))
	}

	// Regeneration should also work (overwrite defaults to true with config).
	if err := runGenerate(nil); err != nil {
		t.Fatalf("regeneration with config file returned error: %v", err)
	}
}

func TestConfigFileLegacyRepoKeyStillWorks(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Legacy Repo API", "version": "1.0.0" },
	  "paths": {
	    "/ping": {
	      "get": {
	        "operationId": "ping.get",
	        "responses": { "200": { "description": "ok" } }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	configDir := t.TempDir()
	configContent := "spec: " + specPath + "\nrepo: acme/legacy-repo-api\n"
	if err := os.WriteFile(filepath.Join(configDir, "agent-cli.yml"), []byte(configContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(configDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	if err := runGenerate(nil); err != nil {
		t.Fatalf("runGenerate with legacy repo config returned error: %v", err)
	}

	outputDir := filepath.Join(configDir, "legacy-repo-api")
	goModBytes, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if !strings.Contains(string(goModBytes), "github.com/acme/legacy-repo-api") {
		t.Fatalf("expected module inferred from legacy repo key, got: %s", string(goModBytes))
	}
}

func TestConfigFileRespectsExplicitOverwriteFalse(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Overwrite API", "version": "1.0.0" },
	  "paths": {
	    "/ping": {
	      "get": {
	        "operationId": "ping.get",
	        "responses": { "200": { "description": "ok" } }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	configDir := t.TempDir()
	configContent := "spec: " + specPath + "\npublish: acme/overwrite-api\noverwrite: false\n"
	if err := os.WriteFile(filepath.Join(configDir, "agent-cli.yml"), []byte(configContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(configDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	if err := runGenerate(nil); err != nil {
		t.Fatalf("first generation with config file returned error: %v", err)
	}

	err := runGenerate(nil)
	if err == nil || !strings.Contains(err.Error(), "output directory is not empty") {
		t.Fatalf("expected overwrite=false to block regeneration, got: %v", err)
	}
}

func TestConfigFileFlagOverride(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Override API", "version": "1.0.0" },
	  "paths": {
	    "/ping": {
	      "get": {
	        "operationId": "ping.get",
	        "responses": { "200": { "description": "ok" } }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	configDir := t.TempDir()
	configContent := "spec: /nonexistent/spec.json\noutput: /nonexistent/out\nname: wrongname\n"
	if err := os.WriteFile(filepath.Join(configDir, "agent-cli.yml"), []byte(configContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(configDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	if err := runGenerate([]string{"--spec", specPath, "--output", outputDir, "--name", "overrideapi"}); err != nil {
		t.Fatalf("runGenerate with flag override returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outputDir, "main.go")); err != nil {
		t.Fatalf("expected generated file main.go: %v", err)
	}
}

func TestLegacyRepoFlagStillWorks(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Legacy Flag API", "version": "1.0.0" },
	  "paths": {
	    "/ping": {
	      "get": {
	        "operationId": "ping.get",
	        "responses": { "200": { "description": "ok" } }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := runGenerate([]string{"--spec", specPath, "--output", outputDir, "--repo", "acme/legacy-flag-api"}); err != nil {
		t.Fatalf("runGenerate with legacy repo flag returned error: %v", err)
	}

	goModBytes, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if !strings.Contains(string(goModBytes), "github.com/acme/legacy-flag-api") {
		t.Fatalf("expected module inferred from legacy repo flag, got: %s", string(goModBytes))
	}
}

func TestMissingConfigFileIsNotAnError(t *testing.T) {
	configDir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(configDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	err := runGenerate(nil)
	if err == nil || !strings.Contains(err.Error(), "missing required --spec") {
		t.Fatalf("expected missing --spec error when no config file, got: %v", err)
	}
}

func TestConfigFileMissingRequiredFieldsErrors(t *testing.T) {
	configDir := t.TempDir()
	configContent := "name: partial\n"
	if err := os.WriteFile(filepath.Join(configDir, "agent-cli.yml"), []byte(configContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(configDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	err := runGenerate(nil)
	if err == nil || !strings.Contains(err.Error(), "missing required --spec") {
		t.Fatalf("expected missing --spec error with partial config, got: %v", err)
	}
}

func TestMalformedConfigFileReturnsParseError(t *testing.T) {
	configDir := t.TempDir()
	configContent := "spec: [\n"
	if err := os.WriteFile(filepath.Join(configDir, "agent-cli.yml"), []byte(configContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(configDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	err := runGenerate(nil)
	if err == nil || !strings.Contains(err.Error(), "parse agent-cli.yml") {
		t.Fatalf("expected parse error for malformed config, got: %v", err)
	}
}

func TestUnreadableConfigPathReturnsReadError(t *testing.T) {
	configDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(configDir, "agent-cli.yml"), 0o755); err != nil {
		t.Fatalf("mkdir config path: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(configDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	err := runGenerate(nil)
	if err == nil || !strings.Contains(err.Error(), "read agent-cli.yml") {
		t.Fatalf("expected read error for invalid config path, got: %v", err)
	}
}

func TestPrintErrorWritesStructuredJSON(t *testing.T) {
	output := captureStream(t, &os.Stderr, func() {
		printError(errors.New("boom"))
	})

	if !strings.Contains(output, `"message": "boom"`) {
		t.Fatalf("error output did not include message: %s", output)
	}
}

func captureStream(t *testing.T, stream **os.File, fn func()) string {
	t.Helper()

	original := *stream
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	*stream = writer
	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		done <- string(data)
	}()

	fn()

	_ = writer.Close()
	*stream = original
	output := <-done
	_ = reader.Close()
	return output
}
