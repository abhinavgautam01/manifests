package opam

import (
	"slices"
	"testing"

	"github.com/git-pkgs/manifests/internal/core"
)

func TestParse(t *testing.T) {
	content := []byte(`opam-version: "2.0"
name: "example"
version: "1.2.3"
license: ["MIT" "ISC"]
depends: [
  "ocaml" {>= "5.0"}
  # "commented-out"
  "dune" {build}
]
`)

	result, err := (&parser{}).Parse("example.opam", content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if result.Name != "example" {
		t.Errorf("Name = %q, want example", result.Name)
	}
	if result.Version != "1.2.3" {
		t.Errorf("Version = %q, want 1.2.3", result.Version)
	}
	if !slices.Equal(result.Licenses, []string{"MIT", "ISC"}) {
		t.Errorf("Licenses = %q, want [MIT ISC]", result.Licenses)
	}
	if len(result.Dependencies) != 2 {
		t.Fatalf("Dependencies = %d, want 2", len(result.Dependencies))
	}
	want := []core.Dependency{
		{Name: "ocaml", Scope: core.Runtime, Direct: true},
		{Name: "dune", Scope: core.Runtime, Direct: true},
	}
	if !slices.Equal(result.Dependencies, want) {
		t.Errorf("Dependencies = %#v, want %#v", result.Dependencies, want)
	}
}

func TestScalarLicense(t *testing.T) {
	result, err := (&parser{}).Parse("opam", []byte(`license: "Apache-2.0"`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !slices.Equal(result.Licenses, []string{"Apache-2.0"}) {
		t.Errorf("Licenses = %q, want [Apache-2.0]", result.Licenses)
	}
}
