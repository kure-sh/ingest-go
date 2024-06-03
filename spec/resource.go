package spec

type ResourceType struct {
	Properties []Property   `json:"properties"`
	Metadata   ResourceMeta `json:"metadata"`
}

func (r ResourceType) Variant() string {
	return "resource"
}

type ResourceMeta struct {
	Name         string       `json:"name,omitempty"`
	SingularName string       `json:"singularName,omitempty"`
	Kind         string       `json:"kind"`
	Scope        string       `json:"scope"`
	Subresources Subresources `json:"subresources,omitempty"`
}

type Subresources struct {
	Status bool `json:"status,omitempty"`
	Scale  bool `json:"scale,omitempty"`
}

const (
	ScopeCluster   = "cluster"
	ScopeNamespace = "namespace"
)
