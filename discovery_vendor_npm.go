package manifests

import (
	"fmt"
	"strings"
)

func (d *vendorDiscovery) discoverNPMVendors() []error {
	manifests, warnings := d.globAll("**/node_modules/**/package.json")
	for _, manifestPath := range manifests {
		rootPath, ok := npmVendorRoot(manifestPath)
		if !ok {
			continue
		}
		d.addRoot(rootPath, "npm", "")

		content, err := d.reader.ReadFile(manifestPath)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("reading vendored npm package %s: %w", manifestPath, err))
			continue
		}
		result, err := Parse(manifestPath, content)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("parsing vendored npm package %s: %w", manifestPath, err))
			continue
		}
		if !d.addDependency(rootPath, manifestPath, "npm", result.Name, result.Version) {
			warnings = append(warnings, fmt.Errorf(
				"vendored npm package %s does not declare both name and version", manifestPath,
			))
		}
	}
	return warnings
}

func npmVendorRoot(manifestPath string) (string, bool) {
	parts := strings.Split(manifestPath, "/")
	nodeModules := -1
	for i, part := range parts {
		if part == "node_modules" {
			nodeModules = i
		}
	}
	if nodeModules < 0 {
		return "", false
	}

	remainder := parts[nodeModules+1:]
	validPackage := len(remainder) == 2 && remainder[1] == "package.json" ||
		len(remainder) == 3 && strings.HasPrefix(remainder[0], "@") && remainder[2] == "package.json"
	if !validPackage {
		return "", false
	}
	return strings.Join(parts[:nodeModules+1], "/"), true
}
