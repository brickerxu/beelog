package output

import (
	"fmt"
	"os"
	"sync"

	"golang.org/x/term"
)

// ANSI color codes for node differentiation
const (
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorReset   = "\033[0m"
)

// colorPalette is a rotating palette of distinct ANSI colors
var colorPalette = []string{
	colorRed,
	colorGreen,
	colorYellow,
	colorBlue,
	colorMagenta,
	colorCyan,
}

// ColorPalette assigns different ANSI colors to different node names.
// It is safe for concurrent use.
type ColorPalette struct {
	enabled    bool
	mu         sync.Mutex
	nodeColors map[string]string
	nextIndex  int
}

// NewColorPalette creates a ColorPalette. When enabled is false, Colorize
// returns text unchanged.
func NewColorPalette(enabled bool) *ColorPalette {
	return &ColorPalette{
		enabled:    enabled,
		nodeColors: make(map[string]string),
	}
}

// Colorize wraps text with the ANSI color assigned to nodeName.
// The same nodeName always receives the same color (deterministic).
// When color is disabled, returns text unchanged.
func (cp *ColorPalette) Colorize(nodeName, text string) string {
	if !cp.enabled {
		return text
	}

	cp.mu.Lock()
	color, ok := cp.nodeColors[nodeName]
	if !ok {
		color = colorPalette[cp.nextIndex%len(colorPalette)]
		cp.nodeColors[nodeName] = color
		cp.nextIndex++
	}
	cp.mu.Unlock()

	return fmt.Sprintf("%s%s%s", color, text, colorReset)
}

// IsColorSupported checks whether the current terminal supports color output.
// It returns false if:
//   - NO_COLOR env var is set (https://no-color.org/)
//   - TERM is "dumb"
//   - stdout is not a terminal (e.g. piped to a file)
func IsColorSupported() bool {
	// Respect NO_COLOR convention
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}

	// TERM=dumb means no color support
	if os.Getenv("TERM") == "dumb" {
		return false
	}

	// Check if stdout is a terminal
	return term.IsTerminal(int(os.Stdout.Fd()))
}
