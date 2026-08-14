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
	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			current = goVendorModule{}
			fields := strings.Fields(strings.TrimPrefix(line, "# "))
			if len(fields) < 2 || fields[0] == "=>" || fields[1] == "=>" || !goModuleVersion.MatchString(fields[1]) {
				continue
			}
			current = goVendorModule{name: fields[0], version: fields[1]}
			continue
		}
		if current.name == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 1 || fields[0] != current.name && !strings.HasPrefix(fields[0], current.name+"/") {
			continue
		}
		if !seen[current] {
			seen[current] = true
			modules = append(modules, current)
		}
	}
	return modules
}
