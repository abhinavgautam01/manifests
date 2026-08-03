package elm

import (
	"os"
	"testing"

	"github.com/git-pkgs/manifests/internal/core"
)

func TestElmPackageJSON(t *testing.T) {
	content, err := os.ReadFile("../../testdata/elm/elm-package.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	parser := &elmPackageJSONParser{}
	res, err := parser.Parse("elm-package.json", content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(res.Dependencies) != 4 {
		t.Fatalf("expected 4 dependencies, got %d", len(res.Dependencies))
	}

	depMap := make(map[string]core.Dependency)
	for _, d := range res.Dependencies {
		depMap[d.Name] = d
	}

	// All 4 packages with versions
	expected := map[string]string{
		"elm-lang/core":        "1.0.0 <= v < 2.0.0",
		"evancz/elm-markdown":  "1.1.0 <= v < 2.0.0",
		"evancz/elm-html":      "1.0.0 <= v < 2.0.0",
		"evancz/local-channel": "1.0.0 <= v < 2.0.0",
	}

	for name, wantVer := range expected {
		dep, ok := depMap[name]
		if !ok {
			t.Errorf("expected %s dependency", name)
			continue
		}
		if dep.Version != wantVer {
			t.Errorf("%s version = %q, want %q", name, dep.Version, wantVer)
		}
		if !dep.Direct {
			t.Errorf("%s should be direct dependency", name)
		}
	}
}

func TestElmLegacyJSON(t *testing.T) {
	content, err := os.ReadFile("../../testdata/elm/elm_dependencies.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	// Legacy format is similar to elm-package.json
	parser := &elmPackageJSONParser{}
	res, err := parser.Parse("elm-package.json", content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(res.Dependencies) != 2 {
		t.Fatalf("expected 2 dependencies, got %d", len(res.Dependencies))
	}

	depMap := make(map[string]core.Dependency)
	for _, d := range res.Dependencies {
		depMap[d.Name] = d
	}

	// All 2 packages with versions
	expected := map[string]string{
		"johnpmayer/elm-webgl":          "0.1.1",
		"johnpmayer/elm-linear-algebra": "1.0.1",
	}

	for name, wantVer := range expected {
		dep, ok := depMap[name]
		if !ok {
			t.Errorf("expected %s dependency", name)
			continue
		}
		if dep.Version != wantVer {
			t.Errorf("%s version = %q, want %q", name, dep.Version, wantVer)
		}
	}
}

func TestElmJSONPackageDependencies(t *testing.T) {
	content := []byte(`{
		"type": "package",
		"name": "author/example",
		"version": "1.0.0",
		"license": "BSD-3-Clause",
		"dependencies": {
			"elm/core": "1.0.0 <= v < 2.0.0"
		},
		"test-dependencies": {
			"elm/json": "1.0.0 <= v < 2.0.0"
		}
	}`)

	result, err := (&elmJSONParser{}).Parse("elm.json", content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(result.Licenses) != 1 || result.Licenses[0] != "BSD-3-Clause" {
		t.Errorf("Licenses = %q, want [BSD-3-Clause]", result.Licenses)
	}
	if len(result.Dependencies) != 2 {
		t.Fatalf("Dependencies = %d, want 2", len(result.Dependencies))
	}
	for _, dependency := range result.Dependencies {
		if !dependency.Direct {
			t.Errorf("%s should be direct", dependency.Name)
		}
		switch dependency.Name {
		case "elm/core":
			if dependency.Scope != core.Runtime {
				t.Errorf("elm/core scope = %q, want runtime", dependency.Scope)
			}
		case "elm/json":
			if dependency.Scope != core.Test {
				t.Errorf("elm/json scope = %q, want test", dependency.Scope)
			}
		default:
			t.Errorf("unexpected dependency %q", dependency.Name)
		}
	}
}

func TestElmJSONApplicationDependencies(t *testing.T) {
	content := []byte(`{
		"type": "application",
		"dependencies": {
			"direct": {"elm/core": "1.0.5"},
			"indirect": {"elm/json": "1.1.3"}
		},
		"test-dependencies": {
			"direct": {"elm-explorations/test": "2.2.0"},
			"indirect": {}
		}
	}`)

	result, err := (&elmJSONParser{}).Parse("elm.json", content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(result.Dependencies) != 3 {
		t.Fatalf("Dependencies = %d, want 3", len(result.Dependencies))
	}
	dependencies := make(map[string]core.Dependency)
	for _, dependency := range result.Dependencies {
		dependencies[dependency.Name] = dependency
	}
	if dependency := dependencies["elm/core"]; !dependency.Direct || dependency.Scope != core.Runtime {
		t.Errorf("elm/core = %#v", dependency)
	}
	if dependency := dependencies["elm/json"]; dependency.Direct || dependency.Scope != core.Runtime {
		t.Errorf("elm/json = %#v", dependency)
	}
	if dependency := dependencies["elm-explorations/test"]; !dependency.Direct || dependency.Scope != core.Test {
		t.Errorf("elm-explorations/test = %#v", dependency)
	}
}
