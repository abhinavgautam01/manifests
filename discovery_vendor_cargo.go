package manifests

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

type cargoVendorSource struct {
	ReplaceWith string `toml:"replace-with"`
	Directory   string `toml:"directory"`
}

func (d *vendorDiscovery) discoverCargoVendors() []error {
	configs, warnings := d.globAll("**/.cargo/config", "**/.cargo/config.toml")
	configSet := make(map[string]bool, len(configs))
	for _, configPath := range configs {
		configSet[configPath] = true
	}
	for _, configPath := range configs {
		if !strings.HasSuffix(configPath, ".toml") && configSet[configPath+".toml"] {
			continue
		}
		content, err := d.reader.ReadFile(configPath)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("reading Cargo vendor configuration %s: %w", configPath, err))
			continue
		}
		var config struct {
			Sources map[string]cargoVendorSource `toml:"source"`
		}
		if _, err := toml.Decode(string(content), &config); err != nil {
			warnings = append(warnings, fmt.Errorf("parsing Cargo vendor configuration %s: %w", configPath, err))
			continue
		}

		directories, sourceWarnings := activeCargoVendorDirectories(config.Sources)
		for _, warning := range sourceWarnings {
			warnings = append(warnings, fmt.Errorf("parsing Cargo vendor configuration %s: %w", configPath, warning))
		}
		baseDir := path.Dir(path.Dir(configPath))
		for _, directory := range directories {
			rootPath, ok := resolveRepositoryPath(baseDir, directory)
			if !ok {
				warnings = append(warnings, fmt.Errorf(
					"cargo vendor configuration %s contains invalid directory path %q", configPath, directory,
				))
				continue
			}
			d.addRoot(rootPath, "cargo", configPath)
			warnings = append(warnings, d.discoverCargoPackages(rootPath)...)
		}
	}
	return warnings
}

func activeCargoVendorDirectories(sources map[string]cargoVendorSource) ([]string, []error) {
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)

	directorySet := make(map[string]struct{})
	var warnings []error
	for _, name := range names {
		target := sources[name].ReplaceWith
		seen := make(map[string]bool)
		for target != "" {
			if seen[target] {
				warnings = append(warnings, fmt.Errorf("source replacement cycle involving %q", target))
				break
			}
			seen[target] = true
			source, ok := sources[target]
			if !ok {
				warnings = append(warnings, fmt.Errorf("source %q replaces with unknown source %q", name, target))
				break
			}
			if source.Directory != "" {
				directorySet[source.Directory] = struct{}{}
				break
			}
			target = source.ReplaceWith
		}
	}
	directories := make([]string, 0, len(directorySet))
	for directory := range directorySet {
		directories = append(directories, directory)
	}
	sort.Strings(directories)
	return directories, warnings
}

func (d *vendorDiscovery) discoverCargoPackages(rootPath string) []error {
	markers, warnings := d.globAll(path.Join(rootPath, "*", ".cargo-checksum.json"))
	for _, markerPath := range markers {
		manifestPath := path.Join(path.Dir(markerPath), "Cargo.toml")
		content, err := d.reader.ReadFile(manifestPath)
		if err != nil {
			warnings = append(warnings, fmt.Errorf(
				"reading vendored Cargo package %s selected by %s: %w", manifestPath, markerPath, err,
			))
			continue
		}
		result, err := Parse(manifestPath, content)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("parsing vendored Cargo package %s: %w", manifestPath, err))
			continue
		}
		if !d.addDependency(rootPath, manifestPath, "cargo", result.Name, result.Version) {
			warnings = append(warnings, fmt.Errorf(
				"vendored Cargo package %s does not declare both name and version", manifestPath,
			))
		}
	}
	return warnings
}
