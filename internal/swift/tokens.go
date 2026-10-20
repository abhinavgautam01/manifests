package swift

import (
	"strconv"
	"strings"
)

type token struct {
	text    string
	literal bool
}

func (t token) is(text string) bool {
	return !t.literal && t.text == text
}

// Only literal arguments are interpreted; manifest expressions are not evaluated.
func tokenize(text string) []token {
	var tokens []token
	for i := 0; i < len(text); {
		switch {
		case strings.ContainsRune(" \t\r\n", rune(text[i])):
			i++
		case strings.HasPrefix(text[i:], "//"):
			end := strings.IndexByte(text[i:], '\n')
			if end < 0 {
				return tokens
			}
			i += end
		case strings.HasPrefix(text[i:], "/*"):
			i = skipComment(text, i)
		case text[i] == '"' || text[i] == '#':
			value, end := stringToken(text, i)
			tokens = append(tokens, token{text: value, literal: true})
			i = end
		default:
			end := i + 1
			if wordByte(text[i]) {
				for end < len(text) && wordByte(text[end]) {
					end++
				}
			}
			tokens = append(tokens, token{text: text[i:end]})
			i = end
		}
	}
	return tokens
}

func wordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}

func skipComment(text string, start int) int {
	depth := 1
	for i := start + len("/*"); i < len(text); {
		switch {
		case strings.HasPrefix(text[i:], "/*"):
			depth++
			i += 2
		case strings.HasPrefix(text[i:], "*/"):
			depth--
			i += 2
			if depth == 0 {
				return i
			}
		default:
			i++
		}
	}
	return len(text)
}

func stringToken(text string, start int) (string, int) {
	i := start
	for i < len(text) && text[i] == '#' {
		i++
	}
	hashes := text[start:i]
	if i == len(text) || text[i] != '"' {
		return "", i
	}
	quote := `"`
	if strings.HasPrefix(text[i:], `"""`) {
		quote = `"""`
	}
	begin := i
	i += len(quote)
	contentStart := i
	closing := quote + hashes
	for i < len(text) {
		if strings.HasPrefix(text[i:], closing) {
			end := i + len(closing)
			if len(quote) != 1 {
				return "", end
			}
			value := text[contentStart:i]
			if hashes != "" {
				if strings.Contains(value, `\`+hashes) {
					return "", end
				}
				return value, end
			}
			decoded, err := strconv.Unquote(text[begin : i+1])
			if err != nil {
				return "", end
			}
			return decoded, end
		}
		if strings.HasPrefix(text[i:], `\`+hashes) {
			i += len(hashes) + 1
		}
		i++
	}
	return "", len(text)
}
