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
	for _, flag := range []string{`"--repo"`, `"--homebrew-tap"`} {
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
