package manifests

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
)

// VendorRoot identifies a repository directory containing vendored packages.
// ConfigPath is the configuration or inventory that selected the root. It is
// empty for convention-based roots such as node_modules.
type VendorRoot struct {
	Path       string
	Ecosystem  string
	ConfigPath string
}

// VendoredDependency is an exact package version stored under a vendor root.
// EvidencePath identifies the repository file from which the package identity
// was read.
type VendoredDependency struct {
	Name         string
	Version      string
	Ecosystem    string
	Kind         Kind
	PURL         string
	RootPath     string
	EvidencePath string
}

// VendorDiscovery contains classified vendor roots and the package identities
// discovered beneath them.
type VendorDiscovery struct {
	Roots        []VendorRoot
	Dependencies []VendoredDependency
}

type vendorDiscovery struct {
	reader       RepositoryReader
	roots        map[vendorRootKey]VendorRoot
	dependencies map[vendoredDependencyKey]VendoredDependency
}

type vendorRootKey struct {
	path      string
	ecosystem string
}

type vendoredDependencyKey struct {
	rootPath     string
	ecosystem    string
	name         string
	version      string
	evidencePath string
}

// DiscoverVendors finds package-manager vendor roots and exact package
// identities in a rooted repository. Warnings report malformed configuration,
// unreadable evidence, or incomplete package identities while retaining all
// successfully discovered results.
func DiscoverVendors(reader RepositoryReader) (VendorDiscovery, []error) {
	if reader == nil {
		return VendorDiscovery{}, []error{errors.New("nil repository reader")}
	}

	discovery := &vendorDiscovery{
		reader:       reader,
		roots:        make(map[vendorRootKey]VendorRoot),
		dependencies: make(map[vendoredDependencyKey]VendoredDependency),
	}
	discoverers := []func() []error{
		discovery.discoverNPMVendors,
		discovery.discoverGoVendors,
		discovery.discoverPythonVendors,
		discovery.discoverCargoVendors,
	}
	var warnings []error
	for _, discover := range discoverers {
		warnings = append(warnings, discover()...)
	}
	return discovery.result(), warnings
}

func (d *vendorDiscovery) addRoot(rootPath, ecosystem, configPath string) bool {
	rootPath, ok := normalizeRepositoryPath(rootPath)
	if !ok || ecosystem == "" {
		return false
	}
	if configPath != "" {
		configPath, ok = normalizeRepositoryPath(configPath)
		if !ok {
			return false
		}
	}

	key := vendorRootKey{path: rootPath, ecosystem: ecosystem}
	root := VendorRoot{Path: rootPath, Ecosystem: ecosystem, ConfigPath: configPath}
	if current, exists := d.roots[key]; !exists || preferConfigPath(configPath, current.ConfigPath) {
		d.roots[key] = root
	}
	return true
}

func preferConfigPath(candidate, current string) bool {
	return candidate != "" && (current == "" || candidate < current)
}

func (d *vendorDiscovery) addDependency(
	rootPath, evidencePath, ecosystem, name, version string,
) bool {
	rootPath, rootOK := normalizeRepositoryPath(rootPath)
	evidencePath, evidenceOK := normalizeRepositoryPath(evidencePath)
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	if !rootOK || !evidenceOK || ecosystem == "" || name == "" || version == "" {
		return false
	}

	dependency := VendoredDependency{
		Name:         name,
		Version:      version,
		Ecosystem:    ecosystem,
		Kind:         Vendor,
		PURL:         makePURL(ecosystem, name, version, ""),
		RootPath:     rootPath,
		EvidencePath: evidencePath,
	}
	key := vendoredDependencyKey{
		rootPath: rootPath, ecosystem: ecosystem, name: name, version: version, evidencePath: evidencePath,
	}
	d.dependencies[key] = dependency
	return true
}

func (d *vendorDiscovery) glob(pattern string) ([]string, error) {
	matches, err := d.reader.Glob(pattern)
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if normalized, ok := normalizeRepositoryPath(match); ok {
			set[normalized] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for match := range set {
		result = append(result, match)
	}
	sort.Strings(result)
	return result, nil
}

func (d *vendorDiscovery) globAll(patterns ...string) ([]string, []error) {
	set := make(map[string]struct{})
	var warnings []error
	for _, pattern := range patterns {
		matches, err := d.glob(pattern)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("discovering vendor files matching %q: %w", pattern, err))
			continue
		}
		for _, match := range matches {
			set[match] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for match := range set {
		result = append(result, match)
	}
	sort.Strings(result)
	return result, warnings
}

func (d *vendorDiscovery) result() VendorDiscovery {
	result := VendorDiscovery{
		Roots:        make([]VendorRoot, 0, len(d.roots)),
		Dependencies: make([]VendoredDependency, 0, len(d.dependencies)),
	}
	for _, root := range d.roots {
		result.Roots = append(result.Roots, root)
	}
	for _, dependency := range d.dependencies {
		result.Dependencies = append(result.Dependencies, dependency)
	}
	sort.Slice(result.Roots, func(i, j int) bool {
		if result.Roots[i].Path != result.Roots[j].Path {
			return result.Roots[i].Path < result.Roots[j].Path
		}
		if result.Roots[i].Ecosystem != result.Roots[j].Ecosystem {
			return result.Roots[i].Ecosystem < result.Roots[j].Ecosystem
		}
		return result.Roots[i].ConfigPath < result.Roots[j].ConfigPath
	})
	sort.Slice(result.Dependencies, func(i, j int) bool {
		left, right := result.Dependencies[i], result.Dependencies[j]
		if left.RootPath != right.RootPath {
			return left.RootPath < right.RootPath
		}
		if left.Ecosystem != right.Ecosystem {
			return left.Ecosystem < right.Ecosystem
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		if left.Version != right.Version {
			return left.Version < right.Version
		}
		return left.EvidencePath < right.EvidencePath
	})
	return result
}

func resolveRepositoryPath(baseDir, value string) (string, bool) {
	value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
	if value == "" || strings.HasPrefix(value, "/") || hasWindowsVolumePrefix(value) {
		return "", false
	}
	return normalizeRepositoryPath(path.Join(baseDir, value))
}

func hasWindowsVolumePrefix(value string) bool {
	if len(value) < 2 || value[1] != ':' {
		return false
	}
	first := value[0]
	return first >= 'A' && first <= 'Z' || first >= 'a' && first <= 'z'
}
