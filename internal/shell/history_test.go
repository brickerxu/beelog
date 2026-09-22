package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBangRef(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantN   int
		wantHit bool
	}{
		{"single digit", "!3", 3, true},
		{"multi digit", "!42", 42, true},
		{"just bang", "!", 0, false},
		{"bang letters", "!abc", 0, false},
		{"trailing text", "!3 foo", 0, false},
		{"leading space handled by trim outside", " !3", 0, false},
		{"zero not allowed", "!0", 0, false},
		{"no bang", "3", 0, false},
		{"empty", "", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, ok := parseBangRef(tt.input)
			if ok != tt.wantHit {
				t.Fatalf("parseBangRef(%q) ok = %v, want %v", tt.input, ok, tt.wantHit)
			}
			if ok && n != tt.wantN {
				t.Errorf("parseBangRef(%q) n = %d, want %d", tt.input, n, tt.wantN)
			}
		})
	}
}

func TestReadHistoryLines_FiltersMetaEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")
	content := "ls -l\n:history\n!3\ntail /var/log/x\n\n:history 10\ncat foo\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := readHistoryLines(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ls -l", "tail /var/log/x", "cat foo"}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %v", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("entry[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestReadHistoryLines_MissingFile(t *testing.T) {
	got, err := readHistoryLines(filepath.Join(t.TempDir(), "no-such-file"))
	if err != nil {
		t.Fatalf("missing file should return nil,nil, got err: %v", err)
	}
	if got != nil {
		t.Errorf("missing file should return nil, got %v", got)
	}
}

func TestLookupHistoryEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")
	os.WriteFile(path, []byte("a\nb\nc\n"), 0600)

	if got, err := lookupHistoryEntry(path, 2); err != nil || got != "b" {
		t.Errorf("lookup 2 = (%q, %v), want (b, nil)", got, err)
	}
	if _, err := lookupHistoryEntry(path, 0); err == nil {
		t.Error("lookup 0 should error")
	}
	if _, err := lookupHistoryEntry(path, 99); err == nil {
		t.Error("lookup 99 should error")
	} else if !strings.Contains(err.Error(), "越界") {
		t.Errorf("expected 越界 in error, got %q", err.Error())
	}
}

func TestHandleHistory_Basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")
	os.WriteFile(path, []byte("first\nsecond\nthird\n"), 0600)

	s := &interactiveShell{historyFile: path}
	out := s.handleHistory(nil)

	for _, want := range []string{"1  first", "2  second", "3  third"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got:\n%s", want, out)
		}
	}
}

func TestHandleHistory_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty")
	os.WriteFile(path, []byte(""), 0600)
	s := &interactiveShell{historyFile: path}
	if out := s.handleHistory(nil); !strings.Contains(out, "暂无历史命令") {
		t.Errorf("expected empty message, got: %s", out)
	}
}

func TestHandleHistory_Limit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")
	var b strings.Builder
	for i := 1; i <= 50; i++ {
		b.WriteString("cmd")
		b.WriteString(strings.Repeat("x", 0))
		b.WriteString("\n")
	}
	os.WriteFile(path, []byte(b.String()), 0600)

	s := &interactiveShell{historyFile: path}
	out := s.handleHistory([]string{"5"})
	// 5 entries plus header + trailing hint = 7 lines
	if got := strings.Count(out, "\n"); got < 6 || got > 7 {
		t.Errorf("expected ~7 lines with limit=5, got %d:\n%s", got, out)
	}
}
