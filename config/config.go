package config

import (
	"fmt"
	"os"

	"github.com/Masterminds/semver/v3"
	"github.com/kure-sh/ingest-go/spec"
	"github.com/pelletier/go-toml/v2"
	"golang.org/x/mod/modfile"
)

type Config struct {
	Name         string       `toml:"name"`
	Exports      []Export     `toml:"export"`
	Dependencies []Dependency `toml:"dependency"`
	Externs      []Extern     `toml:"extern"`
}

type Export struct {
	Path    string `toml:"path"`
	Module  string `toml:"module,omitempty"`
	Group   string `toml:"group"`
	Version string `toml:"version"`

	Include []string `toml:"include,omitempty"`
	Exclude []string `toml:"exclude,omitempty"`

	ExplicitNull bool   `toml:"explicit-null,omitempty"`
	Prune        bool   `toml:"prune,omitempty"`
	Merge        *Merge `toml:"merge,omitempty"`
}

func (e *Export) Is(v *spec.APIGroupVersion) bool {
	var module string
	if v.Group.Module != nil {
		module = *v.Group.Module
	}

	return v.Group.Name == e.Group && module == e.Module && v.Version == e.Version
}

type Merge struct {
	Module  string   `toml:"module"`
	Version string   `toml:"version,omitempty"`
	Include []string `toml:"include,omitempty"`
}

type Dependency struct {
	Name    string `toml:"name"`
	Path    string `toml:"path,omitempty"`
	Version string `toml:"version,omitempty"`
}

type Extern struct {
	Path    string `toml:"path"`
	Package string `toml:"package"`
	Module  string `toml:"module,omitempty"`
	Group   string `toml:"group"`
	Version string `toml:"version"`
}

type Package interface {
	Dependency() string
	Export() *Export
}

func LoadConfig(filename string) (*Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	var conf Config
	if err := toml.NewDecoder(file).Decode(&conf); err != nil {
		return nil, err
	}

	return &conf, nil
}

func (c *Config) Dependency(name string) *Dependency {
	for i, dep := range c.Dependencies {
		if dep.Name == name {
			return &c.Dependencies[i]
		}
	}

	return nil
}

func (c *Config) Export(path string) *Export {
	for i, export := range c.Exports {
		if export.Path == path {
			return &c.Exports[i]
		}
	}

	return nil
}

func (c *Config) ResolvePackage(path string) Package {
	for i, export := range c.Exports {
		if export.Path == path {
			return &c.Exports[i]
		}
	}

	for i, extern := range c.Externs {
		if extern.Path == path {
			return &c.Externs[i]
		}
	}

	return nil
}

func (c *Config) ResolveVersions(required map[string]*modfile.Require) error {
	for i, dep := range c.Dependencies {
		if dep.Version != "" {
			continue
		}
		if dep.Path == "" {
			return fmt.Errorf("dependency %q: no path or version set", dep.Name)
		}

		req := required[dep.Path]
		if req == nil {
			return fmt.Errorf("dependency %q: not required in go.mod", dep.Name)
		}

		version, err := semver.NewVersion(req.Mod.Version)
		if err != nil {
			return fmt.Errorf("dependency %q: invalid version %q: %w", dep.Name, req.Mod.Version, err)
		}

		major, minor := version.Major(), version.Minor()
		if dep.Name == "kubernetes" && major == 0 {
			major = 1
		}

		c.Dependencies[i].Version = fmt.Sprintf("%d.%d", major, minor)
	}

	return nil
}

func (e *Export) Dependency() string {
	return ""
}

func (e *Export) Export() *Export {
	return e
}

func (e *Extern) Dependency() string {
	return e.Package
}

func (e *Extern) Export() *Export {
	return &Export{
		Path:    e.Path,
		Module:  e.Module,
		Group:   e.Group,
		Version: e.Version,
	}
}
