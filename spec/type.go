package spec

import (
	"github.com/byrnedo/pjson"
)

type Definition struct {
	DefinitionMeta
	Value Type `json:"value"`
}

type DefinitionMeta struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Deprecated  bool   `json:"deprecated,omitempty"`
}

type Property struct {
	PropertyMeta
	Value Type `json:"value"`
}

type PropertyMeta struct {
	DefinitionMeta
	Required bool `json:"required,omitempty"`
}

var marshaler = pjson.New([]TypeVariant{
	&ResourceType{},
	&StringType{},
	&IntegerType{},
	&FloatType{},
	&BooleanType{},
	&ObjectType{},
	&ArrayType{},
	&MapType{},
	&UnionType{},
	&ReferenceType{},
	&UnknownType{},
})

type Type struct {
	Variant TypeVariant
}

func (t Type) MarshalJSON() ([]byte, error) {
	return marshaler.MarshalObject(t.Variant)
}

func (t *Type) UnmarshalJSON(bytes []byte) (err error) {
	t.Variant, err = marshaler.UnmarshalObject(bytes)
	return
}

type TypeVariant interface {
	pjson.Variant
}

type StringType struct {
	Enum   []string `json:"enum,omitempty"`
	Format string   `json:"format,omitempty"`
}

func (t StringType) Variant() string {
	return "string"
}

type IntegerType struct {
	Size int `json:"size,omitempty"`
}

func (t IntegerType) Variant() string {
	return "integer"
}

type FloatType struct {
	Size int `json:"size,omitempty"`
}

func (t FloatType) Variant() string {
	return "float"
}

type BooleanType struct{}

func (t BooleanType) Variant() string {
	return "boolean"
}

type ObjectType struct {
	Inherit    []Type     `json:"inherit,omitempty"`
	Properties []Property `json:"properties"`
}

func (t ObjectType) Variant() string {
	return "object"
}

type ArrayType struct {
	Values Type `json:"values"`
}

func (t ArrayType) Variant() string {
	return "array"
}

type MapType struct {
	Values Type `json:"values"`
}

func (t MapType) Variant() string {
	return "map"
}

type UnionType struct {
	Values []Type `json:"values"`
}

func (t UnionType) Variant() string {
	return "union"
}

type OptionalType struct {
	Value Type `json:"value"`
}

func (t OptionalType) Variant() string {
	return "optional"
}

type UnknownType struct{}

func (t UnknownType) Variant() string {
	return "unknown"
}

type ReferenceType struct {
	Target ReferenceTarget `json:"target"`
}

func (t ReferenceType) Variant() string {
	return "reference"
}

type ReferenceTarget struct {
	Scope *ReferenceScope `json:"scope,omitempty"`
	Name  string          `json:"name"`
}

type ReferenceScope struct {
	Package string             `json:"package,omitempty"`
	Group   APIGroupIdentifier `json:"group"`
	Version string             `json:"version"`
}

type ReferencePackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
