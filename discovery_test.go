package manifests

import (
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestDiscoverManifestsRootAndKnownPaths(t *testing.T) {
	reader := mapFSReader(map[string]string{
		".github/workflows/ci.yaml": `jobs: {}`,
		".github/workflows/ci.yml":  `jobs: {}`,
		".github/workflows/readme":  "ignored",
		".gitmodules":               "",
		"README.md":                 "ignored",
		"nested/requirements.txt":   "requests==2.0.0",
		"package-lock.json":         `{"lockfileVersion": 3}`,
		"package.json":              `{"name":"root"}`,
	})

	got, err := DiscoverManifests(reader)
	if err != nil {
		t.Fatalf("DiscoverManifests: %v", err)
	}
	want := []DiscoveredManifest{
		{Path: ".github/workflows/ci.yaml", Ecosystem: "github-actions", Kind: Manifest},
		{Path: ".github/workflows/ci.yml", Ecosystem: "github-actions", Kind: Manifest},
		{Path: ".gitmodules", Ecosystem: "git", Kind: Manifest},
		{Path: "package-lock.json", Ecosystem: "npm", Kind: Lockfile},
		{Path: "package.json", Ecosystem: "npm", Kind: Manifest},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("DiscoverManifests() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestDiscoverManifestsWorkspaces(t *testing.T) {
	reader := mapFSReader(map[string]string{
		"Cargo.toml": `[workspace]
members = ["crates/*", "tools/cli"]
exclude = ["crates/private"]
`,
		"crates/core/Cargo.toml":    `[package]`,
		"crates/private/Cargo.toml": `[package]`,
		"tools/cli/Cargo.toml":      `[package]`,
		"go.work": `go 1.25.0

use (
    "./services/api"
    ./libs/shared
)
`,
		"services/api/go.mod":   `module example.com/api`,
		"libs/shared/go.mod":    `module example.com/shared`,
		"package.json":          `{"workspaces":{"packages":["apps/*"],"nohoist":["**"]}}`,
		"apps/web/package.json": `{"name":"web"}`,
		"pnpm-workspace.yaml": `packages:
  - "packages/**"
  - "!packages/**/fixtures"
`,
		"packages/direct/package.json":         `{"name":"direct"}`,
		"packages/group/api/package.json":      `{"name":"api"}`,
		"packages/group/fixtures/package.json": `{"name":"fixture"}`,
	})

	got, err := DiscoverManifests(reader)
	if err != nil {
		t.Fatalf("DiscoverManifests: %v", err)
	}
	want := []DiscoveredManifest{
		{Path: "Cargo.toml", Ecosystem: "cargo", Kind: Manifest},
		{Path: "apps/web/package.json", Ecosystem: "npm", Kind: Manifest, ParentPath: "package.json"},
		{Path: "crates/core/Cargo.toml", Ecosystem: "cargo", Kind: Manifest, ParentPath: "Cargo.toml"},
		{Path: "libs/shared/go.mod", Ecosystem: "golang", Kind: Manifest, ParentPath: "go.work"},
		{Path: "package.json", Ecosystem: "npm", Kind: Manifest},
		{Path: "packages/direct/package.json", Ecosystem: "npm", Kind: Manifest, ParentPath: "pnpm-workspace.yaml"},
		{Path: "packages/group/api/package.json", Ecosystem: "npm", Kind: Manifest, ParentPath: "pnpm-workspace.yaml"},
		{Path: "services/api/go.mod", Ecosystem: "golang", Kind: Manifest, ParentPath: "go.work"},
		{Path: "tools/cli/Cargo.toml", Ecosystem: "cargo", Kind: Manifest, ParentPath: "Cargo.toml"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("DiscoverManifests() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestDiscoverManifestsNPMWorkspaceForms(t *testing.T) {
	tests := []struct {
		name       string
		workspaces string
	}{
		{name: "npm array", workspaces: `["packages/*"]`},
		{name: "yarn object", workspaces: `{"packages":["packages/*"],"nohoist":["**"]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := mapFSReader(map[string]string{
				"package.json":              `{"workspaces":` + test.workspaces + `}`,
				"packages/api/package.json": `{"name":"api"}`,
			})

			got, err := DiscoverManifests(reader)
			if err != nil {
				t.Fatalf("DiscoverManifests: %v", err)
			}
			want := []DiscoveredManifest{
				{Path: "package.json", Ecosystem: "npm", Kind: Manifest},
				{Path: "packages/api/package.json", Ecosystem: "npm", Kind: Manifest, ParentPath: "package.json"},
			}
			if !slices.Equal(got, want) {
				t.Fatalf("DiscoverManifests() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestDiscoverManifestsRejectsOutsideWorkspaceMembers(t *testing.T) {
	reader := mapFSReader(map[string]string{
		"Cargo.toml":              `[workspace]` + "\n" + `members = ["../outside", "/absolute"]`,
		"outside/Cargo.toml":      `[package]`,
		"absolute/Cargo.toml":     `[package]`,
		"nested/other/Cargo.toml": `[package]`,
	})

	got, err := DiscoverManifests(reader)
	if err != nil {
		t.Fatalf("DiscoverManifests: %v", err)
	}
	want := []DiscoveredManifest{{Path: "Cargo.toml", Ecosystem: "cargo", Kind: Manifest}}
	if !slices.Equal(got, want) {
		t.Fatalf("DiscoverManifests() = %+v, want %+v", got, want)
	}
}

func TestDiscoverManifestsReportsConfigurationErrors(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		content   string
		wantError string
	}{
		{name: "cargo", path: "Cargo.toml", content: `[workspace`, wantError: "parsing Cargo workspace configuration"},
		{name: "go", path: "go.work", content: `use (`, wantError: "parsing Go workspace configuration"},
		{name: "npm", path: "package.json", content: `{"workspaces":true}`, wantError: "parsing npm workspace configuration"},
		{name: "pnpm", path: "pnpm-workspace.yaml", content: `packages: true`, wantError: "parsing pnpm workspace configuration"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DiscoverManifests(mapFSReader(map[string]string{test.path: test.content}))
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("DiscoverManifests() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestDiscoverManifestsReaderErrors(t *testing.T) {
	wantErr := errors.New("glob failed")
	_, err := DiscoverManifests(errorRepositoryReader{err: wantErr})
	if !errors.Is(err, wantErr) {
		t.Fatalf("DiscoverManifests() error = %v, want wrapped %v", err, wantErr)
	}
}

func mapFSReader(files map[string]string) *FSReader {
	root := make(fstest.MapFS, len(files))
	for name, content := range files {
		root[name] = &fstest.MapFile{Data: []byte(content), Mode: 0o644}
	}
	return NewFSReader(root)
}

type errorRepositoryReader struct {
	err error
}

func (r errorRepositoryReader) ReadFile(string) ([]byte, error) {
	return nil, fs.ErrNotExist
}

func (r errorRepositoryReader) Glob(string) ([]string, error) {
	return nil, r.err
}
