package walk

import (
	"fmt"
	"go/ast"
	"go/doc"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unsafe"

	"sigs.k8s.io/controller-tools/pkg/loader"

	"github.com/kure-sh/ingest-go/config"
	"github.com/kure-sh/ingest-go/spec"
)

const metav1 = "k8s.io/apimachinery/pkg/apis/meta/v1"

type GeneratorContext struct {
	Config   *config.Config
	Packages map[string]*Package
}

func NewGeneratorContext(conf *config.Config, pkgs []*Package) *GeneratorContext {
	pmap := make(map[string]*Package, len(pkgs))

	for _, pkg := range pkgs {
		pmap[pkg.Path()] = pkg
	}

	return &GeneratorContext{Config: conf, Packages: pmap}
}

type Generator struct {
	*GeneratorContext

	Target *Package
	Export *config.Export

	// The package-level comment
	comment Comment
	// All comments in the package, indexed by filename and last line number
	comments packageComments
	decls    Declarations
	deps     map[string]*config.Dependency
}

func NewGenerator(gctx *GeneratorContext, target *Package) *Generator {
	if gctx.Packages[target.Path()] != target {
		panic(fmt.Sprintf("package %s not defined", target.Path()))
	}

	export := gctx.Config.Export(target.Path())
	if export == nil {
		panic(fmt.Sprintf("no export defined for package %s", target.Path()))
	}

	gen := &Generator{
		GeneratorContext: gctx,
		Target:           target,
		Export:           export,
		comment:          ReadComment(target.doc.Doc),
		comments:         scanPackageComments(target.pkg),
		decls:            target.Declarations(),
		deps:             make(map[string]*config.Dependency),
	}

	return gen
}

func (g *Generator) Generate() (*spec.APIGroupVersion, error) {
	if g.Target.Group == nil {
		return nil, fmt.Errorf("API group not defined for %s", g.Target.Path())
	}

	gv := &spec.APIGroupVersion{
		APIVersion: "spec.kure.sh/v1alpha1",
		Kind:       "APIGroupVersion",

		API:     g.Config.Name,
		Group:   g.Target.Group.APIGroupIdentifier,
		Version: g.Target.Group.Version,

		Dependencies: []spec.APIDependency{},
	}

	defs, err := g.Definitions()
	if err != nil {
		return nil, err
	}
	gv.Definitions = defs

	for _, dep := range g.deps {
		gv.Dependencies = append(gv.Dependencies, spec.APIDependency{
			Package: dep.Name,
			Version: dep.Version,
		})
	}

	return gv, nil
}

func (g *Generator) Definitions() ([]spec.Definition, error) {
	var defs []spec.Definition

	for _, tn := range g.decls.Types {
		name := tn.Name()

		named, ok := tn.Type().(*types.Named)
		if !ok {
			fmt.Printf("  %s is not a Named: %v\n", name, tn.Type())
			continue
		}

		doct := g.Target.docTypes[name]

		def, err := g.definition(name, g.underlying(named), named, doct, tn.Pos())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}

		if def != nil {
			defs = append(defs, *def)
		}
	}

	return defs, nil
}

func (g *Generator) underlying(named *types.Named) types.Type {
	if named.Obj().Pkg().Path() != g.Target.Path() {
		return named
	}

	// Use reflect + unsafe to access the unexported field fromRHS
	// TODO: is there really no better way? unclear otherwise how to distinguish
	//     type Foo struct{A: boolean}
	// from
	//     type Bar external.Bar
	// where `external` has
	//     type Bar struct{A: boolean}
	namedValue := reflect.ValueOf(named).Elem()
	fromRHS := namedValue.FieldByName("fromRHS")

	if fromRHS.IsValid() {
		value := reflect.NewAt(fromRHS.Type(), unsafe.Pointer(fromRHS.UnsafeAddr())).Elem()
		if inner, ok := value.Interface().(*types.Named); ok {
			fmt.Printf("%s ==> %s\n", named, inner)
			return inner
		}
	}

	return named.Underlying()
}

func (g *Generator) definition(name string, t types.Type, w types.Type, d *doc.Type, p token.Pos) (*spec.Definition, error) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("panic in definition(): %s.%s\n", g.Target.Path(), name)
			panic(r)
		}
	}()

	var comment Comment
	if d != nil {
		comment = ReadComment(d.Doc)
	}

	if comment.Marker("protobuf") == "false" {
		return nil, nil
	}

	meta := spec.DefinitionMeta{
		Name:        name,
		Description: comment.Text,
		Deprecated:  comment.Deprecated(),
	}

	typeDef, err := g.value(t, w, d, p)
	if typeDef == nil {
		return nil, err
	}

	return &spec.Definition{DefinitionMeta: meta, Value: *typeDef}, nil
}

func (g *Generator) value(t types.Type, w types.Type, d *doc.Type, p token.Pos) (r *spec.Type, err error) {
	comment := ReadComment(d.Doc)

	switch t := t.(type) {
	case *types.Basic:
		r, err = g.basicType(t, w, d)
	case *types.Struct:
		r, err = g.structType(t, w, d, p)
	case *types.Named:
		r, err = g.referenceType(t, d)
	case *types.Slice:
		r, err = g.arrayType(t, d)
	case *types.Map:
		r, err = g.mapType(t, d)
	case *types.Pointer:
		r, err = g.pointerType(t, d)
	case *types.Interface:
		if t.Empty() {
			return &spec.Type{Variant: &spec.UnknownType{}}, nil
		}
	default:
		err = fmt.Errorf("unimplemented type %T: %v", t, t)
	}

	if r == nil {
		return
	}

	if _, optional := r.Variant.(*spec.OptionalType); !optional && comment.Marker("+nullable") == "true" {
		r = &spec.Type{
			Variant: &spec.OptionalType{Value: *r},
		}
	}

	return
}

func (g *Generator) basicType(t *types.Basic, w types.Type, d *doc.Type) (*spec.Type, error) {
	info := t.Info()

	switch {
	case info&types.IsString != 0:
		var comment Comment
		if d != nil {
			comment = ReadComment(d.Doc)
		}

		var enum []string
		if values := comment.Marker("kubebuilder:validation:Enum"); values != "" {
			var err error
			enum, err = scanEnumValidation(values)
			if err != nil {
				return nil, fmt.Errorf("invalid Enum marker: %w", err)
			}
		} else if comment.Marker("enum") == "true" {
			enum = g.constantValues(w)
		}

		return &spec.Type{
			Variant: &spec.StringType{
				Enum:   enum,
				Format: comment.Marker("kubebuilder:validation:Format"),
			},
		}, nil

	case info&types.IsBoolean != 0:
		return &spec.Type{
			Variant: &spec.BooleanType{},
		}, nil

	case info&types.IsInteger != 0:
		var size int
		switch t.Kind() {
		case types.Int32, types.Uint32:
			size = 32
		case types.Int64, types.Uint64, types.Uintptr:
			size = 64
		}

		return &spec.Type{
			Variant: &spec.IntegerType{Size: size},
		}, nil

	case info&types.IsFloat != 0:
		return &spec.Type{
			Variant: &spec.FloatType{Size: 64},
		}, nil

	default:
		return nil, fmt.Errorf("unimplemented type %T: %v", t, t)
	}
}

func (g *Generator) constantValues(t types.Type) (vals []string) {
	for _, c := range g.decls.Constants {
		if c.Type() == t {
			val, err := strconv.Unquote(c.Val().ExactString())
			if err != nil {
				panic(err)
			}

			vals = append(vals, val)
		}
	}

	return
}

func (g *Generator) structType(t *types.Struct, w types.Type, d *doc.Type, p token.Pos) (*spec.Type, error) {
	if named, ok := w.(*types.Named); ok {
		for i := 0; i < named.NumMethods(); i++ {
			if named.Method(i).Name() == "OpenAPISchemaType" {
				// assume serialized as a string
				return &spec.Type{
					Variant: &spec.StringType{},
				}, nil
			}
		}
	}

	props := make([]spec.Property, 0, t.NumFields())
	var parents []spec.Type

	comment := ReadComment(d.Doc)
	comment.AddMarkers(g.markerComments(p)) // look for markers above the doc comment

	var fields map[string]*ast.Field

	if d.Decl != nil {
		fields = make(map[string]*ast.Field, t.NumFields())

		for _, spec := range d.Decl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, astField := range structType.Fields.List {
				for _, fieldName := range astField.Names {
					fields[fieldName.Name] = astField
				}
			}
		}
	}

	hasTypeMeta := false
	hasObjectMeta := false

	for i := 0; i < t.NumFields(); i++ {
		field := t.Field(i)
		tag := t.Tag(i)

		var fdoc string
		if fields != nil {
			if astField := fields[field.Name()]; astField != nil {
				if d := astField.Doc; d != nil {
					fdoc = d.Text()
				}
			}
		}

		// detect Kubernetes resource types
		if nt, ok := field.Type().(*types.Named); ok {
			tn := nt.Obj()
			npkg := tn.Pkg()

			if npkg != nil && npkg.Path() == metav1 {
				switch tn.Name() {
				case "TypeMeta":
					hasTypeMeta = true
					continue
				case "ObjectMeta":
					hasObjectMeta = true
				case "ListMeta":
					// Skip generating *List types, except for metav1.List itself
					if g.Target.Path() != metav1 || d.Name != "List" {
						return nil, nil
					}
				}
			}
		}

		vt, err := g.value(field.Type(), nil, &doc.Type{Doc: fdoc}, token.NoPos)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Name(), err)
		} else if vt == nil {
			return nil, nil
		}

		var name string
		parent := false
		omissible := false
		if tag != "" {
			json := reflect.StructTag(tag).Get("json")

			if json != "" {
				parts := strings.Split(json, ",")
				name = parts[0]

				parent = slices.Contains(parts[1:], "inline")
				omissible = slices.Contains(parts[1:], "omitempty")
			}
		}
		if (name == "" && !parent) || name == "-" {
			continue
		}

		if parent {
			if _, ok := vt.Variant.(*spec.ReferenceType); !ok {
				return nil, fmt.Errorf("an inline field must be a named type")
			}

			parents = append(parents, *vt)
		} else {
			comment := ReadComment(fdoc)

			props = append(props, spec.Property{
				PropertyMeta: spec.PropertyMeta{
					DefinitionMeta: spec.DefinitionMeta{
						Name:        name,
						Description: comment.Text,
						Deprecated:  comment.Deprecated(),
					},
					Required: g.fieldRequired(comment, omissible),
				},
				Value: *vt,
			})
		}
	}

	if hasTypeMeta && hasObjectMeta {
		if len(parents) > 0 {
			return nil, fmt.Errorf("resources cannot have inline fields")
		}

		for i, prop := range props {
			if prop.Name == "spec" && !prop.Required {
				props[i].Required = true
			} else if prop.Name == "status" && prop.Required {
				props[i].Required = false
			}
		}

		return &spec.Type{
			Variant: &spec.ResourceType{
				Properties: props,
				Metadata:   g.resourceMeta(d.Name, comment),
			},
		}, nil
	}

	return &spec.Type{
		Variant: &spec.ObjectType{Inherit: parents, Properties: props},
	}, nil
}

func (g *Generator) resourceMeta(kind string, comment Comment) spec.ResourceMeta {
	resource := ""
	scale := comment.Marker("kubebuilder:subresource:scale")
	status := comment.Marker("kubebuilder:subresource:status")
	for _, m := range comment.Markers {
		if strings.HasPrefix(m, "kubebuilder:resource:") {
			value := strings.TrimPrefix(m, "kubebuilder:resource:")

			if resource == "" {
				resource = value
			} else {
				resource += "," + value
			}
		}
	}

	name := ""
	singularName := ""
	scope := spec.ScopeNamespace

	if resource != "" {
		parts := strings.Split(resource, ",")

		for _, part := range parts {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				continue
			}

			switch kv[0] {
			case "path":
				name = kv[1]
			case "singular":
				singularName = kv[1]
			case "scope":
				switch kv[1] {
				case "Cluster", "cluster":
					scope = spec.ScopeCluster
				case "Namespaced", "namespaced", "namespace":
					scope = spec.ScopeNamespace
				}
			}
		}
	}
	for _, m := range comment.Markers {
		if strings.HasPrefix(m, "genclient:") && strings.Contains(m, "nonNamespaced") {
			scope = spec.ScopeCluster
			break
		}
	}

	return spec.ResourceMeta{
		Name:         name,
		SingularName: singularName,
		Kind:         kind,
		Scope:        scope,
		Subresources: spec.Subresources{
			Status: status != "",
			Scale:  scale != "",
		},
	}
}

func (g *Generator) fieldRequired(comment Comment, omissible bool) bool {
	required := g.comment.Marker("kubebuilder:validation:Optional") == ""

	if comment.Marker("+optional") == "true" || comment.Marker("kubebuilder:validation:Optional") == "true" || omissible {
		required = false
	} else if comment.Marker("kubebuilder:validation:Required") == "true" {
		required = true
	}

	return required
}

// Find the comment group one line above the type's doc comment, or one line
// above the type itself (if no docs).
func (g *Generator) markerComments(p token.Pos) *ast.CommentGroup {
	pos := g.Target.pkg.Fset.Position(p)

	if docComment := g.comments.get(pos.Filename, pos.Line-1); docComment != nil {
		pos = g.Target.pkg.Fset.Position(docComment.List[0].Slash)
	}

	return g.comments.get(pos.Filename, pos.Line-2)
}

func (g *Generator) referenceType(t *types.Named, d *doc.Type) (*spec.Type, error) {
	n := t.Obj()

	targetPath := loader.NonVendorPath(n.Pkg().Path())

	var scope *spec.ReferenceScope

	if g.Target.Path() != targetPath {
		if res := g.builtinReferenceType(targetPath, n.Name()); res != nil {
			return res, nil
		}

		target := g.Config.ResolvePackage(targetPath)
		if target == nil {
			return nil, fmt.Errorf("undeclared package %s", targetPath)
		}

		export := target.Export()
		var module *string
		if export.Module != "" {
			module = &export.Module
		}

		depName := target.Dependency()
		scope = &spec.ReferenceScope{
			Package: depName,
			Group: spec.APIGroupIdentifier{
				Module: module,
				Name:   export.Group,
			},
			Version: export.Version,
		}

		if depName != "" {
			depPkg := g.Config.Dependency(depName)
			if depPkg == nil {
				return nil, fmt.Errorf("extern package %+v not a declared dependency", depName)
			}

			g.deps[depName] = depPkg
		}
	}

	return &spec.Type{
		Variant: &spec.ReferenceType{
			Target: spec.ReferenceTarget{
				Scope: scope,
				Name:  n.Name(),
			},
		},
	}, nil
}

func (g *Generator) builtinReferenceType(pkgPath, name string) *spec.Type {
	meta := "meta"

	switch {
	case pkgPath == "k8s.io/apimachinery/pkg/runtime" && (name == "Object" || name == "RawExtension"):
		return &spec.Type{Variant: &spec.UnknownType{}}

	case pkgPath == "k8s.io/apimachinery/pkg/util/intstr" && name == "IntOrString":
		return &spec.Type{
			Variant: &spec.UnionType{
				Values: []spec.Type{
					{Variant: &spec.IntegerType{Size: 32}},
					{Variant: &spec.StringType{}},
				},
			},
		}

	// TODO: this is kinda janky
	case pkgPath == "k8s.io/apimachinery/pkg/api/resource" && name == "Quantity":
		return &spec.Type{
			Variant: &spec.ReferenceType{
				Target: spec.ReferenceTarget{
					Scope: &spec.ReferenceScope{
						Package: "kubernetes",
						Group: spec.APIGroupIdentifier{
							Module: &meta,
							Name:   meta,
						},
						Version: "v1",
					},
					Name: name,
				},
			},
		}

	case pkgPath == "time" && name == "Duration":
		return &spec.Type{
			Variant: &spec.StringType{
				Format: "duration",
			},
		}
	case pkgPath == "time" && name == "Time":
		return &spec.Type{
			Variant: &spec.StringType{
				Format: "date-time",
			},
		}
	}

	return nil
}

func (g *Generator) arrayType(t *types.Slice, d *doc.Type) (*spec.Type, error) {
	et := t.Elem()

	if et, ok := et.(*types.Basic); ok && et.Kind() == types.Byte {
		return &spec.Type{
			Variant: &spec.StringType{Format: "byte"},
		}, nil
	}

	value, err := g.value(et, nil, d, token.NoPos)
	if value == nil {
		return nil, err
	}

	return &spec.Type{
		Variant: &spec.ArrayType{Values: *value},
	}, nil
}

func (g *Generator) mapType(t *types.Map, d *doc.Type) (*spec.Type, error) {
	if key, ok := t.Key().(*types.Basic); ok {
		if key.Info()&types.IsString == 0 {
			return nil, fmt.Errorf("map keys must be strings, not %s", t.Key())
		}
	}

	et := t.Elem()

	value, err := g.value(et, nil, d, token.NoPos)
	if err != nil {
		return nil, err
	}

	return &spec.Type{
		Variant: &spec.MapType{Values: *value},
	}, nil
}

func (g *Generator) pointerType(t *types.Pointer, d *doc.Type) (*spec.Type, error) {
	et := t.Elem()
	value, err := g.value(et, nil, d, token.NoPos)
	if err != nil {
		return nil, err
	}

	if named, ok := et.(*types.Named); ok {
		et = named.Underlying()
	}
	if _, ok := et.(*types.Basic); ok && !g.Export.ExplicitNull {
		return &spec.Type{
			Variant: &spec.OptionalType{
				Value: *value,
			},
		}, nil
	}

	return value, nil
}
