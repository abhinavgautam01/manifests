package manifests

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// insertAfterLine inserts extra on a new line after the first line equal to header.
func insertAfterLine(t *testing.T, content, header, extra string) string {
	t.Helper()
	marker := "\n" + header + "\n"
	idx := strings.Index(content, marker)
	if idx < 0 {
		t.Fatalf("header %q not found", header)
	}
	idx += len(marker)
	return content[:idx] + extra + "\n" + content[idx:]
}

// doubleIndent doubles the leading spaces of every line.
func doubleIndent(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		lines[i] = strings.Repeat(" ", 2*(len(line)-len(trimmed))) + trimmed
	}
	return strings.Join(lines, "\n")
}

func TestLockfileIndentIgnoresComments(t *testing.T) {
	// https://github.com/git-pkgs/manifests/issues/108
	fixtures := []struct {
		path     string
		filename string
		header   string
		count    int
	}{
		{"testdata/npm/pnpm-lock.yaml", "pnpm-lock.yaml", "packages:", 9},
		{"testdata/crystal/shard.lock", "shard.lock", "shards:", 7},
	}

	for _, f := range fixtures {
		raw, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatalf("failed to read fixture: %v", err)
		}
		original := string(raw)

		want, err := Parse(f.filename, raw)
		if err != nil {
			t.Fatalf("%s: Parse failed: %v", f.filename, err)
		}
		if len(want.Dependencies) != f.count {
			t.Fatalf("%s: expected %d dependencies, got %d", f.filename, f.count, len(want.Dependencies))
		}

		cases := []struct {
			name    string
			content string
		}{
			{"deeper comment before first entry", insertAfterLine(t, original, f.header, "    # Dependencies")},
			{"comment at entry indent ending in colon", insertAfterLine(t, original, f.header, "  # pinned:")},
			{"top-level comment inside section", insertAfterLine(t, original, f.header, "# Dependencies")},
			{"shallower comment in doubled indent", insertAfterLine(t, doubleIndent(original), f.header, "  # Dependencies")},
		}

		for _, tc := range cases {
			t.Run(f.filename+"/"+tc.name, func(t *testing.T) {
				got, err := Parse(f.filename, []byte(tc.content))
				if err != nil {
					t.Fatalf("Parse failed: %v", err)
				}
				if !reflect.DeepEqual(got.Dependencies, want.Dependencies) {
					t.Errorf("got %d dependencies, want %d:\ngot  %+v\nwant %+v",
						len(got.Dependencies), len(want.Dependencies), got.Dependencies, want.Dependencies)
				}
			})
		}
	}
}
