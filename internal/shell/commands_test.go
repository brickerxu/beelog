package shell

import (
	"fmt"
	"strings"
	"testing"

	"github.com/brickerxu/beelog/internal/ssh"
)

// --- ParseSessionCommand tests ---

func TestParseSessionCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantCmd  string
		wantArgs []string
	}{
		{"quit", ":quit", "quit", nil},
		{"exit", ":exit", "exit", nil},
		{"quit uppercase", ":QUIT", "quit", nil},
		{"exit mixed case", ":Exit", "exit", nil},
		{"help", ":help", "help", nil},
		{"disconnect with node", ":disconnect web-1", "disconnect", []string{"web-1"}},
		{"disconnect uppercase", ":DISCONNECT web-1", "disconnect", []string{"web-1"}},
		{"disconnect with extra spaces", "  :disconnect   web-1  ", "disconnect", []string{"web-1"}},
		{"disconnect multiple args", ":disconnect web-1 web-2", "disconnect", []string{"web-1", "web-2"}},
		{"unknown command", ":foo", "foo", nil},
		{"empty after colon", ":", "", nil},
		{"no colon prefix", "quit", "", nil},
		{"empty string", "", "", nil},
		{"whitespace only", "   ", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, args := ParseSessionCommand(tt.input)
			if cmd != tt.wantCmd {
				t.Errorf("ParseSessionCommand(%q) cmd = %q, want %q", tt.input, cmd, tt.wantCmd)
			}
			if len(args) != len(tt.wantArgs) {
				t.Fatalf("ParseSessionCommand(%q) args len = %d, want %d", tt.input, len(args), len(tt.wantArgs))
			}
			for i, a := range args {
				if a != tt.wantArgs[i] {
					t.Errorf("ParseSessionCommand(%q) args[%d] = %q, want %q", tt.input, i, a, tt.wantArgs[i])
				}
			}
		})
	}
}

// --- HandleSessionCommand tests ---

// testSessionManager tracks calls for verification
type testSessionManager struct {
	sessions            []*ssh.NodeSession
	disconnectedNodes   []string
	disconnectAllCalled bool
	disconnectErr       error
}

func (m *testSessionManager) GetActiveSessions() []*ssh.NodeSession {
	return m.sessions
}

func (m *testSessionManager) GetAllSessions() []*ssh.NodeSession {
	return m.sessions
}

func (m *testSessionManager) DisconnectNode(nodeName string) error {
	m.disconnectedNodes = append(m.disconnectedNodes, nodeName)
	return m.disconnectErr
}

func (m *testSessionManager) DisconnectAll() error {
	m.disconnectAllCalled = true
	return nil
}

func TestHandleSessionCommand_Quit(t *testing.T) {
	mgr := &testSessionManager{}
	quit, msg := HandleSessionCommand("quit", nil, mgr)

	if !quit {
		t.Error("expected quit=true for :quit")
	}
	if msg != "" {
		t.Errorf("expected empty message for :quit, got %q", msg)
	}
	if !mgr.disconnectAllCalled {
		t.Error("expected DisconnectAll to be called")
	}
}

func TestHandleSessionCommand_Exit(t *testing.T) {
	mgr := &testSessionManager{}
	quit, msg := HandleSessionCommand("exit", nil, mgr)

	if !quit {
		t.Error("expected quit=true for :exit")
	}
	if msg != "" {
		t.Errorf("expected empty message for :exit, got %q", msg)
	}
	if !mgr.disconnectAllCalled {
		t.Error("expected DisconnectAll to be called")
	}
}

func TestHandleSessionCommand_DisconnectSuccess(t *testing.T) {
	mgr := &testSessionManager{}
	quit, msg := HandleSessionCommand("disconnect", []string{"web-1"}, mgr)

	if quit {
		t.Error("expected quit=false for :disconnect")
	}
	if !strings.Contains(msg, "web-1") {
		t.Errorf("expected message to contain node name, got %q", msg)
	}
	if len(mgr.disconnectedNodes) != 1 || mgr.disconnectedNodes[0] != "web-1" {
		t.Errorf("expected DisconnectNode called with 'web-1', got %v", mgr.disconnectedNodes)
	}
}

func TestHandleSessionCommand_DisconnectError(t *testing.T) {
	mgr := &testSessionManager{
		disconnectErr: fmt.Errorf("node not found"),
	}
	quit, msg := HandleSessionCommand("disconnect", []string{"unknown-node"}, mgr)

	if quit {
		t.Error("expected quit=false on disconnect error")
	}
	if !strings.Contains(msg, "失败") {
		t.Errorf("expected failure message, got %q", msg)
	}
	if !strings.Contains(msg, "node not found") {
		t.Errorf("expected error reason in message, got %q", msg)
	}
}

func TestHandleSessionCommand_DisconnectNoArgs(t *testing.T) {
	mgr := &testSessionManager{}
	quit, msg := HandleSessionCommand("disconnect", nil, mgr)

	if quit {
		t.Error("expected quit=false for disconnect without args")
	}
	if !strings.Contains(msg, "用法") {
		t.Errorf("expected usage hint in message, got %q", msg)
	}
}

func TestHandleSessionCommand_Help(t *testing.T) {
	mgr := &testSessionManager{}
	quit, msg := HandleSessionCommand("help", nil, mgr)

	if quit {
		t.Error("expected quit=false for :help")
	}
	if !strings.Contains(msg, ":quit") {
		t.Errorf("expected help text to mention :quit, got %q", msg)
	}
	if !strings.Contains(msg, ":disconnect") {
		t.Errorf("expected help text to mention :disconnect, got %q", msg)
	}
	if !strings.Contains(msg, ":help") {
		t.Errorf("expected help text to mention :help, got %q", msg)
	}
}

func TestHandleSessionCommand_Unknown(t *testing.T) {
	mgr := &testSessionManager{}
	quit, msg := HandleSessionCommand("foobar", nil, mgr)

	if quit {
		t.Error("expected quit=false for unknown command")
	}
	if !strings.Contains(msg, "未知会话命令") {
		t.Errorf("expected unknown command message, got %q", msg)
	}
	// Should also include help text
	if !strings.Contains(msg, ":quit") {
		t.Errorf("expected help text in unknown command response, got %q", msg)
	}
}

// --- isSessionCommand tests ---

func TestIsSessionCommand(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{":quit", true},
		{":exit", true},
		{":disconnect web-1", true},
		{":help", true},
		{":unknown", true},
		{"quit", false},
		{"ls -la", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isSessionCommand(tt.input)
			if got != tt.want {
				t.Errorf("isSessionCommand(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
