package npm

import (
	"encoding/json"
	"github.com/git-pkgs/manifests/internal/core"
	"strings"
)

func init() {
	core.Register("deno", core.Manifest, &denoJSONParser{}, core.ExactMatch("deno.json"))
	core.Register("deno", core.Manifest, &denoJSONParser{}, core.ExactMatch("deno.jsonc"))
	core.Register("deno", core.Lockfile, &denoLockParser{}, core.ExactMatch("deno.lock"))
}

// denoJSONParser parses deno.json files.
type denoJSONParser struct{}

type denoJSON struct {
	Name    string            `json:"name"`
	Version string            `json:"version"`
	Imports map[string]string `json:"imports"`
	Tasks   map[string]any    `json:"tasks"`
}

func (p *denoJSONParser) Parse(filename string, content []byte) (*core.Result, error) {
	var deno denoJSON
	if err := json.Unmarshal(content, &deno); err != nil {
		return nil, &core.ParseError{Filename: filename, Err: err}
	}

	var deps []core.Dependency

	for key, spec := range deno.Imports {
		if dep, ok := denoImportDependency(key, spec); ok {
			deps = append(deps, dep)
		}
	}

	scripts := core.StringScripts(deno.Tasks)
	for name, task := range deno.Tasks {
		if properties, ok := task.(map[string]any); ok {
			if command, ok := properties["command"].(string); ok {
				core.AddScript(&scripts, name, command)
			}
		}
	}
	return &core.Result{Name: deno.Name, Version: deno.Version, Dependencies: deps, Scripts: scripts}, nil
}

// denoLockParser parses deno.lock files.
type denoLockParser struct{}

type denoLock struct {
	Specifiers map[string]string      `json:"specifiers"`
	JSR        map[string]denoLockPkg `json:"jsr"`
	NPM        map[string]denoLockPkg `json:"npm"`
}

type denoLockPkg struct {
	Integrity string `json:"integrity"`
}

func (p *denoLockParser) Parse(filename string, content []byte) (*core.Result, error) {
	var lock denoLock
	if err := json.Unmarshal(content, &lock); err != nil {
		return nil, &core.ParseError{Filename: filename, Err: err}
	}

	var deps []core.Dependency

	// Parse JSR packages
	for pkgVer, pkg := range lock.JSR {
		name, version := parseDenoLockPkg(pkgVer)
		deps = append(deps, core.Dependency{
			Name:      name,
			Version:   version,
			Scope:     core.Runtime,
			Integrity: pkg.Integrity,
			Direct:    false,
		})
	}

	// Parse NPM packages
	for pkgVer, pkg := range lock.NPM {
		name, version := parseDenoLockPkg(pkgVer)
		deps = append(deps, core.Dependency{
			Name:      name,
			Version:   version,
			Scope:     core.Runtime,
			Integrity: pkg.Integrity,
			Direct:    false,
			Ecosystem: "npm",
		})
	}

	return &core.Result{Dependencies: deps}, nil
}

// denoImportDependency converts a deno.json import map entry into a dependency.
// npm: and jsr: specifiers are packages. Remote URLs and local paths are
// source-only entries named by their import map key, with no package identity.
// Runtime builtins such as node:fs, inline data: modules and bare values are
// not dependencies, so they are skipped.
func denoImportDependency(key, spec string) (core.Dependency, bool) {
	dep := core.Dependency{Scope: core.Runtime, Direct: true}
	switch {
	case strings.HasPrefix(spec, "npm:"):
		dep.Name, dep.Version = parseNPMSpec(strings.TrimPrefix(spec, "npm:"))
		dep.Ecosystem = "npm"
	case strings.HasPrefix(spec, "jsr:"):
		dep.Name, dep.Version = parseJSRSpec(strings.TrimPrefix(spec, "jsr:"))
	case strings.HasPrefix(spec, "http://"), strings.HasPrefix(spec, "https://"):
		dep.Name = key
		dep.NoPURL = true
		dep.Source = core.Source{Kind: core.SourceURL, Value: spec}
	case strings.HasPrefix(spec, "./"), strings.HasPrefix(spec, "../"),
		strings.HasPrefix(spec, "/"), strings.HasPrefix(spec, "file:"):
		dep.Name = key
		dep.NoPURL = true
		dep.Source = core.Source{Kind: core.SourcePath, Value: spec}
	default:
		return core.Dependency{}, false
	}
	return dep, dep.Name != ""
}

// parseNPMSpec parses an npm spec like "chalk@5.3.0" or "chalk".
func parseNPMSpec(spec string) (name, version string) {
	// Handle scoped packages
	if strings.HasPrefix(spec, "@") {
		if idx := strings.Index(spec[1:], "@"); idx > 0 {
			return spec[:idx+1], spec[idx+2:]
		}
		return spec, ""
	}

	// Handle regular packages
	if idx := strings.Index(spec, "@"); idx > 0 {
		return spec[:idx], spec[idx+1:]
	}
	return spec, ""
}

// parseJSRSpec parses a JSR spec like "@std/path@^1.0.0" or "@std/path".
func parseJSRSpec(spec string) (name, version string) {
	// JSR packages are always scoped like @scope/name@version
	if idx := strings.LastIndex(spec, "@"); idx > 0 {
		return spec[:idx], spec[idx+1:]
	}
	return spec, ""
}

// parseDenoLockPkg parses a lock package key like "@std/fs@1.0.3" or "chalk@5.3.0".
func parseDenoLockPkg(key string) (name, version string) {
	// Handle scoped packages
	if strings.HasPrefix(key, "@") {
		if idx := strings.LastIndex(key, "@"); idx > 0 {
			return key[:idx], key[idx+1:]
		}
		return key, ""
	}

	// Handle regular packages
	if idx := strings.Index(key, "@"); idx > 0 {
		return key[:idx], key[idx+1:]
	}
	return key, ""
}
