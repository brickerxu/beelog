package output

import (
	"regexp"
	"strings"
	"time"
)

// logTimestampRe matches a timestamp at the beginning of a log line.
// Supported formats:
//   - YYYY-MM-DD HH:MM:SS
//   - YYYY-MM-DD HH:MM:SS.mmm  or  YYYY-MM-DD HH:MM:SS mmm
//   - YYYY/MM/DD HH:MM:SS
//   - YYYY/MM/DD HH:MM:SS.mmm  or  YYYY/MM/DD HH:MM:SS mmm
var logTimestampRe = regexp.MustCompile(`^\d{4}[-/]\d{2}[-/]\d{2}\s+\d{2}:\d{2}:\d{2}(?:[.\s]\d{1,3})?`)

// ParseLogTimestamp attempts to extract a timestamp from the beginning of a log line.
// It returns the parsed time and true on success, or a zero time and false on failure.
// Milliseconds are preserved for sorting precision when present.
func ParseLogTimestamp(line string) (time.Time, bool) {
	match := logTimestampRe.FindString(line)
	if match == "" {
		return time.Time{}, false
	}

	// Unify date separators: replace '/' with '-'.
	normalized := strings.ReplaceAll(match, "/", "-")

	// Extract date part (10 chars: "YYYY-MM-DD"), skip whitespace, then time part.
	datePart := normalized[:10]
	rest := strings.TrimLeft(normalized[10:], " \t")
	if len(rest) < 8 {
		return time.Time{}, false
	}
	timePart := rest[:8] // "HH:MM:SS"
	rest = rest[8:]

	// Check for milliseconds: ".mmm" or " mmm"
	var msStr string
	if len(rest) > 0 && (rest[0] == '.' || rest[0] == ' ') {
		digits := rest[1:]
		if len(digits) > 0 && digits[0] >= '0' && digits[0] <= '9' {
			msStr = digits
		}
	}

	base := datePart + " " + timePart
	if msStr != "" {
		// Pad to 3 digits for consistent millisecond parsing
		for len(msStr) < 3 {
			msStr += "0"
		}
		t, err := time.Parse("2006-01-02 15:04:05.000", base+"."+msStr)
		if err != nil {
			return time.Time{}, false
		}
		return t, true
	}

	t, err := time.Parse(TimestampFormat, base)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
