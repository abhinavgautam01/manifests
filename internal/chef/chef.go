// Package chef parses Chef cookbook manifests without evaluating Ruby.
package chef

import (
	"bytes"
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
	if !supportedCookbookSourceKeywords(keywords) {
		return core.Source{}, false
	}
	source.Branch = keywords["branch"]
	source.Tag = keywords["tag"]
	source.Ref = keywords["ref"]
	source.Rel = keywords["rel"]
	if selected == "" && (source.Branch != "" || source.Tag != "" || source.Ref != "" || source.Rel != "") {
		return core.Source{}, false
	}
	return source, true
}

func supportedCookbookSourceKeywords(keywords map[string]string) bool {
	for key := range keywords {
		switch key {
		case "git", "path", "github", "branch", "tag", "ref", "rel":
		default:
			return false
		}
	}
	return true
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
	scanner := rubyStatementScanner{
		statements: make([]string, 0, core.EstimateDeps(len(content))),
	}
	for position, character := range content {
		if character == '\n' {
			scanner.finishLine(content[position+1:])
			continue
		}
		scanner.consume(character)
	}
	scanner.flush()
	return scanner.statements
}

type rubyStatementScanner struct {
	statements  []string
	statement   strings.Builder
	quote       byte
	escaped     bool
	comment     bool
	parentheses int
	blockDepth  int
}

func (scanner *rubyStatementScanner) consume(character byte) {
	if scanner.comment {
		return
	}
	if scanner.quote != 0 {
		scanner.consumeQuoted(character)
		return
	}
	scanner.consumeUnquoted(character)
}

func (scanner *rubyStatementScanner) consumeQuoted(character byte) {
	scanner.statement.WriteByte(character)
	switch {
	case scanner.escaped:
		scanner.escaped = false
	case character == '\\':
		scanner.escaped = true
	case character == scanner.quote:
		scanner.quote = 0
	}
}

func (scanner *rubyStatementScanner) consumeUnquoted(character byte) {
	switch character {
	case '\'', '"':
		scanner.quote = character
		scanner.statement.WriteByte(character)
	case '#':
		scanner.comment = true
	case '(':
		scanner.parentheses++
		scanner.statement.WriteByte(character)
	case ')':
		scanner.parentheses--
		scanner.statement.WriteByte(character)
	default:
		scanner.statement.WriteByte(character)
	}
}

func (scanner *rubyStatementScanner) finishLine(remaining []byte) {
	if scanner.quote != 0 {
		scanner.resetStatement()
		return
	}
	scanner.comment = false
	if scanner.shouldContinue(remaining) {
		scanner.statement.WriteByte(' ')
		return
	}
	scanner.flush()
}

func (scanner *rubyStatementScanner) shouldContinue(remaining []byte) bool {
	last := lastNonSpaceByte(scanner.statement.String())
	if scanner.parentheses <= 0 {
		return last == ','
	}
	next := nextRubyCode(remaining)
	if startsRubyStatementBoundary(next) && !rubyParenthesesClose(remaining, scanner.parentheses) {
		return false
	}
	return last == ',' || last == '(' || len(next) > 0 && next[0] == ')'
}

func rubyParenthesesClose(content []byte, depth int) bool {
	var quote byte
	escaped := false
	comment := false
	for _, character := range content {
		if character == '\n' {
			quote = 0
			escaped = false
			comment = false
			continue
		}
		if comment {
			continue
		}
		if quote != 0 {
			switch {
			case escaped:
				escaped = false
			case character == '\\':
				escaped = true
			case character == quote:
				quote = 0
			}
			continue
		}
		switch character {
		case '\'', '"':
			quote = character
		case '#':
			comment = true
		case '(':
			depth++
		case ')':
			depth--
			if depth <= 0 {
				return true
			}
		}
	}
	return false
}

func (scanner *rubyStatementScanner) flush() {
	statement := strings.TrimSpace(scanner.statement.String())
	scanner.resetStatement()
	if statement == "" {
		return
	}
	initialDepth := scanner.blockDepth
	newDepth, boundary := applyRubyScopeTransitions(statement, scanner.blockDepth)
	scanner.blockDepth = newDepth
	if initialDepth == 0 && !boundary {
		scanner.statements = append(scanner.statements, statement)
	}
}

func (scanner *rubyStatementScanner) resetStatement() {
	scanner.statement.Reset()
	scanner.quote = 0
	scanner.escaped = false
	scanner.comment = false
	scanner.parentheses = 0
}

func nextRubyCode(content []byte) []byte {
	for len(content) > 0 {
		for len(content) > 0 && isRubySpace(content[0]) {
			content = content[1:]
		}
		if len(content) == 0 || content[0] != '#' {
			return content
		}
		newline := bytes.IndexByte(content, '\n')
		if newline < 0 {
			return nil
		}
		content = content[newline+1:]
	}
	return nil
}

func startsRubyStatementBoundary(content []byte) bool {
	position := 0
	for position < len(content) && isRubyIdentifierByte(content[position]) {
		position++
	}
	if position == 0 {
		return len(content) > 0 && content[0] == '}'
	}
	switch string(content[:position]) {
	case "name", "version", "license", "depends", "source", "cookbook", "metadata",
		"if", "unless", "case", "while", "until", "for", "begin", "class", "module", "def", "end":
		return position == len(content) || isRubySpace(content[position]) || content[position] == '('
	default:
		return false
	}
}

func applyRubyScopeTransitions(statement string, depth int) (int, bool) {
	position := 0
	segmentStart := true
	afterAssignment := false
	boundary := false
	for position < len(statement) {
		character := statement[position]
		if isRubySpace(character) {
			position++
			continue
		}
		if character == '\'' || character == '"' {
			skipRubyQuotedText(statement, &position)
			segmentStart = false
			afterAssignment = false
			continue
		}
		if isRubyIdentifierByte(character) {
			start := position
			for position < len(statement) && isRubyIdentifierByte(statement[position]) {
				position++
			}
			if !rubyKeywordLabel(statement, position) {
				depth, boundary = applyRubyKeywordScope(
					statement[start:position], depth, segmentStart, afterAssignment, boundary,
				)
			}
			segmentStart = false
			afterAssignment = false
			continue
		}
		switch character {
		case '{':
			depth++
			boundary = true
		case '}':
			depth = decrementRubyScope(depth)
			boundary = true
		case ';':
			segmentStart = true
			afterAssignment = false
		case '=':
			afterAssignment = rubyAssignmentOperator(statement, position)
		}
		position++
	}
	return depth, boundary
}

func applyRubyKeywordScope(
	keyword string,
	depth int,
	segmentStart bool,
	afterAssignment bool,
	boundary bool,
) (int, bool) {
	switch keyword {
	case "end":
		return decrementRubyScope(depth), true
	case "do":
		return depth + 1, true
	case "if", "unless", "case", "while", "until", "for", "begin", "class", "module", "def":
		if segmentStart || afterAssignment {
			return depth + 1, true
		}
	case "else", "elsif", "when", "rescue", "ensure":
		return depth, true
	}
	return depth, boundary
}

func decrementRubyScope(depth int) int {
	if depth > 0 {
		return depth - 1
	}
	return 0
}

func rubyKeywordLabel(statement string, position int) bool {
	skipRubySpace(statement, &position)
	return position < len(statement) && statement[position] == ':'
}

func rubyAssignmentOperator(statement string, position int) bool {
	if position+1 < len(statement) && (statement[position+1] == '=' || statement[position+1] == '>') {
		return false
	}
	for position--; position >= 0; position-- {
		if isRubySpace(statement[position]) {
			continue
		}
		return !strings.ContainsRune("=!<>", rune(statement[position]))
	}
	return true
}

func skipRubyQuotedText(statement string, position *int) bool {
	quote := statement[*position]
	*position++
	for *position < len(statement) {
		character := statement[*position]
		*position++
		if character == quote {
			return true
		}
		if character == '\\' && *position < len(statement) {
			*position++
		}
	}
	return false
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
	call, position, parenthesized, ok := parseRubyCallHeader(statement)
	if !ok {
		return rubyCall{}, false
	}
	return parseRubyCallArguments(statement, position, parenthesized, call)
}

func parseRubyCallHeader(statement string) (rubyCall, int, bool, bool) {
	position := 0
	skipRubySpace(statement, &position)
	start := position
	for position < len(statement) && isRubyIdentifierByte(statement[position]) {
		position++
	}
	if position == start {
		return rubyCall{}, 0, false, false
	}
	call := rubyCall{name: statement[start:position]}
	if position < len(statement) && !isRubySpace(statement[position]) && statement[position] != '(' {
		return rubyCall{}, 0, false, false
	}
	skipRubySpace(statement, &position)
	parenthesized := position < len(statement) && statement[position] == '('
	if parenthesized {
		position++
	}
	return call, position, parenthesized, true
}

func parseRubyCallArguments(
	statement string,
	position int,
	parenthesized bool,
	call rubyCall,
) (rubyCall, bool) {
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
		if !appendRubyCallArgument(statement, &position, &call) {
			return rubyCall{}, false
		}
		done, valid := consumeRubyArgumentEnd(statement, &position, parenthesized)
		if !valid {
			return rubyCall{}, false
		}
		if done {
			return call, true
		}
	}
}

func appendRubyCallArgument(statement string, position *int, call *rubyCall) bool {
	if statement[*position] == '\'' || statement[*position] == '"' {
		value, ok := parseRubyString(statement, position)
		if ok {
			call.positional = append(call.positional, value)
		}
		return ok
	}
	key, value, ok := parseRubyKeyword(statement, position)
	if !ok {
		return false
	}
	if call.keywords == nil {
		call.keywords = make(map[string]string)
	}
	if _, duplicate := call.keywords[key]; duplicate {
		return false
	}
	call.keywords[key] = value
	return true
}

func consumeRubyArgumentEnd(statement string, position *int, parenthesized bool) (bool, bool) {
	skipRubySpace(statement, position)
	if *position == len(statement) {
		return true, !parenthesized
	}
	if parenthesized && statement[*position] == ')' {
		*position++
		skipRubySpace(statement, position)
		return true, *position == len(statement)
	}
	if statement[*position] != ',' {
		return false, false
	}
	*position++
	skipRubySpace(statement, position)
	if *position == len(statement) {
		return true, !parenthesized
	}
	return false, true
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
