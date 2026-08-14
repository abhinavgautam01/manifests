package manifests

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

type cargoVendorSource struct {
	ReplaceWith         string `toml:"replace-with"`
	Directory           string `toml:"directory"`
	replaceConfigPath   string
	directoryConfigPath string
}

type cargoVendorConfig struct {
	path    string
	baseDir string
	sources map[string]cargoVendorSource
}

type cargoVendorDirectory struct {
	path           string
	configPath     string
	definitionPath string
}

func (d *vendorDiscovery) discoverCargoVendors() []error {
	configs, warnings := d.globAll("**/.cargo/config", "**/.cargo/config.toml")
	configSet := make(map[string]bool, len(configs))
	for _, configPath := range configs {
		configSet[configPath] = true
	}
	parsedConfigs := make([]cargoVendorConfig, 0, len(configs))
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
		for name, source := range config.Sources {
			if source.ReplaceWith != "" {
				source.replaceConfigPath = configPath
			}
			if source.Directory != "" {
				source.directoryConfigPath = configPath
			}
			config.Sources[name] = source
		}
		parsedConfigs = append(parsedConfigs, cargoVendorConfig{
			path: configPath, baseDir: cargoConfigBaseDir(configPath), sources: config.Sources,
		})
	}
	sort.Slice(parsedConfigs, func(i, j int) bool {
		leftDepth := strings.Count(parsedConfigs[i].baseDir, "/")
		rightDepth := strings.Count(parsedConfigs[j].baseDir, "/")
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		return parsedConfigs[i].path < parsedConfigs[j].path
	})

	for _, config := range parsedConfigs {
		sources := mergedCargoVendorSources(parsedConfigs, config.baseDir)
		directories, sourceWarnings := activeCargoVendorDirectories(sources)
		for _, warning := range sourceWarnings {
			warnings = append(warnings, fmt.Errorf("parsing Cargo vendor configuration %s: %w", config.path, warning))
		}
		for _, directory := range directories {
			rootPath, ok := resolveRepositoryPath(cargoConfigBaseDir(directory.definitionPath), directory.path)
			if !ok {
				warnings = append(warnings, fmt.Errorf(
					"cargo vendor configuration %s contains invalid directory path %q",
					directory.definitionPath, directory.path,
				))
				continue
			}
			d.addRoot(rootPath, "cargo", directory.configPath)
			warnings = append(warnings, d.discoverCargoPackages(rootPath)...)
		}
	}
	return warnings
}

func cargoConfigBaseDir(configPath string) string {
	return path.Dir(path.Dir(configPath))
}

func mergedCargoVendorSources(configs []cargoVendorConfig, baseDir string) map[string]cargoVendorSource {
	sources := make(map[string]cargoVendorSource)
	for _, config := range configs {
		if config.baseDir != "." && config.baseDir != baseDir && !strings.HasPrefix(baseDir, config.baseDir+"/") {
			continue
		}
		for name, candidate := range config.sources {
			source := sources[name]
			if candidate.ReplaceWith != "" {
				source.ReplaceWith = candidate.ReplaceWith
				source.replaceConfigPath = candidate.replaceConfigPath
			}
			if candidate.Directory != "" {
				source.Directory = candidate.Directory
				source.directoryConfigPath = candidate.directoryConfigPath
			}
			sources[name] = source
		}
	}
	return sources
}

func activeCargoVendorDirectories(sources map[string]cargoVendorSource) ([]cargoVendorDirectory, []error) {
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)

	directorySet := make(map[cargoVendorDirectory]struct{})
	var warnings []error
	for _, name := range names {
		selector := sources[name]
		target := selector.ReplaceWith
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
				directorySet[cargoVendorDirectory{
					path:           source.Directory,
					configPath:     selector.replaceConfigPath,
					definitionPath: source.directoryConfigPath,
				}] = struct{}{}
				break
			}
			target = source.ReplaceWith
		}
	}
	directories := make([]cargoVendorDirectory, 0, len(directorySet))
	for directory := range directorySet {
		directories = append(directories, directory)
	}
	sort.Slice(directories, func(i, j int) bool {
		if directories[i].path != directories[j].path {
			return directories[i].path < directories[j].path
		}
		if directories[i].definitionPath != directories[j].definitionPath {
			return directories[i].definitionPath < directories[j].definitionPath
		}
		return directories[i].configPath < directories[j].configPath
	})
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
