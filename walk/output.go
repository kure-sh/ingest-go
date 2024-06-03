package walk

import (
	"encoding/json"
	"fmt"
	"os"
	"path"

	"github.com/kure-sh/ingest-go/spec"
)

func WriteBundle(bundle *spec.Bundle, out string) error {
	var files fileset

	if err := files.add(path.Join(out, "index.json"), &bundle.API); err != nil {
		return err
	}

	for _, group := range bundle.Groups {
		base := out
		if group.Module != nil {
			base = path.Join(base, *group.Module)
		}
		if err := files.add(path.Join(base, "group.json"), group); err != nil {
			return err
		}

		for _, version := range bundle.Versions {
			if !version.Group.Same(group.APIGroupIdentifier) {
				continue
			}

			filename := fmt.Sprintf("%s.json", version.Version)
			if err := files.add(path.Join(base, filename), version); err != nil {
				return err
			}
		}
	}

	return files.write()
}

type fileset []*file

func (s *fileset) add(path string, contents any) error {
	file, err := newFile(path, contents)
	if file == nil {
		return err
	}

	*s = append(*s, file)
	return nil
}

func (s fileset) write() error {
	for _, f := range s {
		if err := os.MkdirAll(path.Dir(f.path), 0755); err != nil {
			return err
		}

		if err := os.WriteFile(f.path, f.data, 0644); err != nil {
			return err
		}
	}

	return nil
}

type file struct {
	path string
	data []byte
}

func newFile(path string, contents any) (*file, error) {
	data, err := json.MarshalIndent(contents, "", "  ")
	if err != nil {
		return nil, err
	}

	return &file{path: path, data: data}, nil

}
