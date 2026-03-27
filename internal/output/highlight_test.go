package output

import (
	"strings"
	"testing"
)

// ── ExtractGrepInfo ──────────────────────────────────────────────────────────

func TestExtractGrepInfo_NoGrep(t *testing.T) {
	info := ExtractGrepInfo("ls -la /var/log")
	if len(info.Patterns) != 0 {
		t.Errorf("expected no patterns, got %v", info.Patterns)
	}
}

func TestExtractGrepInfo_EmptyCommand(t *testing.T) {
	info := ExtractGrepInfo("")
	if len(info.Patterns) != 0 {
		t.Errorf("expected no patterns, got %v", info.Patterns)
	}
}

func TestExtractGrepInfo_SimpleGrep(t *testing.T) {
	info := ExtractGrepInfo(`grep "ERROR" /var/log/app.log`)
	if len(info.Patterns) != 1 || info.Patterns[0] != "ERROR" {
		t.Errorf("expected [ERROR], got %v", info.Patterns)
	}
	if info.CaseInsensitive {
		t.Error("expected case-sensitive")
	}
}

func TestExtractGrepInfo_SimplePipedGrep(t *testing.T) {
	info := ExtractGrepInfo(`tail -f /var/log/app.log | grep "ERROR"`)
	if len(info.Patterns) != 1 || info.Patterns[0] != "ERROR" {
		t.Errorf("expected [ERROR], got %v", info.Patterns)
	}
}

func TestExtractGrepInfo_MultiPipeGrep(t *testing.T) {
	info := ExtractGrepInfo(`cat file.log | grep "ERROR" | head -20`)
	// head is not grep, so only the one grep segment contributes
	if len(info.Patterns) != 1 || info.Patterns[0] != "ERROR" {
		t.Errorf("expected [ERROR], got %v", info.Patterns)
	}
}

// TestExtractGrepInfo_TwoGrepPipe verifies that patterns from multiple grep
// segments in a pipeline are all collected for highlighting.
// e.g. "grep 206092 | grep 正常IP" should highlight both.
func TestExtractGrepInfo_TwoGrepPipe(t *testing.T) {
	info := ExtractGrepInfo(`zcat app.log | grep 206092 | grep 正常IP`)
	if len(info.Patterns) != 2 {
		t.Fatalf("expected 2 patterns, got %v", info.Patterns)
	}
	if info.Patterns[0] != "206092" || info.Patterns[1] != "正常IP" {
		t.Errorf("expected [206092 正常IP], got %v", info.Patterns)
	}
}

// TestExtractGrepInfo_TwoGrepPipe_InvertSkipped verifies that a -v grep
// contributes no patterns, while the non-inverted grep still does.
func TestExtractGrepInfo_TwoGrepPipe_InvertSkipped(t *testing.T) {
	info := ExtractGrepInfo(`grep 206092 | grep -v DEBUG`)
	if len(info.Patterns) != 1 || info.Patterns[0] != "206092" {
		t.Errorf("expected [206092], got %v", info.Patterns)
	}
}

func TestExtractGrepInfo_CaseInsensitive(t *testing.T) {
	info := ExtractGrepInfo(`grep -i "warn" /var/log/app.log`)
	if len(info.Patterns) != 1 || info.Patterns[0] != "warn" {
		t.Errorf("expected [warn], got %v", info.Patterns)
	}
	if !info.CaseInsensitive {
		t.Error("expected case-insensitive")
	}
}

func TestExtractGrepInfo_CombinedFlags(t *testing.T) {
	info := ExtractGrepInfo(`grep -iE "error|warn" app.log`)
	if len(info.Patterns) != 1 || info.Patterns[0] != "error|warn" {
		t.Errorf("expected [error|warn], got %v", info.Patterns)
	}
	if !info.CaseInsensitive {
		t.Error("expected case-insensitive")
	}
}

func TestExtractGrepInfo_MultipleEFlags(t *testing.T) {
	info := ExtractGrepInfo(`grep -e "ERROR" -e "WARN" app.log`)
	if len(info.Patterns) != 2 {
		t.Fatalf("expected 2 patterns, got %v", info.Patterns)
	}
	if info.Patterns[0] != "ERROR" || info.Patterns[1] != "WARN" {
		t.Errorf("expected [ERROR WARN], got %v", info.Patterns)
	}
}

func TestExtractGrepInfo_InvertMatch(t *testing.T) {
	info := ExtractGrepInfo(`grep -v "DEBUG" app.log`)
	if len(info.Patterns) != 0 {
		t.Errorf("expected no patterns for -v, got %v", info.Patterns)
	}
}

func TestExtractGrepInfo_InvertWithCombinedFlags(t *testing.T) {
	info := ExtractGrepInfo(`grep -iv "debug" app.log`)
	if len(info.Patterns) != 0 {
		t.Errorf("expected no patterns for -iv, got %v", info.Patterns)
	}
}

func TestExtractGrepInfo_UnquotedPattern(t *testing.T) {
	info := ExtractGrepInfo(`grep ERROR app.log`)
	if len(info.Patterns) != 1 || info.Patterns[0] != "ERROR" {
		t.Errorf("expected [ERROR], got %v", info.Patterns)
	}
}

func TestExtractGrepInfo_SingleQuotedPattern(t *testing.T) {
	info := ExtractGrepInfo(`grep 'ERROR' app.log`)
	if len(info.Patterns) != 1 || info.Patterns[0] != "ERROR" {
		t.Errorf("expected [ERROR], got %v", info.Patterns)
	}
}

// ── HighlightPatterns ────────────────────────────────────────────────────────

func TestHighlightPatterns_ColorDisabled(t *testing.T) {
	info := GrepInfo{Patterns: []string{"ERROR"}}
	text := "2024-01-01 ERROR something failed"
	result := HighlightPatterns(text, info, false)
	if result != text {
		t.Errorf("expected unchanged text when color disabled, got %q", result)
	}
}

func TestHighlightPatterns_NoPatterns(t *testing.T) {
	info := GrepInfo{}
	text := "some log line"
	result := HighlightPatterns(text, info, true)
	if result != text {
		t.Error("expected unchanged text when no patterns")
	}
}

func TestHighlightPatterns_BasicHighlight(t *testing.T) {
	info := GrepInfo{Patterns: []string{"ERROR"}}
	text := "2024-01-01 ERROR something failed"
	result := HighlightPatterns(text, info, true)
	if !strings.Contains(result, highlightOn+"ERROR"+highlightOff) {
		t.Errorf("expected highlighted ERROR in %q", result)
	}
}

func TestHighlightPatterns_CaseInsensitive(t *testing.T) {
	info := GrepInfo{Patterns: []string{"error"}, CaseInsensitive: true}
	text := "2024-01-01 ERROR something failed"
	result := HighlightPatterns(text, info, true)
	if !strings.Contains(result, highlightOn+"ERROR"+highlightOff) {
		t.Errorf("expected case-insensitive highlight of ERROR in %q", result)
	}
}

func TestHighlightPatterns_MultiplePatterns(t *testing.T) {
	info := GrepInfo{Patterns: []string{"ERROR", "WARN"}}
	text := "ERROR and WARN occurred"
	result := HighlightPatterns(text, info, true)
	if !strings.Contains(result, highlightOn+"ERROR"+highlightOff) {
		t.Errorf("expected ERROR highlighted in %q", result)
	}
	if !strings.Contains(result, highlightOn+"WARN"+highlightOff) {
		t.Errorf("expected WARN highlighted in %q", result)
	}
}

func TestHighlightPatterns_MultipleOccurrences(t *testing.T) {
	info := GrepInfo{Patterns: []string{"ERROR"}}
	text := "ERROR first ERROR second"
	result := HighlightPatterns(text, info, true)
	count := strings.Count(result, highlightOn+"ERROR"+highlightOff)
	if count != 2 {
		t.Errorf("expected 2 highlighted occurrences, got %d in %q", count, result)
	}
}

func TestHighlightPatterns_InvalidRegexFallback(t *testing.T) {
	// Pattern with invalid regex (unclosed bracket), should fall back to literal match
	info := GrepInfo{Patterns: []string{"[invalid"}}
	text := "contains [invalid pattern"
	result := HighlightPatterns(text, info, true)
	if !strings.Contains(result, highlightOn+"[invalid"+highlightOff) {
		t.Errorf("expected literal fallback highlight in %q", result)
	}
}

func TestHighlightPatterns_StripExistingANSI(t *testing.T) {
	// Simulate grep --color=auto over PTY: grep emits red codes around the match.
	// HighlightPatterns should strip those first, then apply cyan-bold.
	grepRedOn := "\033[01;31m"
	grepReset := "\033[m"
	grepErase := "\033[K" // erase-to-EOL that grep --color appends
	text := "user " + grepRedOn + "ERROR" + grepReset + grepErase + " happened"
	info := GrepInfo{Patterns: []string{"ERROR"}}
	result := HighlightPatterns(text, info, true)
	if !strings.Contains(result, highlightOn+"ERROR"+highlightOff) {
		t.Errorf("expected cyan-bold ERROR after stripping grep color, got %q", result)
	}
	if strings.Contains(result, grepRedOn) {
		t.Errorf("expected grep red code stripped, but it is still present in %q", result)
	}
}

func TestHighlightPatterns_NoStrip_WhenNoPatterns(t *testing.T) {
	// When GrepInfo has no patterns, text (including any ANSI codes) is returned as-is.
	grepRedOn := "\033[01;31m"
	text := "user " + grepRedOn + "ERROR\033[0m happened"
	info := GrepInfo{}
	result := HighlightPatterns(text, info, true)
	if result != text {
		t.Errorf("expected unchanged text when no patterns, got %q", result)
	}
}

func TestHighlightPatterns_RegexPattern(t *testing.T) {
	info := GrepInfo{Patterns: []string{"error|warn"}}
	text := "error occurred and warn issued"
	result := HighlightPatterns(text, info, true)
	if !strings.Contains(result, highlightOn+"error"+highlightOff) {
		t.Errorf("expected error highlighted via regex in %q", result)
	}
	if !strings.Contains(result, highlightOn+"warn"+highlightOff) {
		t.Errorf("expected warn highlighted via regex in %q", result)
	}
}
