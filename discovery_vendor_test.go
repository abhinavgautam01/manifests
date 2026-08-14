package manifests

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestDiscoverVendorsNPM(t *testing.T) {
	reader := mapFSReader(map[string]string{
		"node_modules/@scope/pkg/package.json":                   `{"name":"@scope/pkg","version":"2.1.0"}`,
		"node_modules/left-pad/examples/package.json":            `{"name":"example","version":"1.0.0"}`,
		"node_modules/left-pad/node_modules/repeat/package.json": `{"name":"repeat","version":"1.2.3"}`,
		"node_modules/left-pad/package.json":                     `{"name":"left-pad","version":"1.3.0"}`,
	})

	got, warnings := DiscoverVendors(reader)
	if len(warnings) != 0 {
		t.Fatalf("DiscoverVendors warnings: %v", warnings)
	}
	want := VendorDiscovery{
		Roots: []VendorRoot{
			{Path: "node_modules", Ecosystem: "npm"},
			{Path: "node_modules/left-pad/node_modules", Ecosystem: "npm"},
		},
		Dependencies: []VendoredDependency{
			{
				Name: "@scope/pkg", Version: "2.1.0", Ecosystem: "npm", Kind: Vendor,
				PURL: makePURL("npm", "@scope/pkg", "2.1.0", ""), RootPath: "node_modules",
				EvidencePath: "node_modules/@scope/pkg/package.json",
			},
			{
				Name: "left-pad", Version: "1.3.0", Ecosystem: "npm", Kind: Vendor,
				PURL: makePURL("npm", "left-pad", "1.3.0", ""), RootPath: "node_modules",
				EvidencePath: "node_modules/left-pad/package.json",
			},
			{
				Name: "repeat", Version: "1.2.3", Ecosystem: "npm", Kind: Vendor,
				PURL: makePURL("npm", "repeat", "1.2.3", ""), RootPath: "node_modules/left-pad/node_modules",
				EvidencePath: "node_modules/left-pad/node_modules/repeat/package.json",
			},
		},
	}
	if !slices.Equal(got.Roots, want.Roots) || !slices.Equal(got.Dependencies, want.Dependencies) {
		t.Fatalf("DiscoverVendors() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestDiscoverVendorsGo(t *testing.T) {
	reader := mapFSReader(map[string]string{
		"vendor/modules.txt": `# github.com/acme/lib v1.2.3
## explicit; go 1.22
github.com/acme/lib
# example.com/original v1.0.0 => example.com/fork v1.1.0
example.com/original/pkg
# example.com/local => ./local
example.com/local
`,
		"services/api/vendor/modules.txt": `# golang.org/x/text v0.22.0
## explicit; go 1.23
golang.org/x/text/language
`,
	})

	got, warnings := DiscoverVendors(reader)
	if len(warnings) != 0 {
		t.Fatalf("DiscoverVendors warnings: %v", warnings)
	}
	want := VendorDiscovery{
		Roots: []VendorRoot{
			{Path: "services/api/vendor", Ecosystem: "golang", ConfigPath: "services/api/vendor/modules.txt"},
			{Path: "vendor", Ecosystem: "golang", ConfigPath: "vendor/modules.txt"},
		},
		Dependencies: []VendoredDependency{
			{
				Name: "golang.org/x/text", Version: "v0.22.0", Ecosystem: "golang", Kind: Vendor,
				PURL:     makePURL("golang", "golang.org/x/text", "v0.22.0", ""),
				RootPath: "services/api/vendor", EvidencePath: "services/api/vendor/modules.txt",
			},
			{
				Name: "example.com/original", Version: "v1.0.0", Ecosystem: "golang", Kind: Vendor,
				PURL:     makePURL("golang", "example.com/original", "v1.0.0", ""),
				RootPath: "vendor", EvidencePath: "vendor/modules.txt",
			},
			{
				Name: "github.com/acme/lib", Version: "v1.2.3", Ecosystem: "golang", Kind: Vendor,
				PURL:     makePURL("golang", "github.com/acme/lib", "v1.2.3", ""),
				RootPath: "vendor", EvidencePath: "vendor/modules.txt",
			},
		},
	}
	if !slices.Equal(got.Roots, want.Roots) || !slices.Equal(got.Dependencies, want.Dependencies) {
		t.Fatalf("DiscoverVendors() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestDiscoverVendorsGoSkipsModulesWithoutPackages(t *testing.T) {
	reader := mapFSReader(map[string]string{
		"vendor/modules.txt": `# example.com/unused v1.0.0 => ./unused
# example.com/invalid not-a-version
example.com/invalid/pkg
# example.com/present v1.2.3
## explicit; go 1.24
example.com/present/pkg
# example.com/also-unused v2.0.0
## explicit; go 1.24
`,
	})

	got, warnings := DiscoverVendors(reader)
	if len(warnings) != 0 {
		t.Fatalf("DiscoverVendors warnings: %v", warnings)
	}
	want := []VendoredDependency{{
		Name: "example.com/present", Version: "v1.2.3", Ecosystem: "golang", Kind: Vendor,
		PURL: makePURL("golang", "example.com/present", "v1.2.3", ""), RootPath: "vendor",
		EvidencePath: "vendor/modules.txt",
	}}
	if !slices.Equal(got.Dependencies, want) {
		t.Fatalf("DiscoverVendors dependencies = %+v, want %+v", got.Dependencies, want)
	}
}

func TestDiscoverVendorsPython(t *testing.T) {
	reader := mapFSReader(map[string]string{
		"tools/pyproject.toml": `[tool.vendoring]
destination = "src/example/_vendor/"
requirements = "src/example/_vendor/vendor.txt"
`,
		"tools/src/example/_vendor/vendor.txt": `# Generated file
requests==2.32.3
idna[security] === 3.10 ; python_version >= "3.9"
urllib3>=2.0
charset-normalizer==3.4.*,!=3.4.2
typing-extensions==4.12.*
invalid====version
--only-binary :all:
requests==2.32.3
`,
	})

	got, warnings := DiscoverVendors(reader)
	if len(warnings) != 0 {
		t.Fatalf("DiscoverVendors warnings: %v", warnings)
	}
	want := VendorDiscovery{
		Roots: []VendorRoot{{
			Path: "tools/src/example/_vendor", Ecosystem: "pypi", ConfigPath: "tools/pyproject.toml",
		}},
		Dependencies: []VendoredDependency{
			{
				Name: "idna", Version: "3.10", Ecosystem: "pypi", Kind: Vendor,
				PURL: makePURL("pypi", "idna", "3.10", ""), RootPath: "tools/src/example/_vendor",
				EvidencePath: "tools/src/example/_vendor/vendor.txt",
			},
			{
				Name: "requests", Version: "2.32.3", Ecosystem: "pypi", Kind: Vendor,
				PURL: makePURL("pypi", "requests", "2.32.3", ""), RootPath: "tools/src/example/_vendor",
				EvidencePath: "tools/src/example/_vendor/vendor.txt",
			},
		},
	}
	if !slices.Equal(got.Roots, want.Roots) || !slices.Equal(got.Dependencies, want.Dependencies) {
		t.Fatalf("DiscoverVendors() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestDiscoverVendorsPythonWarnsForDirectReferences(t *testing.T) {
	reader := mapFSReader(map[string]string{
		"pyproject.toml": `[tool.vendoring]
destination = "_vendor"
requirements = "vendor.txt"
`,
		"vendor.txt": `requests==2.32.3
archspec @ git+https://github.com/archspec/archspec.git@4a8cb2a1c7d8e264c0a391f4d2d6b4235b3201b5
`,
	})

	got, warnings := DiscoverVendors(reader)
	if len(warnings) != 1 || !strings.Contains(warnings[0].Error(), "direct reference") {
		t.Fatalf("DiscoverVendors warnings = %v, want unsupported direct reference warning", warnings)
	}
	want := []VendoredDependency{{
		Name: "requests", Version: "2.32.3", Ecosystem: "pypi", Kind: Vendor,
		PURL: makePURL("pypi", "requests", "2.32.3", ""), RootPath: "_vendor",
		EvidencePath: "vendor.txt",
	}}
	if !slices.Equal(got.Dependencies, want) {
		t.Fatalf("DiscoverVendors dependencies = %+v, want %+v", got.Dependencies, want)
	}
}

func TestDiscoverVendorsCargo(t *testing.T) {
	reader := mapFSReader(map[string]string{
		"project/.cargo/config.toml": `[source.crates-io]
replace-with = "company-mirror"

[source.company-mirror]
replace-with = "vendored-sources"

[source.vendored-sources]
directory = "vendor"

[source.unused]
directory = "unused-vendor"
`,
		"project/vendor/serde-1.0.217/.cargo-checksum.json": `{}`,
		"project/vendor/serde-1.0.217/Cargo.toml": `[package]
name = "serde"
version = "1.0.217"
`,
		"project/unused-vendor/ignored-1.0.0/.cargo-checksum.json": `{}`,
		"project/unused-vendor/ignored-1.0.0/Cargo.toml": `[package]
name = "ignored"
version = "1.0.0"
`,
	})

	got, warnings := DiscoverVendors(reader)
	if len(warnings) != 0 {
		t.Fatalf("DiscoverVendors warnings: %v", warnings)
	}
	want := VendorDiscovery{
		Roots: []VendorRoot{{
			Path: "project/vendor", Ecosystem: "cargo", ConfigPath: "project/.cargo/config.toml",
		}},
		Dependencies: []VendoredDependency{{
			Name: "serde", Version: "1.0.217", Ecosystem: "cargo", Kind: Vendor,
			PURL: makePURL("cargo", "serde", "1.0.217", ""), RootPath: "project/vendor",
			EvidencePath: "project/vendor/serde-1.0.217/Cargo.toml",
		}},
	}
	if !slices.Equal(got.Roots, want.Roots) || !slices.Equal(got.Dependencies, want.Dependencies) {
		t.Fatalf("DiscoverVendors() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestDiscoverVendorsCargoPrefersExtensionlessConfigAtRoot(t *testing.T) {
	reader := mapFSReader(map[string]string{
		".cargo/config": `[source.crates-io]
replace-with = "legacy"
[source.legacy]
directory = "legacy-vendor"
`,
		".cargo/config.toml": `[source.crates-io]
replace-with = "current"
[source.current]
directory = "vendor"
`,
		"legacy-vendor/old-1.0.0/.cargo-checksum.json": `{}`,
		"legacy-vendor/old-1.0.0/Cargo.toml":           "[package]\nname = \"old\"\nversion = \"1.0.0\"\n",
		"vendor/current-2.0.0/.cargo-checksum.json":    `{}`,
		"vendor/current-2.0.0/Cargo.toml":              "[package]\nname = \"current\"\nversion = \"2.0.0\"\n",
	})

	got, warnings := DiscoverVendors(reader)
	if len(warnings) != 0 {
		t.Fatalf("DiscoverVendors warnings: %v", warnings)
	}
	wantRoots := []VendorRoot{{Path: "legacy-vendor", Ecosystem: "cargo", ConfigPath: ".cargo/config"}}
	if !slices.Equal(got.Roots, wantRoots) {
		t.Fatalf("DiscoverVendors roots = %+v, want %+v", got.Roots, wantRoots)
	}
	if len(got.Dependencies) != 1 || got.Dependencies[0].Name != "old" {
		t.Fatalf("DiscoverVendors dependencies = %+v, want only old", got.Dependencies)
	}
}

func TestDiscoverVendorsCargoMergesAncestorConfiguration(t *testing.T) {
	reader := mapFSReader(map[string]string{
		".cargo/config.toml": `[source.vendored-sources]
directory = "third_party/vendor"
`,
		"apps/api/.cargo/config.toml": `[source.crates-io]
replace-with = "vendored-sources"
`,
		"third_party/vendor/serde-1.0.217/.cargo-checksum.json": `{}`,
		"third_party/vendor/serde-1.0.217/Cargo.toml": `[package]
name = "serde"
version = "1.0.217"
`,
	})

	got, warnings := DiscoverVendors(reader)
	if len(warnings) != 0 {
		t.Fatalf("DiscoverVendors warnings: %v", warnings)
	}
	want := VendorDiscovery{
		Roots: []VendorRoot{{
			Path:       "third_party/vendor",
			Ecosystem:  "cargo",
			ConfigPath: "apps/api/.cargo/config.toml",
		}},
		Dependencies: []VendoredDependency{{
			Name: "serde", Version: "1.0.217", Ecosystem: "cargo", Kind: Vendor,
			PURL: makePURL("cargo", "serde", "1.0.217", ""), RootPath: "third_party/vendor",
			EvidencePath: "third_party/vendor/serde-1.0.217/Cargo.toml",
		}},
	}
	if !slices.Equal(got.Roots, want.Roots) || !slices.Equal(got.Dependencies, want.Dependencies) {
		t.Fatalf("DiscoverVendors() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestDiscoverVendorsReturnsPartialResultsWithWarnings(t *testing.T) {
	reader := mapFSReader(map[string]string{
		"node_modules/good/package.json":       `{"name":"good","version":"1.0.0"}`,
		"node_modules/incomplete/package.json": `{"name":"incomplete"}`,
		"pyproject.toml": `[tool.vendoring]
destination = "_vendor"
requirements = "missing.txt"
`,
		"nested/pyproject.toml": `[tool.vendoring]
destination = "../../outside"
requirements = "vendor.txt"
`,
	})

	got, warnings := DiscoverVendors(reader)
	if len(warnings) != 3 {
		t.Fatalf("DiscoverVendors warnings = %v, want three", warnings)
	}
	for _, want := range []string{
		"does not declare both name and version",
		"contains an invalid destination or requirements path",
		"reading Python vendor requirements missing.txt",
	} {
		if !containsError(warnings, want) {
			t.Errorf("DiscoverVendors warnings = %v, want one containing %q", warnings, want)
		}
	}
	wantRoot := VendorRoot{Path: "_vendor", Ecosystem: "pypi", ConfigPath: "pyproject.toml"}
	if !slices.Contains(got.Roots, wantRoot) {
		t.Errorf("DiscoverVendors roots = %+v, want %+v", got.Roots, wantRoot)
	}
	wantDependency := VendoredDependency{
		Name: "good", Version: "1.0.0", Ecosystem: "npm", Kind: Vendor,
		PURL: makePURL("npm", "good", "1.0.0", ""), RootPath: "node_modules",
		EvidencePath: "node_modules/good/package.json",
	}
	if !slices.Contains(got.Dependencies, wantDependency) {
		t.Errorf("DiscoverVendors dependencies = %+v, want %+v", got.Dependencies, wantDependency)
	}
}

func TestDiscoverVendorsReaderWarnings(t *testing.T) {
	wantErr := errors.New("glob failed")
	_, warnings := DiscoverVendors(errorRepositoryReader{err: wantErr})
	if len(warnings) != 5 {
		t.Fatalf("DiscoverVendors warnings = %v, want five glob warnings", warnings)
	}
	for _, warning := range warnings {
		if !errors.Is(warning, wantErr) {
			t.Fatalf("DiscoverVendors warning = %v, want wrapped %v", warning, wantErr)
		}
	}
}

func TestDiscoverVendorsNilReader(t *testing.T) {
	got, warnings := DiscoverVendors(nil)
	if len(got.Roots) != 0 || len(got.Dependencies) != 0 {
		t.Fatalf("DiscoverVendors(nil) = %+v, want empty result", got)
	}
	if len(warnings) != 1 || warnings[0].Error() != "nil repository reader" {
		t.Fatalf("DiscoverVendors(nil) warnings = %v", warnings)
	}
}

func containsError(errs []error, text string) bool {
	return slices.ContainsFunc(errs, func(err error) bool {
		return strings.Contains(err.Error(), text)
	})
}
