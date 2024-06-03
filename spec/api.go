package spec

import (
	"fmt"
)

type Bundle struct {
	API      API                `json:"api"`
	Groups   []*APIGroup        `json:"groups"`
	Versions []*APIGroupVersion `json:"versions"`
}

func NewBundle(gvs []*APIGroupVersion) (*Bundle, error) {
	apiName := ""
	groups := []*APIGroup{}

	for _, gv := range gvs {
		if apiName == "" {
			apiName = gv.API
		} else if apiName != gv.API {
			return nil, fmt.Errorf("conflicting API names %q ≠ %q", apiName, gv.API)
		}

		var group *APIGroup
		for _, g := range groups {
			if g.APIGroupIdentifier.Same(gv.Group) {
				group = g
				group.Versions = append(group.Versions, gv.Version)
				break
			}
		}
		if group == nil {
			group = &APIGroup{
				APIVersion:         APIVersion,
				Kind:               "APIGroup",
				API:                apiName,
				APIGroupIdentifier: gv.Group,
				Versions:           []string{gv.Version},
				PreferredVersion:   nil,
			}
			groups = append(groups, group)
		}
	}

	var groupIds []APIGroupIdentifier
	for _, g := range groups {
		groupIds = append(groupIds, g.APIGroupIdentifier)
	}

	api := API{
		APIVersion: APIVersion,
		Kind:       "API",
		Name:       apiName,
		Groups:     groupIds,
	}

	return &Bundle{
		API:      api,
		Groups:   groups,
		Versions: gvs,
	}, nil
}

const APIVersion = "spec.kure.sh/v1alpha1"

type API struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`

	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`

	Groups []APIGroupIdentifier `json:"groups"`
}

type APIGroup struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`

	API                string `json:"api"`
	APIGroupIdentifier `json:",inline"`

	Versions         []string `json:"versions"`
	PreferredVersion *string  `json:"preferredVersion"`
}

type APIGroupIdentifier struct {
	Module *string `json:"module"`
	Name   string  `json:"name"`
}

func (i APIGroupIdentifier) Same(o APIGroupIdentifier) bool {
	if (i.Module == nil) != (o.Module == nil) {
		return false
	}

	sameModule := (i.Module == nil && o.Module == nil) || *i.Module == *o.Module

	return sameModule && i.Name == o.Name
}

type APIGroupVersion struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`

	API     string             `json:"api"`
	Group   APIGroupIdentifier `json:"group"`
	Version string             `json:"version"`

	Dependencies []APIDependency `json:"dependencies"`

	Definitions []Definition `json:"definitions"`
}

type APIDependency struct {
	Package string `json:"package"`
	Version string `json:"version"`
}
