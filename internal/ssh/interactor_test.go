package ssh

import (
	"bytes"
	"io"
	"testing"
	"time"
)

func TestNewJumpServerInteractor(t *testing.T) {
	interactor := NewJumpServerInteractor(30 * time.Second)
	if interactor == nil {
		t.Fatal("NewJumpServerInteractor returned nil")
	}
}

func TestWaitForMenuPrompt_DetectsOptPrompt(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"Opt> prompt", "Welcome to JumpServer\r\n1) Search\r\n2) List\r\nOpt>"},
		{"opt> prompt", "Welcome\r\nopt>"},
		{"]> prompt", "Select asset ]>"},
		{"$ prompt", "user@jump $ "},
		{"# prompt", "root@jump # "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := &jumpServerInteractor{timeout: 5 * time.Second}
			reader := bytes.NewReader([]byte(tt.input))
			err := j.waitForMenuPrompt(reader)
			if err != nil {
				t.Errorf("waitForMenuPrompt failed for %q: %v", tt.name, err)
			}
		})
	}
}

func TestWaitForMenuPrompt_Timeout(t *testing.T) {
	j := &jumpServerInteractor{timeout: 100 * time.Millisecond}
	// Use a pipe that never writes anything matching
	pr, pw := io.Pipe()
	go func() {
		// Write data that doesn't match any menu prompt
		pw.Write([]byte("Loading...\r\nPlease wait...\r\n"))
		// Don't close - let it timeout
		time.Sleep(200 * time.Millisecond)
		pw.Close()
	}()

	err := j.waitForMenuPrompt(pr)
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}

func TestWaitForShellPrompt_DetectsPrompts(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"dollar prompt", "Connecting to web-1...\r\nuser@web-1:~$"},
		{"hash prompt", "root@web-1:~#"},
		{"angle prompt", "PS C:\\Users\\admin>"},
		{"dollar with space", "user@host:~$ "},
		{"hash with space", "root@host:~# "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := &jumpServerInteractor{timeout: 5 * time.Second}
			reader := bytes.NewReader([]byte(tt.input))
			err := j.waitForShellPrompt(reader)
			if err != nil {
				t.Errorf("waitForShellPrompt failed for %q: %v", tt.name, err)
			}
		})
	}
}

func TestWaitForShellPrompt_Timeout(t *testing.T) {
	j := &jumpServerInteractor{timeout: 100 * time.Millisecond}
	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte("Connecting...\r\n"))
		time.Sleep(200 * time.Millisecond)
		pw.Close()
	}()

	err := j.waitForShellPrompt(pr)
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}

func TestWaitForShellPrompt_EOF(t *testing.T) {
	j := &jumpServerInteractor{timeout: 5 * time.Second}
	// Empty reader - immediate EOF
	reader := bytes.NewReader([]byte{})
	err := j.waitForShellPrompt(reader)
	if err == nil {
		t.Error("expected EOF error, got nil")
	}
}

func TestWaitForMenuPrompt_DelayedData(t *testing.T) {
	j := &jumpServerInteractor{timeout: 5 * time.Second}
	pr, pw := io.Pipe()

	go func() {
		// Simulate JumpServer sending data in chunks with delays
		pw.Write([]byte("Welcome to JumpServer\r\n"))
		time.Sleep(50 * time.Millisecond)
		pw.Write([]byte("1) Search assets\r\n"))
		time.Sleep(50 * time.Millisecond)
		pw.Write([]byte("2) List assets\r\n"))
		time.Sleep(50 * time.Millisecond)
		pw.Write([]byte("Opt>"))
	}()

	err := j.waitForMenuPrompt(pr)
	if err != nil {
		t.Errorf("waitForMenuPrompt failed with delayed data: %v", err)
	}
}

func TestNavigateToNode_FullFlow(t *testing.T) {
	j := &jumpServerInteractor{timeout: 5 * time.Second}

	// Create pipes to simulate JumpServer interaction
	// stdout: what the JumpServer sends to us
	stdoutPR, stdoutPW := io.Pipe()
	// stdin: what we send to the JumpServer (captured for verification)
	stdinPR, stdinPW := io.Pipe()

	var capturedInput bytes.Buffer

	// Simulate JumpServer behavior in a goroutine
	go func() {
		// Step 1: JumpServer sends menu
		stdoutPW.Write([]byte("Welcome to JumpServer\r\n"))
		time.Sleep(20 * time.Millisecond)
		stdoutPW.Write([]byte("1) Search assets\r\n2) List assets\r\nOpt>"))

		// Step 2: Read what the tool sends (node name)
		buf := make([]byte, 256)
		n, _ := stdinPR.Read(buf)
		capturedInput.Write(buf[:n])

		// Step 3: JumpServer connects to node and shows shell prompt
		time.Sleep(20 * time.Millisecond)
		stdoutPW.Write([]byte("\r\nConnecting to web-1...\r\n"))
		time.Sleep(20 * time.Millisecond)
		stdoutPW.Write([]byte("user@web-1:~$ "))
	}()

	err := j.NavigateToNode(nil, stdinPW, stdoutPR, "web-1")
	if err != nil {
		t.Fatalf("NavigateToNode failed: %v", err)
	}

	// Verify the tool sent the correct node name
	sent := capturedInput.String()
	if sent != "web-1\r" {
		t.Errorf("expected sent input %q, got %q", "web-1\r", sent)
	}
}

func TestTruncateForError(t *testing.T) {
	// Short string - no truncation
	short := "hello"
	if result := truncateForError([]byte(short)); result != short {
		t.Errorf("expected %q, got %q", short, result)
	}

	// Long string - should be truncated
	long := make([]byte, 1024)
	for i := range long {
		long[i] = 'x'
	}
	result := truncateForError(long)
	if len(result) > 515 { // "..." + 512 chars
		t.Errorf("truncated result too long: %d", len(result))
	}
	if result[:3] != "..." {
		t.Error("truncated result should start with ...")
	}
}
