package output

import (
	"regexp"
	"strings"
)

// GrepInfo holds the extracted grep search parameters from a command string.
type GrepInfo struct {
	Patterns        []string
	CaseInsensitive bool
}

// ExtractGrepInfo parses a shell command and returns grep search info.
// It collects patterns from ALL grep segments in a pipeline, so that
// commands like "grep 206092 | grep 正常IP" highlight both patterns.
// Returns GrepInfo with empty Patterns if no grep is found or all greps use -v.
// Supports grep, egrep, and fgrep variants.
func ExtractGrepInfo(command string) GrepInfo {
	var combined GrepInfo
	for _, part := range splitPipes(command) {
		t := strings.TrimSpace(part)
		if !isGrepCommand(t) {
			continue
		}
		info := parseGrepCommand(t)
		combined.Patterns = append(combined.Patterns, info.Patterns...)
		if info.CaseInsensitive {
			combined.CaseInsensitive = true
		}
	}
	return combined
}

// isGrepCommand reports whether s starts with grep, egrep, or fgrep
// (as a standalone command or followed by a space/tab).
func isGrepCommand(s string) bool {
	for _, cmd := range []string{"grep", "egrep", "fgrep"} {
		if s == cmd || strings.HasPrefix(s, cmd+" ") || strings.HasPrefix(s, cmd+"\t") {
			return true
		}
	}
	return false
}

// HighlightPatterns applies ANSI cyan-bold highlighting to all occurrences
// of each pattern in text. Returns text unchanged when colorEnabled is false
// or GrepInfo has no patterns.
// Existing ANSI escape codes in text are stripped first so that color codes
// emitted by remote programs (e.g. grep --color=auto over a PTY session) do
// not interfere with beelog's own highlighting.
func HighlightPatterns(text string, info GrepInfo, colorEnabled bool) string {
	if !colorEnabled || len(info.Patterns) == 0 {
		return text
	}
	result := stripANSI(text)
	for _, p := range info.Patterns {
		result = applyHighlight(result, p, info.CaseInsensitive)
	}
	return result
}

// ansiEscapeRe matches ANSI / VT100 CSI escape sequences of the form
// ESC [ <params> <final-byte>, covering SGR color codes, cursor movement,
// and erase sequences emitted by programs like grep --color.
var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// stripANSI removes ANSI escape sequences from text.
func stripANSI(text string) string {
	return ansiEscapeRe.ReplaceAllString(text, "")
}

// highlightOn / highlightOff are the ANSI escape sequences for cyan-bold text.
const (
	highlightOn  = "\033[1;36m"
	highlightOff = "\033[0m"
)

// applyHighlight highlights all occurrences of pattern in text.
// Tries to compile pattern as a regular expression; falls back to literal
// matching if the pattern is not valid regex syntax.
func applyHighlight(text, pattern string, caseInsensitive bool) string {
	if pattern == "" {
		return text
	}
	re := compilePattern(pattern, caseInsensitive)
	if re == nil {
		return text
	}
	return re.ReplaceAllStringFunc(text, func(match string) string {
		return highlightOn + match + highlightOff
	})
}

// compilePattern compiles a grep pattern for use with regexp.
// Tries the pattern as-is first; if invalid, retries with QuoteMeta (literal).
// Returns nil if both attempts fail.
func compilePattern(pattern string, caseInsensitive bool) *regexp.Regexp {
	prefix := ""
	if caseInsensitive {
		prefix = "(?i)"
	}
	if re, err := regexp.Compile(prefix + pattern); err == nil {
		return re
	}
	// Fall back to literal match
	if re, err := regexp.Compile(prefix + regexp.QuoteMeta(pattern)); err == nil {
		return re
	}
	return nil
}

// splitPipes splits a shell command by unquoted "|" characters.
// Characters inside single or double quotes are not treated as pipe separators.
func splitPipes(command string) []string {
	var parts []string
	var cur strings.Builder
	inSingle := false
	inDouble := false

	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case inSingle:
			cur.WriteByte(c)
			if c == '\'' {
				inSingle = false
			}
		case inDouble:
			cur.WriteByte(c)
			if c == '"' {
				inDouble = false
			} else if c == '\\' && i+1 < len(command) {
				i++
				cur.WriteByte(command[i])
			}
		case c == '\'':
			inSingle = true
			cur.WriteByte(c)
		case c == '"':
			inDouble = true
			cur.WriteByte(c)
		case c == '|':
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 || len(parts) > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

// parseGrepCommand parses a single grep command string (starting with grep/egrep/fgrep)
// to extract search patterns and relevant flags.
func parseGrepCommand(cmd string) GrepInfo {
	tokens := shellTokenize(cmd)
	if len(tokens) == 0 {
		return GrepInfo{}
	}
	// tokens[0] is "grep", "egrep", or "fgrep"
	fixedStrings := tokens[0] == "fgrep"

	var info GrepInfo
	invertMatch := false
	endOfFlags := false
	positionalPatternSet := false

	i := 1
	for i < len(tokens) {
		tok := tokens[i]

		if endOfFlags || !strings.HasPrefix(tok, "-") || tok == "-" {
			// Positional argument: first one is the pattern (when no -e was used)
			if !positionalPatternSet && len(info.Patterns) == 0 {
				info.Patterns = append(info.Patterns, tok)
				positionalPatternSet = true
			}
			i++
			continue
		}

		if tok == "--" {
			endOfFlags = true
			i++
			continue
		}

		// Parse flag characters (may be combined, e.g. -iE)
		flagStr := tok[1:]
		j := 0
		for j < len(flagStr) {
			switch flagStr[j] {
			case 'i':
				info.CaseInsensitive = true
			case 'v':
				invertMatch = true
			case 'F':
				fixedStrings = true
			case 'e':
				// -e: the pattern follows — either as the rest of this token or next token
				if j+1 < len(flagStr) {
					info.Patterns = append(info.Patterns, flagStr[j+1:])
					j = len(flagStr) // consume rest of flag string
					continue
				}
				i++
				if i < len(tokens) {
					info.Patterns = append(info.Patterns, tokens[i])
				}
			case 'f':
				// -f file: skip file argument
				if j+1 >= len(flagStr) {
					i++ // skip next token (the file)
				}
				j = len(flagStr)
				continue
			case 'A', 'B', 'C', 'm':
				// flags that consume the next argument (or rest of combined flag)
				if j+1 >= len(flagStr) {
					i++ // skip the numeric argument
				}
				j = len(flagStr)
				continue
			}
			j++
		}
		i++
	}

	if invertMatch {
		return GrepInfo{} // -v: no highlighting
	}
	// fgrep / grep -F: patterns are literal strings, escape regex metacharacters
	if fixedStrings {
		for k, p := range info.Patterns {
			info.Patterns[k] = regexp.QuoteMeta(p)
		}
	}
	return info
}

// shellTokenize splits a shell command into tokens, respecting single and
// double quoted strings. Backslash escapes inside double quotes are handled.
func shellTokenize(cmd string) []string {
	var tokens []string
	var cur strings.Builder
	inSingle := false
	inDouble := false

	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			} else {
				cur.WriteByte(c)
			}
		case inDouble:
			if c == '"' {
				inDouble = false
			} else if c == '\\' && i+1 < len(cmd) {
				i++
				cur.WriteByte(cmd[i])
			} else {
				cur.WriteByte(c)
			}
		case c == '\'':
			inSingle = true
		case c == '"':
			inDouble = true
		case c == ' ' || c == '\t':
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}
