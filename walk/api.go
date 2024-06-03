package walk

import (
	"os"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/kure-sh/ingest-go/config"
	"github.com/kure-sh/ingest-go/spec"
)

func APIPackages(conf *config.Config, local *LocalGoModule, pkgs []*Package) []*Package {
	var apis []*Package

	for _, pkg := range pkgs {
		local.ResolvePackage(pkg)
		pkg.Group = groupForPackage(conf, pkg)

		if pkg.Imports().APIMachinery() {
			apis = append(apis, pkg)
		}
	}

	return apis
}

func LoadGoModule() (*LocalGoModule, error) {
	contents, err := os.ReadFile("go.mod")
	if err != nil {
		return nil, err
	}

	mf, err := modfile.Parse("go.mod", contents, nil)
	if err != nil {
		return nil, err
	}

	deps := make(map[string]*modfile.Require, len(mf.Require))
	for _, req := range mf.Require {
		deps[req.Mod.Path] = req
	}

	return &LocalGoModule{
		Path:         mf.Module.Mod.Path,
		GoVersion:    mf.Go.Version,
		Dependencies: deps,
	}, nil
}

type LocalGoModule struct {
	Path         string
	GoVersion    string
	Dependencies map[string]*modfile.Require
}

func (m LocalGoModule) ResolvePackage(pkg *Package) *modfile.Require {
	pkg.Local = withinModule(m.Path, pkg.Path())

	if !pkg.Local {
		for name, dep := range m.Dependencies {
			if withinModule(name, pkg.Path()) {
				return dep
			}
		}
	}

	return nil
}

func groupForPackage(conf *config.Config, pkg *Package) *PackageGroup {
	resolved := conf.ResolvePackage(pkg.Path())
	if resolved == nil {
		return nil
	}

	export := resolved.Export()
	var module *string
	if export.Module != "" {
		module = &export.Module
	}

	return &PackageGroup{
		APIGroupIdentifier: spec.APIGroupIdentifier{
			Module: module,
			Name:   export.Group,
		},
		Version: export.Version,
	}
}

func withinModule(mod string, pkg string) bool {
	return mod == pkg || strings.HasPrefix(pkg, mod+"/")
}
