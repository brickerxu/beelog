package shell

import (
	"context"
	"testing"

	"github.com/brickerxu/beelog/internal/executor"
	"github.com/brickerxu/beelog/internal/ssh"
)

// mockSessionManager 用于测试的 SessionManager mock
type mockSessionManager struct {
	sessions []*ssh.NodeSession
}

func (m *mockSessionManager) GetActiveSessions() []*ssh.NodeSession {
	return m.sessions
}

func (m *mockSessionManager) DisconnectNode(nodeName string) error {
	return nil
}

func (m *mockSessionManager) DisconnectAll() error {
	return nil
}

// mockExecFn 创建一个返回固定输出的 ExecFunc
func mockExecFn(output string) executor.ExecFunc {
	return func(ctx context.Context, session executor.Session, command string) (*executor.ExecResult, error) {
		return &executor.ExecResult{
			NodeName: session.GetNodeName(),
			Output:   output,
		}, nil
	}
}

func TestExtractLastWord(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected string
	}{
		{"empty line", "", ""},
		{"single word", "cat", "cat"},
		{"command with path", "cat /var/log/", "/var/log/"},
		{"command with partial path", "ls /etc/sys", "/etc/sys"},
		{"multiple spaces", "grep pattern  /var/", "/var/"},
		{"trailing spaces trimmed", "ls /tmp   ", "/tmp"},
		{"relative path", "cat logs/app", "logs/app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractLastWord(tt.line)
			if got != tt.expected {
				t.Errorf("extractLastWord(%q) = %q, want %q", tt.line, got, tt.expected)
			}
		})
	}
}

func TestParseLsOutput(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected []string
	}{
		{"empty output", "", nil},
		{"whitespace only", "   \n  \n  ", nil},
		{"single file", "app.log\n", []string{"app.log"}},
		{"multiple files", "app.log\nerror.log\naccess.log\n", []string{"app.log", "error.log", "access.log"}},
		{"directories with slash", "bin/\netc/\nvar/\n", []string{"bin/", "etc/", "var/"}},
		{"mixed files and dirs", "README.md\nsrc/\ngo.mod\n", []string{"README.md", "src/", "go.mod"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLsOutput(tt.output)
			if len(got) != len(tt.expected) {
				t.Fatalf("parseLsOutput() returned %d items, want %d", len(got), len(tt.expected))
			}
			for i, v := range got {
				if v != tt.expected[i] {
					t.Errorf("parseLsOutput()[%d] = %q, want %q", i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestShellEscape(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"no special chars", "/var/log/app", "/var/log/app"},
		{"space in path", "/my dir/file", "/my\\ dir/file"},
		{"parentheses", "file(1)", "file\\(1\\)"},
		{"normal path", "/etc/nginx/", "/etc/nginx/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellEscape(tt.input)
			if got != tt.expected {
				t.Errorf("shellEscape(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestRemoteCompleterDo_NoActiveSessions(t *testing.T) {
	sessMgr := &mockSessionManager{sessions: nil}
	completer := NewRemoteCompleter(sessMgr, mockExecFn(""))

	line := []rune("ls /var/lo")
	newLine, length := completer.Do(line, len(line))

	if len(newLine) != 0 {
		t.Errorf("expected no candidates when no active sessions, got %d", len(newLine))
	}
	if length != 0 {
		t.Errorf("expected length 0, got %d", length)
	}
}

func TestRemoteCompleterDo_WithCandidates(t *testing.T) {
	session := &ssh.NodeSession{NodeName: "test-node"}
	sessMgr := &mockSessionManager{sessions: []*ssh.NodeSession{session}}

	// Mock ls output returning files that match the partial path
	execFn := mockExecFn("/var/log/app.log\n/var/log/access.log\n")
	completer := NewRemoteCompleter(sessMgr, execFn)

	line := []rune("cat /var/log/a")
	newLine, length := completer.Do(line, len(line))

	// Should return suffix candidates with shared prefix length for display
	if len(newLine) == 0 {
		t.Error("expected candidates, got none")
	}
	// length = len("/var/log/a") for display purposes
	if length != len([]rune("/var/log/a")) {
		t.Errorf("expected length %d, got %d", len([]rune("/var/log/a")), length)
	}
}

func TestRemoteCompleterDo_SingleMatch(t *testing.T) {
	session := &ssh.NodeSession{NodeName: "test-node"}
	sessMgr := &mockSessionManager{sessions: []*ssh.NodeSession{session}}

	// Single match: /www/hwyc-content/logs
	execFn := mockExecFn("/www/hwyc-content/logs\n")
	completer := NewRemoteCompleter(sessMgr, execFn)

	line := []rune("cd /www/hwyc-content/lo")
	newLine, length := completer.Do(line, len(line))

	if len(newLine) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(newLine))
	}
	// Should return suffix "gs" to append (not full path)
	if string(newLine[0]) != "gs" {
		t.Errorf("expected suffix %q, got %q", "gs", string(newLine[0]))
	}
	// length=0 for single match append
	if length != 0 {
		t.Errorf("expected length 0, got %d", length)
	}
}

func TestRemoteCompleterDo_SingleMatch_AlreadyComplete(t *testing.T) {
	session := &ssh.NodeSession{NodeName: "test-node"}
	sessMgr := &mockSessionManager{sessions: []*ssh.NodeSession{session}}

	// Already fully typed: /www matches /www
	execFn := mockExecFn("/www\n")
	completer := NewRemoteCompleter(sessMgr, execFn)

	line := []rune("cd /www")
	newLine, length := completer.Do(line, len(line))

	// Already complete, nothing to append
	if len(newLine) != 0 {
		t.Errorf("expected no candidates when already complete, got %d: %v", len(newLine), newLine)
	}
	if length != 0 {
		t.Errorf("expected length 0, got %d", length)
	}
}

func TestRemoteCompleterDo_MultipleMatchCommonPrefix(t *testing.T) {
	session := &ssh.NodeSession{NodeName: "test-node"}
	sessMgr := &mockSessionManager{sessions: []*ssh.NodeSession{session}}

	// Two matches with common prefix beyond what's typed
	execFn := mockExecFn("/var/log/app.log\n/var/log/app.err\n")
	completer := NewRemoteCompleter(sessMgr, execFn)

	line := []rune("cat /var/log/a")
	newLine, length := completer.Do(line, len(line))

	if len(newLine) != 1 {
		t.Fatalf("expected 1 common prefix candidate, got %d", len(newLine))
	}
	// Common prefix is "/var/log/app.", suffix to append is "pp."
	if string(newLine[0]) != "pp." {
		t.Errorf("expected suffix %q, got %q", "pp.", string(newLine[0]))
	}
	if length != 0 {
		t.Errorf("expected length 0, got %d", length)
	}
}

func TestRemoteCompleterDo_EmptyLsOutput(t *testing.T) {
	session := &ssh.NodeSession{NodeName: "test-node"}
	sessMgr := &mockSessionManager{sessions: []*ssh.NodeSession{session}}

	execFn := mockExecFn("")
	completer := NewRemoteCompleter(sessMgr, execFn)

	line := []rune("cat /nonexistent/path")
	newLine, length := completer.Do(line, len(line))

	if len(newLine) != 0 {
		t.Errorf("expected no candidates for nonexistent path, got %d", len(newLine))
	}
	if length != 0 {
		t.Errorf("expected length 0, got %d", length)
	}
}

func TestRemoteCompleterDo_ExecError(t *testing.T) {
	session := &ssh.NodeSession{NodeName: "test-node"}
	sessMgr := &mockSessionManager{sessions: []*ssh.NodeSession{session}}

	// ExecFunc that returns an error
	execFn := func(ctx context.Context, session executor.Session, command string) (*executor.ExecResult, error) {
		return nil, context.DeadlineExceeded
	}
	completer := NewRemoteCompleter(sessMgr, execFn)

	line := []rune("ls /some/path")
	newLine, length := completer.Do(line, len(line))

	if len(newLine) != 0 {
		t.Errorf("expected no candidates on exec error, got %d", len(newLine))
	}
	if length != 0 {
		t.Errorf("expected length 0, got %d", length)
	}
}

func TestParseLsOutput_FiltersPromptAndMarker(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected []string
	}{
		{
			"filters shell prompt with @",
			"/www/hwyc-content/logs\n[root@as-11 /www/hwyc-content]#\n",
			[]string{"/www/hwyc-content/logs"},
		},
		{
			"filters $? marker remnant",
			"/www/hwyc-content/logs\n$?\n",
			[]string{"/www/hwyc-content/logs"},
		},
		{
			"filters BEELOG marker",
			"/www/hwyc-content/logs\n__BEELOG_END_12345__ 0\n",
			[]string{"/www/hwyc-content/logs"},
		},
		{
			"filters command echo",
			"ls -1 -d /www/hwyc-content/lo* 2>/dev/null\n/www/hwyc-content/logs\n",
			[]string{"/www/hwyc-content/logs"},
		},
		{
			"filters prompt ending with $",
			"/var/log/app.log\nuser@host:~$\n",
			[]string{"/var/log/app.log"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLsOutput(tt.output)
			if len(got) != len(tt.expected) {
				t.Fatalf("parseLsOutput() returned %d items %v, want %d items %v", len(got), got, len(tt.expected), tt.expected)
			}
			for i, v := range got {
				if v != tt.expected[i] {
					t.Errorf("parseLsOutput()[%d] = %q, want %q", i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestIsShellPromptLine(t *testing.T) {
	tests := []struct {
		line     string
		expected bool
	}{
		{"[root@as-11 /www/hwyc-content]#", true},
		{"user@host:~$", true},
		{"[admin@server /tmp]$", true},
		{"/www/hwyc-content/logs", false},
		{"app.log", false},
		{"bin/", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got := isShellPromptLine(tt.line)
			if got != tt.expected {
				t.Errorf("isShellPromptLine(%q) = %v, want %v", tt.line, got, tt.expected)
			}
		})
	}
}
