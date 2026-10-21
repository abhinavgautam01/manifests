package swift

import (
	"cmp"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/git-pkgs/manifests/internal/core"
)

func init() {
	core.Register("swift", core.Manifest, &packageSwiftParser{}, core.AnyMatch(
		core.ExactMatch("Package.swift"),
		core.GlobMatch("Package@swift-*.swift"),
	))
	core.Register("swift", core.Lockfile, &packageResolvedParser{}, core.ExactMatch("Package.resolved"))
}

func swiftSourceCoordinate(rawURL string) string {
	if strings.Contains(rawURL, "://") {
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Hostname() == "" {
			return ""
		}

		host := strings.ToLower(parsed.Hostname())
		if parsed.Port() != "" {
			host += ":" + parsed.Port()
		}
		return joinSwiftSourceCoordinate(host, parsed.Path)
	}

	separator := strings.IndexByte(rawURL, ':')
	if separator <= 0 {
		return ""
	}
	host := rawURL[:separator]
	if at := strings.LastIndexByte(host, '@'); at >= 0 {
		host = host[at+1:]
	}
	return joinSwiftSourceCoordinate(strings.ToLower(host), rawURL[separator+1:])
}

func joinSwiftSourceCoordinate(host, path string) string {
	path = strings.Trim(strings.TrimSpace(path), "/")
	path = strings.TrimSuffix(path, ".git")
	if host == "" || path == "" {
		return ""
	}
	return host + "/" + path
}

// packageResolvedParser parses Package.resolved files.
type packageResolvedParser struct{}

type packageResolvedV1 struct {
	Object struct {
		Pins []packageResolvedPinV1 `json:"pins"`
	} `json:"object"`
	Version int `json:"version"`
}

type packageResolvedPinV1 struct {
	Package       string        `json:"package"`
	RepositoryURL string        `json:"repositoryURL"`
	State         resolvedState `json:"state"`
}

type packageResolvedV2 struct {
	Pins    []packageResolvedPinV2 `json:"pins"`
	Version int                    `json:"version"`
}

type packageResolvedPinV2 struct {
	Identity string        `json:"identity"`
	Kind     string        `json:"kind"`
	Location string        `json:"location"`
	State    resolvedState `json:"state"`
}

type resolvedState struct {
	Version  string `json:"version"`
	Revision string `json:"revision"`
	Branch   string `json:"branch"`
}

func (s resolvedState) source(repository string) core.Source {
	return core.Source{Kind: core.SourceGit, Value: repository, Branch: s.Branch, Ref: s.Revision}
}

func (p *packageResolvedParser) Parse(filename string, content []byte) (*core.Result, error) {
	// Try to detect version
	var versionCheck struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(content, &versionCheck); err != nil {
		return nil, &core.ParseError{Filename: filename, Err: err}
	}

	const resolvedV2 = 2
	var deps []core.Dependency
	var err error
	if versionCheck.Version >= resolvedV2 {
		deps, err = parsePackageResolvedV2(filename, content)
	} else {
		deps, err = parsePackageResolvedV1(filename, content)
	}
	return &core.Result{Dependencies: deps}, err
}

func parsePackageResolvedV1(filename string, content []byte) ([]core.Dependency, error) {
	var resolved packageResolvedV1
	if err := json.Unmarshal(content, &resolved); err != nil {
		return nil, &core.ParseError{Filename: filename, Err: err}
	}

	var deps []core.Dependency
	for _, pin := range resolved.Object.Pins {
		name := swiftSourceCoordinate(pin.RepositoryURL)
		if name == "" {
			name = pin.Package
		}

		deps = append(deps, core.Dependency{
			Name:    name,
			Version: cmp.Or(pin.State.Version, pin.State.Revision),
			Scope:   core.Runtime,
			Direct:  false,
			Source:  pin.State.source(pin.RepositoryURL),
		})
	}

	return deps, nil
}

func parsePackageResolvedV2(filename string, content []byte) ([]core.Dependency, error) {
	var resolved packageResolvedV2
	if err := json.Unmarshal(content, &resolved); err != nil {
		return nil, &core.ParseError{Filename: filename, Err: err}
	}

	var deps []core.Dependency
	for _, pin := range resolved.Pins {
		name := pin.Identity
		var source core.Source
		switch pin.Kind {
		case "remoteSourceControl":
			if coordinate := swiftSourceCoordinate(pin.Location); coordinate != "" {
				name = coordinate
			}
			source = pin.State.source(pin.Location)
		case "registry":
			name = strings.ToLower(pin.Identity)
			source = core.Source{Kind: core.SourceRegistry, Value: pin.Identity}
		}

		deps = append(deps, core.Dependency{
			Name:    name,
			Version: cmp.Or(pin.State.Version, pin.State.Revision),
			Scope:   core.Runtime,
			Direct:  false,
			Source:  source,
		})
	}

	return deps, nil
}
