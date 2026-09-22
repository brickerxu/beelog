package shell

import (
	"fmt"
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

func TestHandleHistory_PatternFilter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")
	os.WriteFile(path, []byte(strings.Join([]string{
		"ls -l",
		"tail /var/log/nginx/access.log",
		"systemctl status nginx",
		"cat /etc/hosts",
		"grep ERROR /var/log/nginx/error.log",
	}, "\n")+"\n"), 0600)

	s := &interactiveShell{historyFile: path}
	out := s.handleHistory([]string{"nginx"})

	// 仅命中 nginx 相关三条；原编号 2/3/5 都要保留
	for _, want := range []string{"2  tail /var/log/nginx/access.log", "3  systemctl status nginx", "5  grep ERROR /var/log/nginx/error.log"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got:\n%s", want, out)
		}
	}
	// 不应包含未匹配行
	for _, unwant := range []string{"ls -l", "cat /etc/hosts"} {
		if strings.Contains(out, unwant) {
			t.Errorf("unexpected %q in output:\n%s", unwant, out)
		}
	}
	if !strings.Contains(out, `匹配 "nginx"`) {
		t.Errorf("expected header showing pattern, got:\n%s", out)
	}
}

func TestHandleHistory_PatternWithLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")
	var b strings.Builder
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&b, "ls -l /path/%d\n", i)
	}
	fmt.Fprintln(&b, "other cmd")
	os.WriteFile(path, []byte(b.String()), 0600)

	s := &interactiveShell{historyFile: path}
	// 有 10 条匹配 "ls"，限制显示 3 条 → 应取最近 3 条（第 8/9/10 行）
	out := s.handleHistory([]string{"ls", "3"})
	for _, want := range []string{"8  ls -l /path/8", "9  ls -l /path/9", "10  ls -l /path/10"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ls -l /path/1\n") {
		t.Errorf("earliest match should be trimmed by limit, got:\n%s", out)
	}
	if !strings.Contains(out, "3/10") {
		t.Errorf("header should show 3/10 shown/matches, got:\n%s", out)
	}
}

func TestHandleHistory_PatternNoMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")
	os.WriteFile(path, []byte("foo\nbar\n"), 0600)
	s := &interactiveShell{historyFile: path}
	if out := s.handleHistory([]string{"zzz"}); !strings.Contains(out, "未匹配到") {
		t.Errorf("expected no-match message, got: %s", out)
	}
}

func TestHandleHistory_RegexPattern(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")
	os.WriteFile(path, []byte("sudo systemctl restart nginx\nnot-sudo something\nsudo apt update\n"), 0600)

	s := &interactiveShell{historyFile: path}
	// 用锚点 ^sudo 只匹配以 sudo 开头的两条
	out := s.handleHistory([]string{"^sudo "})
	if !strings.Contains(out, "sudo systemctl restart nginx") {
		t.Errorf("expected sudo systemctl in output, got:\n%s", out)
	}
	if !strings.Contains(out, "sudo apt update") {
		t.Errorf("expected sudo apt in output, got:\n%s", out)
	}
	if strings.Contains(out, "not-sudo something") {
		t.Errorf("regex anchor should exclude not-sudo, got:\n%s", out)
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
