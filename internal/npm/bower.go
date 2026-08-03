package npm

import (
	"encoding/json"
	"github.com/git-pkgs/manifests/internal/core"
)

func init() {
	core.Register("bower", core.Manifest, &bowerParser{}, core.ExactMatch("bower.json"))
}

// bowerParser parses bower.json files.
type bowerParser struct{}

type bowerJSON struct {
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	License         any               `json:"license"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func (p *bowerParser) Parse(filename string, content []byte) (*core.Result, error) {
	var bower bowerJSON
	if err := json.Unmarshal(content, &bower); err != nil {
		return nil, &core.ParseError{Filename: filename, Err: err}
	}

	var deps []core.Dependency

	for name, version := range bower.Dependencies {
		deps = append(deps, core.Dependency{
			Name:    name,
			Version: version,
			Scope:   core.Runtime,
			Direct:  true,
		})
	}

	for name, version := range bower.DevDependencies {
		deps = append(deps, core.Dependency{
			Name:    name,
			Version: version,
			Scope:   core.Development,
			Direct:  true,
		})
	}

	return &core.Result{
		Name:         bower.Name,
		Version:      bower.Version,
		Licenses:     bowerLicenses(bower.License),
		Dependencies: deps,
	}, nil
}

func bowerLicenses(value any) []string {
	switch license := value.(type) {
	case string:
		if license != "" {
			return []string{license}
		}
	case []any:
		licenses := make([]string, 0, len(license))
		for _, item := range license {
			if text, ok := item.(string); ok && text != "" {
				licenses = append(licenses, text)
			}
		}
		return licenses
	}
	return nil
}
