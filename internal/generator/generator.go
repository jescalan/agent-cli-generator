package generator

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

const (
	generatedMarkerFile = ".agent-cli-generator.json"
	generatorVersion    = "dev"
)

type Options struct {
	SpecPath    string
	OutputDir   string
	Name        string
	ModuleName  string
	Repo        string
	HomebrewTap string
	Build       bool
	Overwrite   bool
}

func Generate(opts Options) error {
	doc, err := loadSpec(opts.SpecPath)
	if err != nil {
		return err
	}

	binaryName := opts.Name
	if binaryName == "" {
		binaryName = deriveBinaryName(doc, opts.SpecPath)
	}

	moduleName := opts.ModuleName
	if moduleName == "" {
		moduleName = "generated/" + binaryName
	}

	manifest, err := BuildManifest(doc, binaryName)
	if err != nil {
		return err
	}

	if err := ensureOutputDir(opts.OutputDir, opts.Overwrite); err != nil {
		return err
	}

	renderData := newTemplateData(manifest, moduleName, opts)

	if err := renderTemplate(filepath.Join(opts.OutputDir, "go.mod"), "templates/go.mod.tmpl", renderData); err != nil {
		return err
	}
	if err := renderTemplate(filepath.Join(opts.OutputDir, "main.go"), "templates/main.go.tmpl", renderData); err != nil {
		return err
	}
	if err := renderTemplate(filepath.Join(opts.OutputDir, "runtime.go"), "templates/runtime.go.tmpl", renderData); err != nil {
		return err
	}
	if err := renderTemplate(filepath.Join(opts.OutputDir, "README.md"), "templates/README.md.tmpl", renderData); err != nil {
		return err
	}
	if err := renderTemplate(filepath.Join(opts.OutputDir, ".goreleaser.yaml"), "templates/goreleaser.yaml.tmpl", renderData); err != nil {
		return err
	}
	if err := renderTemplate(filepath.Join(opts.OutputDir, "RELEASING.md"), "templates/RELEASING.md.tmpl", renderData); err != nil {
		return err
	}
	if err := renderTemplate(filepath.Join(opts.OutputDir, "scripts", "install.sh"), "templates/install.sh.tmpl", renderData); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Join(opts.OutputDir, "scripts", "install.sh"), 0o755); err != nil {
		return fmt.Errorf("mark install script executable: %w", err)
	}
	if err := renderTemplate(filepath.Join(opts.OutputDir, "scripts", "install-skills.sh"), "templates/install-skills.sh.tmpl", renderData); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Join(opts.OutputDir, "scripts", "install-skills.sh"), 0o755); err != nil {
		return fmt.Errorf("mark skill install script executable: %w", err)
	}
	if err := renderTemplate(filepath.Join(opts.OutputDir, ".github", "workflows", "release.yml"), "templates/release.yml.tmpl", renderData); err != nil {
		return err
	}

	normalizedSpec, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal normalized spec: %w", err)
	}
	if err := os.WriteFile(filepath.Join(opts.OutputDir, "openapi.json"), normalizedSpec, 0o644); err != nil {
		return fmt.Errorf("write openapi.json: %w", err)
	}

	manifest.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(opts.OutputDir, "manifest.json"), manifestBytes, 0o644); err != nil {
		return fmt.Errorf("write manifest.json: %w", err)
	}

	if err := os.WriteFile(filepath.Join(opts.OutputDir, ".gitignore"), []byte("bin/\ndist/\n"), 0o644); err != nil {
		return fmt.Errorf("write .gitignore: %w", err)
	}
	if err := writeGeneratedMarker(opts.OutputDir, manifest); err != nil {
		return err
	}
	if err := writeSkills(opts.OutputDir, manifest, renderData.Release); err != nil {
		return err
	}
	if opts.Build {
		if err := buildGeneratedProject(opts.OutputDir, binaryName); err != nil {
			return err
		}
	}
	return nil
}

func deriveBinaryName(doc *openapi3.T, specPath string) string {
	if doc != nil && doc.Info != nil && strings.TrimSpace(doc.Info.Title) != "" {
		return sanitizeSlug(doc.Info.Title)
	}

	basePath := specPath
	if parsed, err := url.Parse(specPath); err == nil && parsed.Path != "" {
		basePath = parsed.Path
	}
	base := strings.TrimSuffix(filepath.Base(basePath), filepath.Ext(basePath))
	if base == "" {
		return "agent-cli"
	}
	return sanitizeSlug(base)
}

func ensureOutputDir(outputDir string, overwrite bool) error {
	info, err := os.Stat(outputDir)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("output path exists and is not a directory: %s", outputDir)
		}
		entries, readErr := os.ReadDir(outputDir)
		if readErr != nil {
			return fmt.Errorf("read output directory: %w", readErr)
		}
		if len(entries) > 0 && !overwrite {
			return fmt.Errorf("output directory is not empty: %s (pass --overwrite to allow this)", outputDir)
		}
		if len(entries) > 0 && overwrite {
			markerPath := filepath.Join(outputDir, generatedMarkerFile)
			if _, markerErr := os.Stat(markerPath); markerErr != nil {
				return fmt.Errorf("refusing to overwrite a non-generated directory: %s", outputDir)
			}
			for _, entry := range entries {
				if removeErr := os.RemoveAll(filepath.Join(outputDir, entry.Name())); removeErr != nil {
					return fmt.Errorf("clean output directory: %w", removeErr)
				}
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat output directory: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	return nil
}

func writeGeneratedMarker(outputDir string, manifest Manifest) error {
	payload := map[string]any{
		"tool":             "agent-cli-generator",
		"generatorVersion": generatorVersion,
		"name":             manifest.Name,
		"title":            manifest.Title,
	}
	blob, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal generated marker: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, generatedMarkerFile), blob, 0o644); err != nil {
		return fmt.Errorf("write generated marker: %w", err)
	}
	return nil
}

func buildGeneratedProject(outputDir, binaryName string) error {
	if err := os.MkdirAll(filepath.Join(outputDir, "bin"), 0o755); err != nil {
		return fmt.Errorf("create build output directory: %w", err)
	}
	if err := runCommand(outputDir, "go", "mod", "tidy"); err != nil {
		return fmt.Errorf("prepare generated module: %w", err)
	}
	if err := runCommand(outputDir, "go", "build", "-o", filepath.Join("bin", binaryName), "."); err != nil {
		return fmt.Errorf("build generated CLI: %w", err)
	}
	return nil
}

func runCommand(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
