package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/jeff/agent-cli-generator/internal/generator"
)

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
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	specPath := fs.String("spec", "", "Path or URL to an OpenAPI 3.0/3.1 or Swagger 2.0 spec")
	outputDir := fs.String("output", "", "Directory to write the generated CLI project into")
	name := fs.String("name", "", "Binary name for the generated CLI")
	moduleName := fs.String("module", "", "Go module name for the generated CLI")
	repo := fs.String("repo", "", "GitHub repository in owner/name form for generated release/install scaffolding")
	homebrewTap := fs.String("homebrew-tap", "", "GitHub tap repository in owner/name form for generated Homebrew publishing config")
	build := fs.Bool("build", false, "Run go build in the generated project")
	overwrite := fs.Bool("overwrite", false, "Allow writing into an existing directory")

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
