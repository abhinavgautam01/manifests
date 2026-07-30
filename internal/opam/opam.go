package opam

import (
	"regexp"
	"strings"

	"github.com/git-pkgs/manifests/internal/core"
)

func init() {
	core.Register("opam", core.Manifest, &parser{},
		core.AnyMatch(core.ExactMatch("opam"), core.SuffixMatch(".opam")))
}

// parser parses OPAM package definition files.
type parser struct{}

func (p *parser) Parse(_ string, content []byte) (*core.Result, error) {
	text := string(content)
	var deps []core.Dependency
	for _, name := range opamTopLevelStrings(opamField(text, "depends")) {
		deps = append(deps, core.Dependency{
			Name:   name,
			Scope:  core.Runtime,
			Direct: true,
		})
	}

	return &core.Result{
		Name:         opamScalar(opamField(text, "name")),
		Version:      opamScalar(opamField(text, "version")),
		Licenses:     opamTopLevelStrings(opamField(text, "license")),
		Dependencies: deps,
	}, nil
}

func opamField(text, field string) string {
	pattern := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(field) + `[ \t]*:[ \t]*`)
	location := pattern.FindStringIndex(text)
	if location == nil {
		return ""
	}
	start := location[1]
	for start < len(text) && (text[start] == ' ' || text[start] == '\t') {
		start++
	}
	if start >= len(text) {
		return ""
	}
	if text[start] != '[' {
		if end := strings.IndexByte(text[start:], '\n'); end >= 0 {
			return strings.TrimSpace(text[start : start+end])
		}
		return strings.TrimSpace(text[start:])
	}

	depth := 0
	inString := false
	escaped := false
	inComment := false
	for i := start; i < len(text); i++ {
		switch {
		case inComment:
			if text[i] == '\n' {
				inComment = false
			}
		case inString:
			if escaped {
				escaped = false
			} else if text[i] == '\\' {
				escaped = true
			} else if text[i] == '"' {
				inString = false
			}
		default:
			switch text[i] {
			case '#':
				inComment = true
			case '"':
				inString = true
			case '[':
				depth++
			case ']':
				depth--
				if depth == 0 {
					return text[start : i+1]
				}
			}
		}
	}
	return strings.TrimSpace(text[start:])
}

func opamScalar(value string) string {
	values := opamTopLevelStrings(value)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func opamTopLevelStrings(value string) []string {
	var values []string
	braceDepth := 0
	inComment := false
	for i := 0; i < len(value); {
		switch value[i] {
		case '#':
			inComment = true
			i++
		case '\n':
			inComment = false
			i++
		case '{':
			if !inComment {
				braceDepth++
			}
			i++
		case '}':
			if !inComment && braceDepth > 0 {
				braceDepth--
			}
			i++
		case '"':
			if inComment {
				i++
				continue
			}
			start := i + 1
			i++
			escaped := false
			var valueBuilder strings.Builder
			for i < len(value) {
				if escaped {
					valueBuilder.WriteByte(value[i])
					escaped = false
				} else if value[i] == '\\' {
					escaped = true
				} else if value[i] == '"' {
					break
				} else {
					valueBuilder.WriteByte(value[i])
				}
				i++
			}
			if braceDepth == 0 && i <= len(value) {
				if valueBuilder.Len() == 0 {
					values = append(values, value[start:i])
				} else {
					values = append(values, valueBuilder.String())
				}
			}
			if i < len(value) {
				i++
			}
		default:
			i++
		}
	}
	return values
}
