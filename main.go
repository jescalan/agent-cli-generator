package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/jescalan/agent-cli-generator/internal/generator"
)

type configFile struct {
	Spec        string `yaml:"spec"`
	Output      string `yaml:"output"`
	Name        string `yaml:"name"`
	Module      string `yaml:"module"`
	Repo        string `yaml:"repo"`
	HomebrewTap string `yaml:"homebrew_tap"`
	Build       bool   `yaml:"build"`
	Overwrite   bool   `yaml:"overwrite"`
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
	repo := fs.String("repo", cfg.Repo, "GitHub repository in owner/name form for generated release/install scaffolding")
	homebrewTap := fs.String("homebrew-tap", cfg.HomebrewTap, "GitHub tap repository in owner/name form for generated Homebrew publishing config")
	build := fs.Bool("build", cfg.Build, "Run go build in the generated project")
	overwrite := fs.Bool("overwrite", cfg.Overwrite, "Allow writing into an existing directory")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *specPath == "" {
		return errors.New("missing required --spec")
	}
	if *outputDir == "" {
		return errors.New("missing required --output")
	}

	return generator.Generate(generator.Options{
		SpecPath:    *specPath,
		OutputDir:   *outputDir,
		Name:        *name,
		ModuleName:  *moduleName,
		Repo:        *repo,
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
		// When a config file drives generation, apply sensible defaults:
		// output into the current directory and allow overwriting the
		// previous generation (the generator still refuses to overwrite
		// directories it did not create).
		if cfg.Output == "" {
			cfg.Output = "."
		}
		cfg.Overwrite = true
		return cfg, nil
	}
	return configFile{}, nil
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
