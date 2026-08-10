package manifests

import (
	"bytes"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

var pinnedPythonRequirement = regexp.MustCompile(
	`^([A-Za-z0-9_.-]+)(?:\[[^]]+\])?\s*(?:===|==)\s*([^\s;,\\=<>~]+)\s*(?:;|\\|#|$)`,
)

type pythonVendoringConfig struct {
	Destination  string `toml:"destination"`
	Requirements string `toml:"requirements"`
}

func (d *vendorDiscovery) discoverPythonVendors() []error {
	configs, warnings := d.globAll("**/pyproject.toml")
	for _, configPath := range configs {
		content, err := d.reader.ReadFile(configPath)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("reading Python vendor configuration %s: %w", configPath, err))
			continue
		}

		config, present, err := parsePythonVendoringConfig(content)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("parsing Python vendor configuration %s: %w", configPath, err))
			continue
		}
		if !present {
			continue
		}
		baseDir := path.Dir(configPath)
		rootPath, rootOK := resolveRepositoryPath(baseDir, config.Destination)
		requirementsPath, requirementsOK := resolveRepositoryPath(baseDir, config.Requirements)
		if !rootOK || !requirementsOK {
			warnings = append(warnings, fmt.Errorf(
				"python vendor configuration %s contains an invalid destination or requirements path", configPath,
			))
			continue
		}
		d.addRoot(rootPath, "pypi", configPath)

		requirements, err := d.reader.ReadFile(requirementsPath)
		if err != nil {
			warnings = append(warnings, fmt.Errorf(
				"reading Python vendor requirements %s selected by %s: %w",
				requirementsPath, configPath, err,
			))
			continue
		}
		for _, requirement := range parsePinnedPythonRequirements(requirements) {
			d.addDependency(rootPath, requirementsPath, "pypi", requirement.name, requirement.version)
		}
	}
	return warnings
}

func parsePythonVendoringConfig(content []byte) (pythonVendoringConfig, bool, error) {
	var document struct {
		Tool struct {
			Vendoring *pythonVendoringConfig `toml:"vendoring"`
		} `toml:"tool"`
	}
	if _, err := toml.Decode(string(content), &document); err != nil {
		if bytes.Contains(bytes.ToLower(content), []byte("vendoring")) {
			return pythonVendoringConfig{}, false, err
		}
		return pythonVendoringConfig{}, false, nil
	}
	if document.Tool.Vendoring == nil {
		return pythonVendoringConfig{}, false, nil
	}
	return *document.Tool.Vendoring, true, nil
}

type pythonVendorRequirement struct {
	name    string
	version string
}

func parsePinnedPythonRequirements(content []byte) []pythonVendorRequirement {
	var requirements []pythonVendorRequirement
	seen := make(map[pythonVendorRequirement]bool)
	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		match := pinnedPythonRequirement.FindStringSubmatch(line)
		if match == nil || strings.Contains(match[2], "*") {
			continue
		}
		requirement := pythonVendorRequirement{name: match[1], version: match[2]}
		if !seen[requirement] {
			seen[requirement] = true
			requirements = append(requirements, requirement)
		}
	}
	return requirements
}
