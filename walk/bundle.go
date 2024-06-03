package walk

import (
	"fmt"

	"github.com/kure-sh/ingest-go/config"
	"github.com/kure-sh/ingest-go/spec"
)

func GenerateBundle(gctx *GeneratorContext) (*spec.Bundle, error) {
	gvs := []*spec.APIGroupVersion{}

	for _, export := range gctx.Config.Exports {
		pkg := gctx.Packages[export.Path]
		if pkg == nil {
			return nil, fmt.Errorf("exported package %s was not scanned", export.Path)
		}

		gv, err := NewGenerator(gctx, pkg).Generate()
		if err != nil {
			return nil, fmt.Errorf("generate %s/%s: %w", export.Group, export.Version, err)
		}

		gvs = append(gvs, gv)
	}

	pruneDefinitions(gctx.Config, gvs)

	mgvs, err := applyMerges(gctx.Config, gvs)
	if err != nil {
		return nil, err
	}

	bundle, err := spec.NewBundle(mgvs)
	if err != nil {
		return nil, err
	}

	return bundle, nil
}

type key struct {
	group, version, name string
}

type qualifiedType struct {
	gv *spec.APIGroupVersion
	t  *spec.Type
}

func indexDefinitions(gvs []*spec.APIGroupVersion) map[key]qualifiedType {
	n := 0
	for _, gv := range gvs {
		n += len(gv.Definitions)
	}

	index := make(map[key]qualifiedType, n)

	for _, gv := range gvs {
		group, version := gv.Group.Name, gv.Version

		for i := range gv.Definitions {
			def := &gv.Definitions[i]
			index[key{group, version, def.Name}] = qualifiedType{gv, &def.Value}
		}
	}

	return index
}

func pruneDefinitions(conf *config.Config, gvs []*spec.APIGroupVersion) {
	refs := make(map[key]uint)
	index := indexDefinitions(gvs)

	var visit func(gv *spec.APIGroupVersion, t *spec.Type)
	visit = func(gv *spec.APIGroupVersion, t *spec.Type) {
		switch v := t.Variant.(type) {
		case *spec.ReferenceType:
			var k key
			if scope := v.Target.Scope; scope != nil {
				k = key{scope.Group.Name, scope.Version, v.Target.Name}
			} else {
				k = key{gv.Group.Name, gv.Version, v.Target.Name}
			}

			_, seen := refs[k]
			refs[k] += 1

			if qt, ok := index[k]; ok && !seen {
				visit(qt.gv, qt.t)
			}

		case *spec.ArrayType:
			visit(gv, &v.Values)
		case *spec.MapType:
			visit(gv, &v.Values)
		case *spec.OptionalType:
			visit(gv, &v.Value)
		case *spec.ObjectType:
			for i := range v.Inherit {
				visit(gv, &v.Inherit[i])
			}
			for i := range v.Properties {
				visit(gv, &v.Properties[i].Value)
			}
		case *spec.ResourceType:
			for i := range v.Properties {
				visit(gv, &v.Properties[i].Value)
			}
		case *spec.UnionType:
			for i := range v.Values {
				visit(gv, &v.Values[i])
			}
		}
	}

	// Visit every type visible from a resource (de facto public API).
	for _, gv := range gvs {
		for _, def := range gv.Definitions {
			if _, ok := def.Value.Variant.(*spec.ResourceType); ok {
				visit(gv, &spec.Type{
					Variant: &spec.ReferenceType{
						Target: spec.ReferenceTarget{Name: def.Name},
					},
				})
			}
		}
	}

	for _, gv := range gvs {
		export := exportFor(conf, gv)
		if export == nil || !export.Prune {
			continue
		}

		used := make([]spec.Definition, 0, len(gv.Definitions))

		for _, def := range gv.Definitions {
			if refs[key{gv.Group.Name, gv.Version, def.Name}] > 0 {
				used = append(used, def)
			} else {
				fmt.Printf("prune %s/%s %s\n", gv.Group.Name, gv.Version, def.Name)
			}
		}

		gv.Definitions = used
	}
}

func applyMerges(conf *config.Config, gvs []*spec.APIGroupVersion) ([]*spec.APIGroupVersion, error) {
	type merge struct {
		from spec.APIGroupIdentifier
		to   spec.APIGroupIdentifier
	}
	merged := make([]*spec.APIGroupVersion, 0, len(gvs))
	var merges []merge

	for _, gv := range gvs {
		export := exportFor(conf, gv)
		if export == nil {
			return nil, fmt.Errorf("no export declaration for %s/%s", gv.Group.Name, gv.Version)
		}

		if export.Merge == nil {
			merged = append(merged, gv)
			continue
		}

		if export.Merge.Version == "" {
			export.Merge.Version = export.Version
		}

		target := mergeTarget(gvs, export.Merge)
		if target == nil {
			return nil, fmt.Errorf("merge target not found for %s/%s", gv.Group.Name, gv.Version)
		}

		if err := applyMerge(export.Merge, gv, target); err != nil {
			return nil, fmt.Errorf("failed to merge %s/%s: %w", gv.Group.Name, gv.Version, err)
		}

		merges = append(merges, merge{from: gv.Group, to: target.Group})
	}

	for _, m := range merges {
		updateReferences(merged, m.from, m.to)
	}

	return merged, nil
}

func applyMerge(spec *config.Merge, from, to *spec.APIGroupVersion) error {
	// TODO: dependencies

	include := make(map[string]struct{}, len(spec.Include))
	if spec.Include != nil {
		for _, inc := range spec.Include {
			include[inc] = struct{}{}
		}
	}

	for _, def := range from.Definitions {
		if _, included := include[def.Name]; len(include) > 0 && !included {
			continue
		}

		to.Definitions = append(to.Definitions, def)
	}

	return nil
}

func updateReferences(gvs []*spec.APIGroupVersion, from, to spec.APIGroupIdentifier) {
	for _, gv := range gvs {
		for i := range gv.Definitions {
			t := &gv.Definitions[i].Value
			updateReference(t, gv.Group, from, to)
		}
	}
}

func updateReference(t *spec.Type, loc, from, to spec.APIGroupIdentifier) {
	switch v := t.Variant.(type) {
	case *spec.ReferenceType:
		if v.Target.Scope == nil {
			return
		}

		if v.Target.Scope.Group.Same(from) {
			if loc.Same(to) {
				v.Target.Scope = nil
			} else {
				v.Target.Scope.Group = to
			}
		}

	case *spec.ArrayType:
		updateReference(&v.Values, loc, from, to)
	case *spec.MapType:
		updateReference(&v.Values, loc, from, to)
	case *spec.OptionalType:
		updateReference(&v.Value, loc, from, to)
	case *spec.ObjectType:
		for i := range v.Inherit {
			updateReference(&v.Inherit[i], loc, from, to)
		}
		for i := range v.Properties {
			updateReference(&v.Properties[i].Value, loc, from, to)
		}
	case *spec.ResourceType:
		for i := range v.Properties {
			updateReference(&v.Properties[i].Value, loc, from, to)
		}
	case *spec.UnionType:
		for i := range v.Values {
			updateReference(&v.Values[i], loc, from, to)
		}
	}
}

func exportFor(conf *config.Config, gv *spec.APIGroupVersion) *config.Export {
	for i, export := range conf.Exports {
		if export.Is(gv) {
			return &conf.Exports[i]
		}
	}

	return nil
}

func mergeTarget(gvs []*spec.APIGroupVersion, merge *config.Merge) *spec.APIGroupVersion {
	for _, gv := range gvs {
		var module string
		if gv.Group.Module != nil {
			module = *gv.Group.Module
		}

		if merge.Module == module && merge.Version == gv.Version {
			return gv
		}
	}

	return nil
}
