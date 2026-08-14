package manifests

import (
	"fmt"
	"regexp"
	"strings"
)

var goModuleVersion = regexp.MustCompile(
	`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`,
)

func (d *vendorDiscovery) discoverGoVendors() []error {
	inventories, warnings := d.globAll("**/vendor/modules.txt")
	for _, inventoryPath := range inventories {
		rootPath := strings.TrimSuffix(inventoryPath, "/modules.txt")
		if !d.addRoot(rootPath, "golang", inventoryPath) {
			warnings = append(warnings, fmt.Errorf("invalid Go vendor inventory path %q", inventoryPath))
			continue
		}

		content, err := d.reader.ReadFile(inventoryPath)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("reading Go vendor inventory %s: %w", inventoryPath, err))
			continue
		}
		for _, module := range parseGoVendorModules(content) {
			d.addDependency(rootPath, inventoryPath, "golang", module.name, module.version)
		}
	}
	return warnings
}

type goVendorModule struct {
	name    string
	version string
}

func parseGoVendorModules(content []byte) []goVendorModule {
	var modules []goVendorModule
	seen := make(map[goVendorModule]bool)
	var current goVendorModule
	var packagePrefix string
	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			current, packagePrefix = parseGoVendorModuleHeader(strings.TrimPrefix(line, "# "))
			continue
		}
		if packagePrefix == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 1 || fields[0] != packagePrefix && !strings.HasPrefix(fields[0], packagePrefix+"/") {
			continue
		}
		if !seen[current] {
			seen[current] = true
			modules = append(modules, current)
		}
	}
	return modules
}

// parseGoVendorModuleHeader parses a modules.txt "# module version" header,
// returning the vendored module identity and the import-path prefix used by
// package lines beneath it. A replace directive vendors the replacement code
// under the original import path, so the identity is the replacement while the
// prefix remains the original. Local-path replacements have no module identity.
func parseGoVendorModuleHeader(header string) (goVendorModule, string) {
	required, replacement, replaced := strings.Cut(header, "=>")
	fields := strings.Fields(required)
	if len(fields) == 0 {
		return goVendorModule{}, ""
	}
	prefix := fields[0]
	if replaced {
		fields = strings.Fields(replacement)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "./") || strings.HasPrefix(fields[0], "../") {
			return goVendorModule{}, ""
		}
	}
	if len(fields) < 2 || !goModuleVersion.MatchString(fields[1]) {
		return goVendorModule{}, ""
	}
	return goVendorModule{name: fields[0], version: fields[1]}, prefix
}
