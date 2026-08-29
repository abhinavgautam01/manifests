package chef

import (
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/git-pkgs/manifests/internal/core"
)

func TestMetadataRubyParser(t *testing.T) {
	t.Parallel()

	content := readChefFixture(t, "../../testdata/chef/metadata.rb")
	result, err := (&metadataRubyParser{}).Parse("metadata.rb", content)
	if err != nil {
		t.Fatal(err)
	}
	assertChefIdentity(t, result)
	assertChefDependencies(t, result, map[string]string{
		"apt":   "",
		"ntp":   "~> 3.0",
		"users": ">= 5.0, < 9.0",
	})
}

func TestMetadataJSONParser(t *testing.T) {
	t.Parallel()

	content := readChefFixture(t, "../../testdata/chef/metadata.json")
	result, err := (&metadataJSONParser{}).Parse("metadata.json", content)
	if err != nil {
		t.Fatal(err)
	}
	assertChefIdentity(t, result)
	assertChefDependencies(t, result, map[string]string{
		"apt":   "",
		"ntp":   "~> 3.0",
		"users": ">= 5.0, < 9.0",
	})
	if got := dependencyNames(result.Dependencies); !slices.Equal(got, []string{"apt", "ntp", "users"}) {
		t.Errorf("dependency order = %v, want sorted JSON object keys", got)
	}
}

func TestMetadataJSONParserSkipsNonliteralFields(t *testing.T) {
	t.Parallel()

	content := []byte(`{
  "name": {"dynamic": true},
  "version": 2,
  "license": ["MIT"],
  "dependencies": {
    "valid": ">= 1.0",
    "also-valid": [">= 2.0", "< 3.0"],
    "invalid": {"constraint": "~> 1.0"}
  }
}`)
	result, err := (&metadataJSONParser{}).Parse("metadata.json", content)
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "" || result.Version != "" || len(result.Licenses) != 0 {
		t.Errorf("identity = %q %q %v, want empty", result.Name, result.Version, result.Licenses)
	}
	assertChefDependencies(t, result, map[string]string{
		"also-valid": ">= 2.0, < 3.0",
		"valid":      ">= 1.0",
	})
}

func TestMetadataJSONParserRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	_, err := (&metadataJSONParser{}).Parse("metadata.json", []byte(`{"name":`))
	var parseError *core.ParseError
	if !errors.As(err, &parseError) {
		t.Fatalf("error = %v, want ParseError", err)
	}
}

func TestMetadataRubyParserSkipsDynamicExpressions(t *testing.T) {
	t.Parallel()

	content := []byte(`
name cookbook_name
version(version_from_env)
license "MIT-#{suffix}"
license "MIT-#@suffix"
name "#$cookbook_name"
depends dependency_name
depends "concatenated" + suffix
depends("method", requirement())
depends "interpolated-#{name}"
depends "unterminated

name 'literal_name'
version "1.2.3"
license('MIT')
depends "literal_dependency", "~> 4.0" # retained after dynamic lines
`)
	result, err := (&metadataRubyParser{}).Parse("metadata.rb", content)
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "literal_name" || result.Version != "1.2.3" ||
		!slices.Equal(result.Licenses, []string{"MIT"}) {
		t.Errorf("identity = %q %q %v", result.Name, result.Version, result.Licenses)
	}
	assertChefDependencies(t, result, map[string]string{"literal_dependency": "~> 4.0"})
}

func TestBerksfileParser(t *testing.T) {
	t.Parallel()

	content := readChefFixture(t, "../../testdata/chef/Berksfile")
	result, err := (&berksfileParser{}).Parse("Berksfile", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sources) != 2 {
		t.Fatalf("sources = %+v, want 2", result.Sources)
	}
	if result.Sources[0].Kind != core.SourceRegistry ||
		result.Sources[0].Value != "https://supermarket.chef.io" ||
		result.Sources[1].Value != "https://supermarket.example.test" {
		t.Errorf("sources = %+v, want ordered public and private registries", result.Sources)
	}
	assertChefDependencies(t, result, map[string]string{
		"ntp":             "<= 1.0.0",
		"mysql":           "",
		"company_base":    "",
		"local_users":     "",
		"github_cookbook": "",
	})

	dependencies := indexChefDependencies(result.Dependencies)
	assertChefSource(t, dependencies["company_base"].Source, core.SourceGit,
		"https://github.com/example/company_base.git")
	assertChefSource(t, dependencies["local_users"].Source, core.SourcePath,
		"../local_users")
	assertChefSource(t, dependencies["github_cookbook"].Source, core.SourceGitHub,
		"example/github_cookbook")
	if dependencies["ntp"].Source.Kind != "" || dependencies["mysql"].Source.Kind != "" {
		t.Errorf("registry dependencies claim explicit sources: %+v", dependencies)
	}
	for _, declaration := range result.Declarations {
		dependency := dependencies[declaration.Name]
		if declaration.Source.Kind != dependency.Source.Kind || declaration.Source.Value != dependency.Source.Value {
			t.Errorf("declaration source = %+v, dependency source = %+v", declaration.Source, dependency.Source)
		}
	}
}

func TestBerksfileParserSupportsHashRocketAndLiteralSourceOptions(t *testing.T) {
	t.Parallel()

	content := []byte(`
source "https://private.example.test", ssl_verify: "false"
cookbook 'legacy', :git => 'https://example.test/legacy.git', :ref => 'abc123'
`)
	result, err := (&berksfileParser{}).Parse("Berksfile", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sources) != 1 || result.Sources[0].Value != "https://private.example.test" {
		t.Fatalf("sources = %+v", result.Sources)
	}
	if len(result.Dependencies) != 1 {
		t.Fatalf("dependencies = %+v", result.Dependencies)
	}
	assertChefSource(t, result.Dependencies[0].Source, core.SourceGit,
		"https://example.test/legacy.git")
}

func TestBerksfileParserSkipsDynamicCallsAndMetadata(t *testing.T) {
	t.Parallel()

	content := []byte(`
source ENV.fetch("CHEF_SOURCE")
source "https://#{host}"
cookbook cookbook_name
cookbook "dynamic-git", git: repository_url
cookbook "ambiguous", git: "https://example.test/a.git", path: "../a"
metadata
metadata path: './cookbook'

source "https://literal.example.test"
cookbook "literal", path: "../literal"
`)
	result, err := (&berksfileParser{}).Parse("Berksfile", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sources) != 1 || result.Sources[0].Value != "https://literal.example.test" {
		t.Errorf("sources = %+v", result.Sources)
	}
	assertChefDependencies(t, result, map[string]string{"literal": ""})
}

func TestRubyStringsPreserveLiteralHashesAndEscapes(t *testing.T) {
	t.Parallel()

	content := []byte(`
depends 'hash#name', '~> 1.0' # comment
depends "escaped\#{name}", "line\nconstraint"
depends 'single\q', '>= 2.0'
`)
	result, err := (&metadataRubyParser{}).Parse("metadata.rb", content)
	if err != nil {
		t.Fatal(err)
	}
	assertChefDependencies(t, result, map[string]string{
		"hash#name":      "~> 1.0",
		"escaped#{name}": "line\nconstraint",
		"single\\q":      ">= 2.0",
	})
}

func readChefFixture(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func assertChefIdentity(t *testing.T, result *core.Result) {
	t.Helper()
	if result.Name != "example_cookbook" || result.Version != "2.4.1" ||
		!slices.Equal(result.Licenses, []string{"Apache-2.0"}) {
		t.Errorf("identity = %q %q %v, want example_cookbook 2.4.1 Apache-2.0",
			result.Name, result.Version, result.Licenses)
	}
}

func assertChefDependencies(t *testing.T, result *core.Result, want map[string]string) {
	t.Helper()
	if len(result.Dependencies) != len(want) {
		t.Fatalf("dependencies = %+v, want %v", result.Dependencies, want)
	}
	if len(result.Declarations) != len(want) {
		t.Fatalf("declarations = %+v, want %d", result.Declarations, len(want))
	}
	seen := make(map[string]bool, len(result.Dependencies))
	for _, dependency := range result.Dependencies {
		version, ok := want[dependency.Name]
		if !ok {
			t.Errorf("unexpected dependency %+v", dependency)
			continue
		}
		if seen[dependency.Name] {
			t.Errorf("duplicate dependency %q", dependency.Name)
		}
		seen[dependency.Name] = true
		if dependency.Version != version || dependency.Scope != core.Runtime || !dependency.Direct ||
			dependency.RegistryURL != "" {
			t.Errorf("dependency = %+v, want version %q direct runtime without registry", dependency, version)
		}
	}
}

func indexChefDependencies(dependencies []core.Dependency) map[string]core.Dependency {
	indexed := make(map[string]core.Dependency, len(dependencies))
	for _, dependency := range dependencies {
		indexed[dependency.Name] = dependency
	}
	return indexed
}

func dependencyNames(dependencies []core.Dependency) []string {
	names := make([]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		names = append(names, dependency.Name)
	}
	return names
}

func assertChefSource(
	t *testing.T,
	source core.Source,
	kind core.SourceKind,
	value string,
) {
	t.Helper()
	if source.Kind != kind || source.Value != value {
		t.Errorf("source = %+v, want kind %q value %q", source, kind, value)
	}
}
