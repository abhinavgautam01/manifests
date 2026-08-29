// Package chef parses Chef cookbook manifests without evaluating Ruby.
package chef

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"github.com/git-pkgs/manifests/internal/core"
)

func init() {
	core.Register("chef", core.Manifest, &metadataRubyParser{}, core.ExactMatch("metadata.rb"))
	core.Register("chef", core.Manifest, &metadataJSONParser{}, core.ExactMatch("metadata.json"))
	core.Register("chef", core.Manifest, &berksfileParser{}, core.ExactMatch("Berksfile"))
}

type metadataRubyParser struct{}

func (p *metadataRubyParser) Parse(_ string, content []byte) (*core.Result, error) {
	result := &core.Result{}
	locations := make(map[string]int)
	for _, statement := range rubyStatements(content) {
		call, ok := parseRubyCall(statement)
		if !ok {
			continue
		}
		switch call.name {
		case "name":
			if call.hasExactPositionalCount(1) && call.positional[0] != "" {
				result.Name = call.positional[0]
			}
		case "version":
			if call.hasExactPositionalCount(1) && call.positional[0] != "" {
				result.Version = call.positional[0]
			}
		case "license":
			if call.hasExactPositionalCount(1) && call.positional[0] != "" {
				result.Licenses = []string{call.positional[0]}
			}
		case "depends":
			if len(call.positional) == 0 || call.positional[0] == "" || len(call.keywords) != 0 {
				continue
			}
			appendChefDependency(result, locations, "depends", call.positional, core.Source{})
		}
	}
	return result, nil
}

type metadataJSONParser struct{}

type metadataJSONDocument struct {
	Name         json.RawMessage            `json:"name"`
	Version      json.RawMessage            `json:"version"`
	License      json.RawMessage            `json:"license"`
	Dependencies map[string]json.RawMessage `json:"dependencies"`
}

func (p *metadataJSONParser) Parse(filename string, content []byte) (*core.Result, error) {
	var document metadataJSONDocument
	if err := json.Unmarshal(content, &document); err != nil {
		return nil, &core.ParseError{Filename: filename, Err: err}
	}

	result := &core.Result{}
	result.Name, _ = decodeJSONString(document.Name)
	result.Version, _ = decodeJSONString(document.Version)
	if license, ok := decodeJSONString(document.License); ok && license != "" {
		result.Licenses = []string{license}
	}

	names := make([]string, 0, len(document.Dependencies))
	for name := range document.Dependencies {
		names = append(names, name)
	}
	sort.Strings(names)
	locations := make(map[string]int)
	for _, name := range names {
		if name == "" {
			continue
		}
		constraints, ok := decodeJSONConstraints(document.Dependencies[name])
		if !ok {
			continue
		}
		appendChefDependency(result, locations, "dependencies", append([]string{name}, constraints...), core.Source{})
	}
	return result, nil
}

func decodeJSONString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func decodeJSONConstraints(raw json.RawMessage) ([]string, bool) {
	if value, ok := decodeJSONString(raw); ok {
		return []string{value}, true
	}
	var values []string
	if len(raw) == 0 || json.Unmarshal(raw, &values) != nil {
		return nil, false
	}
	return values, true
}

type berksfileParser struct{}

func (p *berksfileParser) Parse(_ string, content []byte) (*core.Result, error) {
	result := &core.Result{}
	locations := make(map[string]int)
	for _, statement := range rubyStatements(content) {
		call, ok := parseRubyCall(statement)
		if !ok {
			continue
		}
		switch call.name {
		case "source":
			if len(call.positional) != 1 || call.positional[0] == "" {
				continue
			}
			result.Sources = append(result.Sources, core.Source{
				Kind:  core.SourceRegistry,
				Value: call.positional[0],
			})
		case "cookbook":
			if len(call.positional) == 0 || len(call.positional) > 2 || call.positional[0] == "" {
				continue
			}
			source, ok := cookbookSource(call.keywords)
			if !ok {
				continue
			}
			appendChefDependency(result, locations, "cookbooks", call.positional, source)
		case "metadata":
			// Deliberately ignored. Evaluating this directive would read an
			// adjacent cookbook and make Parse impure.
		}
	}
	return result, nil
}

func cookbookSource(keywords map[string]string) (core.Source, bool) {
	type sourceKey struct {
		name string
		kind core.SourceKind
	}
	keys := []sourceKey{
		{name: "git", kind: core.SourceGit},
		{name: "path", kind: core.SourcePath},
		{name: "github", kind: core.SourceGitHub},
	}
	var source core.Source
	selected := ""
	for _, key := range keys {
		value, ok := keywords[key.name]
		if !ok {
			continue
		}
		if selected != "" || value == "" {
			return core.Source{}, false
		}
		selected = key.name
		source.Kind = key.kind
		source.Value = value
	}
	// Other literal options such as branch, tag, ref, and rel are accepted so
	// the source location is retained from common multiline Berksfile
	// declarations. They do not alter the raw coordinate represented by Source.
	return source, true
}

func appendChefDependency(
	result *core.Result,
	locations map[string]int,
	prefix string,
	values []string,
	source core.Source,
) {
	name := values[0]
	version := strings.Join(values[1:], ", ")
	result.Dependencies = append(result.Dependencies, core.Dependency{
		Name:    name,
		Version: version,
		Scope:   core.Runtime,
		Direct:  true,
		Source:  source,
	})
	location := core.NextLocation(locations, prefix+"/"+url.PathEscape(name))
	result.Declarations = append(result.Declarations, core.Declaration{
		Name:     name,
		Version:  version,
		Scope:    core.Runtime,
		Direct:   true,
		Location: location,
		Source:   source,
	})
}

type rubyCall struct {
	name       string
	positional []string
	keywords   map[string]string
}

func (call rubyCall) hasExactPositionalCount(count int) bool {
	return len(call.positional) == count && len(call.keywords) == 0
}

// rubyStatements splits the supported single-line Ruby DSL and comma- or
// parenthesis-continued calls. Comments are removed only outside strings.
// Unterminated strings are abandoned at the physical newline so one malformed
// dynamic declaration cannot hide later valid declarations.
func rubyStatements(content []byte) []string {
	statements := make([]string, 0, strings.Count(string(content), "\n")+1)
	var statement strings.Builder
	var quote byte
	escaped := false
	comment := false
	parentheses := 0

	flush := func() {
		trimmed := strings.TrimSpace(statement.String())
		if trimmed != "" {
			statements = append(statements, trimmed)
		}
		statement.Reset()
		quote = 0
		escaped = false
		comment = false
		parentheses = 0
	}

	for _, character := range content {
		if character == '\n' {
			if quote != 0 {
				flush()
				continue
			}
			comment = false
			if parentheses > 0 || lastNonSpaceByte(statement.String()) == ',' {
				statement.WriteByte(' ')
			} else {
				flush()
			}
			continue
		}
		if comment {
			continue
		}
		if quote != 0 {
			statement.WriteByte(character)
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if character == quote {
				quote = 0
			}
			continue
		}
		switch character {
		case '\'', '"':
			quote = character
			statement.WriteByte(character)
		case '#':
			comment = true
		case '(':
			parentheses++
			statement.WriteByte(character)
		case ')':
			parentheses--
			statement.WriteByte(character)
		default:
			statement.WriteByte(character)
		}
	}
	flush()
	return statements
}

func lastNonSpaceByte(value string) byte {
	for index := len(value) - 1; index >= 0; index-- {
		if !isRubySpace(value[index]) {
			return value[index]
		}
	}
	return 0
}

// parseRubyCall accepts only a method name followed by literal string
// positional arguments and literal string keyword arguments. Any remaining
// Ruby expression makes the whole declaration ineligible.
func parseRubyCall(statement string) (rubyCall, bool) {
	position := 0
	skipRubySpace(statement, &position)
	start := position
	for position < len(statement) && isRubyIdentifierByte(statement[position]) {
		position++
	}
	if position == start {
		return rubyCall{}, false
	}
	call := rubyCall{name: statement[start:position]}
	if position < len(statement) && !isRubySpace(statement[position]) && statement[position] != '(' {
		return rubyCall{}, false
	}
	skipRubySpace(statement, &position)
	parenthesized := position < len(statement) && statement[position] == '('
	if parenthesized {
		position++
	}

	for {
		skipRubySpace(statement, &position)
		if parenthesized && position < len(statement) && statement[position] == ')' {
			position++
			skipRubySpace(statement, &position)
			return call, position == len(statement)
		}
		if position == len(statement) {
			return call, !parenthesized
		}

		if statement[position] == '\'' || statement[position] == '"' {
			value, ok := parseRubyString(statement, &position)
			if !ok {
				return rubyCall{}, false
			}
			call.positional = append(call.positional, value)
		} else {
			key, value, ok := parseRubyKeyword(statement, &position)
			if !ok {
				return rubyCall{}, false
			}
			if call.keywords == nil {
				call.keywords = make(map[string]string)
			}
			if _, duplicate := call.keywords[key]; duplicate {
				return rubyCall{}, false
			}
			call.keywords[key] = value
		}

		skipRubySpace(statement, &position)
		if position == len(statement) {
			return call, !parenthesized
		}
		if parenthesized && statement[position] == ')' {
			continue
		}
		if statement[position] != ',' {
			return rubyCall{}, false
		}
		position++
		skipRubySpace(statement, &position)
		if position == len(statement) {
			return call, !parenthesized
		}
	}
}

func parseRubyKeyword(statement string, position *int) (string, string, bool) {
	start := *position
	var key string
	if statement[*position] == ':' {
		*position++
		identifierStart := *position
		for *position < len(statement) && isRubyIdentifierByte(statement[*position]) {
			*position++
		}
		if *position == identifierStart {
			return "", "", false
		}
		key = statement[identifierStart:*position]
		skipRubySpace(statement, position)
		if !strings.HasPrefix(statement[*position:], "=>") {
			*position = start
			return "", "", false
		}
		*position += 2
	} else {
		for *position < len(statement) && isRubyIdentifierByte(statement[*position]) {
			*position++
		}
		if *position == start {
			return "", "", false
		}
		key = statement[start:*position]
		skipRubySpace(statement, position)
		if *position >= len(statement) || statement[*position] != ':' {
			*position = start
			return "", "", false
		}
		*position++
	}
	skipRubySpace(statement, position)
	if *position >= len(statement) || (statement[*position] != '\'' && statement[*position] != '"') {
		*position = start
		return "", "", false
	}
	value, ok := parseRubyString(statement, position)
	if !ok {
		*position = start
		return "", "", false
	}
	return key, value, true
}

func parseRubyString(statement string, position *int) (string, bool) {
	quote := statement[*position]
	*position++
	var value strings.Builder
	for *position < len(statement) {
		character := statement[*position]
		*position++
		if character == quote {
			return value.String(), true
		}
		if character == '\\' {
			if *position >= len(statement) {
				return "", false
			}
			next := statement[*position]
			*position++
			if quote == '\'' && next != '\'' && next != '\\' {
				value.WriteByte('\\')
				value.WriteByte(next)
				continue
			}
			switch next {
			case 'n':
				value.WriteByte('\n')
			case 'r':
				value.WriteByte('\r')
			case 't':
				value.WriteByte('\t')
			default:
				value.WriteByte(next)
			}
			continue
		}
		if quote == '"' && character == '#' && *position < len(statement) {
			switch statement[*position] {
			case '{', '$', '@':
				return "", false
			}
		}
		value.WriteByte(character)
	}
	return "", false
}

func skipRubySpace(value string, position *int) {
	for *position < len(value) && isRubySpace(value[*position]) {
		*position++
	}
}

func isRubySpace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n'
}

func isRubyIdentifierByte(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' || character == '_'
}
