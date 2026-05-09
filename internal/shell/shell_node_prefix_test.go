package shell

import (
	"testing"
)

func TestParseNodePrefix_NoPrefix(t *testing.T) {
	nodes, cmd, ok := parseNodePrefix("ls /var/log")
	if ok {
		t.Error("expected ok=false for input without @")
	}
	if cmd != "ls /var/log" {
		t.Errorf("expected original command, got %q", cmd)
	}
	if nodes != nil {
		t.Errorf("expected nil nodes, got %v", nodes)
	}
}

func TestParseNodePrefix_SingleNode(t *testing.T) {
	nodes, cmd, ok := parseNodePrefix("@web-1 ls /var/log")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(nodes) != 1 || nodes[0] != "web-1" {
		t.Errorf("expected [web-1], got %v", nodes)
	}
	if cmd != "ls /var/log" {
		t.Errorf("expected 'ls /var/log', got %q", cmd)
	}
}

func TestParseNodePrefix_MultipleNodes(t *testing.T) {
	nodes, cmd, ok := parseNodePrefix("@web-1,web-2,web-3 systemctl restart nginx")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %v", nodes)
	}
	if nodes[0] != "web-1" || nodes[1] != "web-2" || nodes[2] != "web-3" {
		t.Errorf("unexpected nodes: %v", nodes)
	}
	if cmd != "systemctl restart nginx" {
		t.Errorf("expected 'systemctl restart nginx', got %q", cmd)
	}
}

func TestParseNodePrefix_NoSpace(t *testing.T) {
	// "@web-1" with no command after it — should not parse
	_, cmd, ok := parseNodePrefix("@web-1")
	if ok {
		t.Error("expected ok=false when no space/command follows")
	}
	if cmd != "@web-1" {
		t.Errorf("expected original input returned, got %q", cmd)
	}
}

func TestParseNodePrefix_EmptyCommand(t *testing.T) {
	_, _, ok := parseNodePrefix("@web-1 ")
	if ok {
		t.Error("expected ok=false when command is empty after trimming")
	}
}

func TestParseNodePrefix_EmptyNodeStr(t *testing.T) {
	_, _, ok := parseNodePrefix("@ ls")
	if ok {
		t.Error("expected ok=false when node string is empty")
	}
}

func TestParseNodePrefix_SpacesAroundComma(t *testing.T) {
	// Node list uses comma without spaces; the first space always delimits the command.
	// "@web-1,web-2 uptime" is the correct format.
	nodes, cmd, ok := parseNodePrefix("@web-1,web-2 uptime")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(nodes) != 2 || nodes[0] != "web-1" || nodes[1] != "web-2" {
		t.Errorf("expected [web-1 web-2], got %v", nodes)
	}
	if cmd != "uptime" {
		t.Errorf("expected 'uptime', got %q", cmd)
	}
}

func TestParseNodePrefix_NotAtSign(t *testing.T) {
	_, _, ok := parseNodePrefix(":only web-1")
	if ok {
		t.Error("expected ok=false for non-@ prefix")
	}
}
