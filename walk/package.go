package walk

import (
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"go/types"

	"github.com/kure-sh/ingest-go/spec"
	"golang.org/x/tools/go/packages"
)

type Package struct {
	pkg *packages.Package
	doc *doc.Package

	Group *PackageGroup
	Local bool

	docTypes map[string]*doc.Type
}

type PackageGroup struct {
	spec.APIGroupIdentifier
	Version string
}

func LoadPackages(patterns ...string) ([]*Package, error) {
	cfg := packages.Config{
		Mode: packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps | packages.NeedSyntax | packages.NeedFiles,
	}

	lpkgs, err := packages.Load(&cfg, patterns...)
	if err != nil {
		return nil, err
	}

	pkgs := make([]*Package, 0, len(lpkgs))
	for _, lpkg := range lpkgs {
		pkg, err := NewPackage(lpkg)
		if err != nil {
			return nil, err
		}

		pkgs = append(pkgs, pkg)
	}

	return pkgs, nil
}

func NewPackage(pkg *packages.Package) (*Package, error) {
	path := pkg.Types.Path()

	fileSet := token.NewFileSet()
	files := make([]*ast.File, 0, len(pkg.GoFiles))
	for _, filename := range pkg.GoFiles {
		file, err := parser.ParseFile(fileSet, filename, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse file %s: %w", filename, err)
		}

		files = append(files, file)
	}

	docPkg, err := doc.NewFromFiles(fileSet, files, path)
	if err != nil {
		return nil, fmt.Errorf("parse go doc %s: %w", path, err)
	}

	wpkg := &Package{pkg: pkg, doc: docPkg}
	return wpkg, wpkg.initialize()
}

func (p *Package) initialize() error {
	p.docTypes = make(map[string]*doc.Type, len(p.doc.Types))

	for _, dt := range p.doc.Types {
		p.docTypes[dt.Name] = dt
	}

	return nil
}

func (p *Package) Path() string {
	return p.pkg.Types.Path()
}

func (p *Package) Scope() *types.Scope {
	return p.pkg.Types.Scope()
}

func (p *Package) Imports() Imports {
	set := make(Imports, len(p.pkg.Imports))

	for name := range p.pkg.Imports {
		set[name] = struct{}{}
	}

	return set
}

func (p *Package) Declarations() (decls Declarations) {
	scope := p.Scope()

	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}

		switch obj := obj.(type) {
		case *types.TypeName:
			decls.Types = append(decls.Types, obj)

		case *types.Const:
			decls.Constants = append(decls.Constants, obj)
		}
	}

	return
}

type Imports map[string]struct{}

func (i Imports) APIMachinery() bool {
	for name := range i {
		if withinModule("k8s.io/apimachinery", name) {
			return true
		}
	}

	return false
}

type Declarations struct {
	Types     []*types.TypeName
	Constants []*types.Const
}
