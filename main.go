package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/jescalan/agent-cli-generator/internal/generator"
)

type configFile struct {
	Spec        string `yaml:"spec"`
	Output      string `yaml:"output"`
	Name        string `yaml:"name"`
	Module      string `yaml:"module"`
	Publish     string `yaml:"publish"`
	Repo        string `yaml:"repo"`
	HomebrewTap string `yaml:"homebrew_tap"`
	Build       bool   `yaml:"build"`
	Overwrite   *bool  `yaml:"overwrite"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		printError(err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "help", "-h", "--help":
		printUsage()
		return nil
	case "generate":
		return runGenerate(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runGenerate(args []string) error {
	cfg, err := loadConfigFile()
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	specPath := fs.String("spec", cfg.Spec, "Path or URL to an OpenAPI 3.0/3.1 or Swagger 2.0 spec")
	outputDir := fs.String("output", cfg.Output, "Directory to write the generated CLI project into")
	name := fs.String("name", cfg.Name, "Binary name for the generated CLI")
	moduleName := fs.String("module", cfg.Module, "Go module name for the generated CLI")
	publishDefault := cfg.publishSlug()
	publish := fs.String("publish", publishDefault, "GitHub repository in owner/name form where the generated CLI will be published")
	repo := fs.String("repo", publishDefault, "Deprecated alias for --publish")
	homebrewTap := fs.String("homebrew-tap", cfg.HomebrewTap, "GitHub tap repository in owner/name form for generated Homebrew publishing config")
	build := fs.Bool("build", cfg.Build, "Run go build in the generated project")
	overwriteDefault := false
	if cfg.Overwrite != nil {
		overwriteDefault = *cfg.Overwrite
	}
	overwrite := fs.Bool("overwrite", overwriteDefault, "Allow writing into an existing directory")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *specPath == "" {
		return errors.New("missing required --spec")
	}
	if *outputDir == "" {
		return errors.New("missing required --output")
	}
	publishValue := strings.TrimSpace(*publish)
	if publishValue == "" {
		publishValue = strings.TrimSpace(*repo)
	}

	return generator.Generate(generator.Options{
		SpecPath:    *specPath,
		OutputDir:   *outputDir,
		Name:        *name,
		ModuleName:  *moduleName,
		Publish:     publishValue,
		HomebrewTap: *homebrewTap,
		Build:       *build,
		Overwrite:   *overwrite,
	})
}

func loadConfigFile() (configFile, error) {
	for _, name := range []string{"agent-cli.yml", "agent-cli.yaml"} {
		data, err := os.ReadFile(name)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return configFile{}, fmt.Errorf("read %s: %w", name, err)
		}
		var cfg configFile
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return configFile{}, fmt.Errorf("parse %s: %w", name, err)
		}
		if cfg.Output == "" {
			cfg.Output = defaultConfigOutputDir(cfg)
		}
		if cfg.Overwrite == nil {
			defaultOverwrite := true
			cfg.Overwrite = &defaultOverwrite
		}
		return cfg, nil
	}
	return configFile{}, nil
}

func defaultConfigOutputDir(cfg configFile) string {
	if repo := normalizeRepoSlug(cfg.publishSlug()); repo != "" {
		if base := filepath.Base(repo); base != "." && base != "/" && base != "" {
			return base
		}
	}
	if name := strings.TrimSpace(cfg.Name); name != "" {
		return name
	}
	return "generated"
}

func normalizeRepoSlug(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.TrimPrefix(trimmed, "https://github.com/")
	trimmed = strings.TrimPrefix(trimmed, "http://github.com/")
	trimmed = strings.Trim(trimmed, "/")
	return trimmed
}

func (cfg configFile) publishSlug() string {
	if value := strings.TrimSpace(cfg.Publish); value != "" {
		return value
	}
	return strings.TrimSpace(cfg.Repo)
}

func printUsage() {
	payload := map[string]any{
		"name":        "agent-cli-generator",
		"description": "Generate agent-first CLIs from OpenAPI specs.",
		"commands": []map[string]any{
			{
				"name":        "generate",
				"description": "Generate a standalone CLI project from an OpenAPI spec.",
				"flags": []string{
					"--spec",
					"--output",
					"--name",
					"--module",
					"--repo",
					"--publish",
					"--homebrew-tap",
					"--build",
					"--overwrite",
				},
			},
		},
		"config":  "Optional: place an agent-cli.yml in the working directory to set defaults for all flags.",
		"example": "agent-cli-generator generate --spec https://example.com/openapi.yaml --output ./out/execos-cli --name execos --build",
	}
	blob, _ := json.MarshalIndent(payload, "", "  ")
	fmt.Println(string(blob))
}

func printError(err error) {
	payload := map[string]any{
		"error": map[string]any{
			"message": err.Error(),
		},
	}
	blob, _ := json.MarshalIndent(payload, "", "  ")
	fmt.Fprintln(os.Stderr, string(blob))
}
