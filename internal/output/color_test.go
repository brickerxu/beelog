package output

import (
	"strings"
	"testing"
)

func TestNewColorPalette(t *testing.T) {
	cp := NewColorPalette(true)
	if cp == nil {
		t.Fatal("NewColorPalette returned nil")
	}
	if !cp.enabled {
		t.Error("expected enabled to be true")
	}
}

func TestColorize_Disabled_ReturnsUnchanged(t *testing.T) {
	cp := NewColorPalette(false)
	text := "hello world"
	got := cp.Colorize("web-1", text)
	if got != text {
		t.Errorf("expected %q, got %q", text, got)
	}
}

func TestColorize_Enabled_WrapsWithANSI(t *testing.T) {
	cp := NewColorPalette(true)
	got := cp.Colorize("web-1", "hello")
	if !strings.HasSuffix(got, colorReset) {
		t.Errorf("expected ANSI reset suffix, got %q", got)
	}
	if !strings.Contains(got, "hello") {
		t.Errorf("expected text to be present, got %q", got)
	}
	// Should start with an ANSI escape
	if !strings.HasPrefix(got, "\033[") {
		t.Errorf("expected ANSI escape prefix, got %q", got)
	}
}

func TestColorize_Deterministic_SameNodeSameColor(t *testing.T) {
	cp := NewColorPalette(true)
	first := cp.Colorize("web-1", "a")
	second := cp.Colorize("web-1", "b")

	// Extract color code (everything before the text)
	colorFirst := strings.SplitN(first, "a", 2)[0]
	colorSecond := strings.SplitN(second, "b", 2)[0]

	if colorFirst != colorSecond {
		t.Errorf("same node should get same color: %q vs %q", colorFirst, colorSecond)
	}
}

func TestColorize_DifferentNodes_GetDifferentColors(t *testing.T) {
	cp := NewColorPalette(true)
	nodes := []string{"web-1", "web-2", "db-1", "db-2", "cache-1", "cache-2"}
	colors := make(map[string]string)

	for _, node := range nodes {
		result := cp.Colorize(node, "x")
		// Extract color prefix (before "x")
		color := strings.SplitN(result, "x", 2)[0]
		colors[node] = color
	}

	// With 6 nodes and 6 palette colors, all should be different
	seen := make(map[string]bool)
	for _, c := range colors {
		seen[c] = true
	}
	if len(seen) != len(nodes) {
		t.Errorf("expected %d distinct colors, got %d", len(nodes), len(seen))
	}
}

func TestColorize_RotatesAfterPaletteExhausted(t *testing.T) {
	cp := NewColorPalette(true)
	// Create more nodes than palette colors (6)
	node7 := cp.Colorize("node-7", "x")
	// First assign 6 nodes to exhaust palette
	for i := 0; i < 6; i++ {
		cp.Colorize("node-"+string(rune('a'+i)), "x")
	}
	// node-7 was already assigned index 0, so it should match the first color
	node7Again := cp.Colorize("node-7", "y")
	color7 := strings.SplitN(node7, "x", 2)[0]
	color7Again := strings.SplitN(node7Again, "y", 2)[0]
	if color7 != color7Again {
		t.Errorf("node-7 color should be stable: %q vs %q", color7, color7Again)
	}
}

func TestColorize_EmptyText(t *testing.T) {
	cp := NewColorPalette(true)
	got := cp.Colorize("web-1", "")
	// Should still have color codes wrapping empty string
	if !strings.HasPrefix(got, "\033[") || !strings.HasSuffix(got, colorReset) {
		t.Errorf("expected ANSI wrapping even for empty text, got %q", got)
	}
}

func TestColorize_Disabled_EmptyText(t *testing.T) {
	cp := NewColorPalette(false)
	got := cp.Colorize("web-1", "")
	if got != "" {
		t.Errorf("expected empty string when disabled, got %q", got)
	}
}
