package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/alecthomas/kong"

	"github.com/kure-sh/ingest-go/config"
	"github.com/kure-sh/ingest-go/walk"
)

var cli struct {
	Config   string   `short:"c" default:"kure.toml" help:"kure.toml configuration file"`
	Cd       string   `short:"d" type:"path" help:"Change to this directory before starting"`
	Output   string   `short:"o" default:"schema" help:"Directory to write generated schemas"`
	Packages []string `arg:"" help:"Go packages to scan" name:"package"`
}

func main() {
	kong.Parse(&cli,
		kong.Name("kure-ingest-go"),
		kong.Description("Generate Kure API definitions from a Go project"),
		kong.UsageOnError())

	conf, err := config.LoadConfig(cli.Config)
	if err != nil {
		log.Fatalf("failed to load kure.toml: %v", err)
	}

	output, err := filepath.Abs(cli.Output)
	if err != nil {
		log.Fatalf("failed to resolve output path %q: %v", cli.Output, err)
	}

	if cli.Cd != "" && cli.Cd != "." {
		if err := os.Chdir(cli.Cd); err != nil {
			log.Fatalf("failed to change working directory to %q: %v", cli.Cd, err)
		}
	}

	packages, err := walk.LoadPackages(cli.Packages...)
	if err != nil {
		log.Fatalf("failed to load packages: %v", err)
	}

	local, err := walk.LoadGoModule()
	if err != nil {
		log.Fatalf("failed to load go.mod: %v", err)
	}

	if err := conf.ResolveVersions(local.Dependencies); err != nil {
		log.Fatalf("failed to resolve dependency version: %v", err)
	}

	walk.APIPackages(conf, local, packages)
	gctx := walk.NewGeneratorContext(conf, packages)

	bundle, err := walk.GenerateBundle(gctx)
	if err != nil {
		log.Fatalf("error: %v", err)
	}

	fmt.Printf("API: %s\n", bundle.API.Name)

	if err := walk.WriteBundle(bundle, output); err != nil {
		log.Fatalf("error: %v", err)
	}
}
