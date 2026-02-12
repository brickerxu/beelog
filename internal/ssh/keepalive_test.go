package ssh

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// mockWriteCloser is a controllable io.WriteCloser for testing heartbeat writes
type mockWriteCloser struct {
	mu         sync.Mutex
	writeCount int
	failAfter  int // if > 0, fail after this many writes
	failErr    error
	closed     bool
}

func newMockWriteCloser(failAfter int, failErr error) *mockWriteCloser {
	return &mockWriteCloser{
		failAfter: failAfter,
		failErr:   failErr,
	}
}

func (m *mockWriteCloser) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeCount++
	if m.failAfter > 0 && m.writeCount >= m.failAfter {
		return 0, m.failErr
	}
	return len(p), nil
}

func (m *mockWriteCloser) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockWriteCloser) getWriteCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeCount
}

// --- NodeSession disconnect state tests ---

func TestNodeSession_MarkDisconnected(t *testing.T) {
	session := &NodeSession{NodeName: "web-1"}

	if session.IsDisconnected() {
		t.Fatal("new session should not be disconnected")
	}

	testErr := errors.New("connection lost")
	session.MarkDisconnected(testErr)

	if !session.IsDisconnected() {
		t.Fatal("session should be disconnected after MarkDisconnected")
	}
	if session.GetDisconnectErr() != testErr {
		t.Errorf("expected disconnect error %v, got %v", testErr, session.GetDisconnectErr())
	}
}

func TestNodeSession_MarkDisconnected_ThreadSafe(t *testing.T) {
	session := &NodeSession{NodeName: "web-1"}
	var wg sync.WaitGroup

	// Concurrently mark disconnected and check state
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			session.MarkDisconnected(errors.New("err"))
		}()
		go func() {
			defer wg.Done()
			_ = session.IsDisconnected()
		}()
	}
	wg.Wait()

	if !session.IsDisconnected() {
		t.Fatal("session should be disconnected after concurrent marks")
	}
}

// --- Keepalive tests ---

func TestStartKeepalive_SendsHeartbeats(t *testing.T) {
	mgr := NewSSHConnManager(nil, nil).(*sshConnManager)
	writer := newMockWriteCloser(0, nil)
	session := &NodeSession{
		NodeName: "web-1",
		Stdin:    writer,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := mgr.StartKeepalive(ctx, session, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("StartKeepalive failed: %v", err)
	}

	// Wait for a few heartbeats
	time.Sleep(90 * time.Millisecond)
	cancel()

	count := writer.getWriteCount()
	if count < 2 {
		t.Errorf("expected at least 2 heartbeats, got %d", count)
	}
	if session.IsDisconnected() {
		t.Error("session should not be disconnected when heartbeats succeed")
	}
}

func TestStartKeepalive_MarksDisconnectedOnFailure(t *testing.T) {
	mgr := NewSSHConnManager(nil, nil).(*sshConnManager)
	writeErr := errors.New("broken pipe")
	writer := newMockWriteCloser(1, writeErr) // fail on first write
	session := &NodeSession{
		NodeName: "web-1",
		Stdin:    writer,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := mgr.StartKeepalive(ctx, session, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("StartKeepalive failed: %v", err)
	}

	// Wait for the heartbeat to fail
	time.Sleep(50 * time.Millisecond)

	if !session.IsDisconnected() {
		t.Fatal("session should be marked disconnected after heartbeat failure")
	}
	if session.GetDisconnectErr() != writeErr {
		t.Errorf("expected disconnect error %v, got %v", writeErr, session.GetDisconnectErr())
	}
}

func TestStopKeepalive_StopsHeartbeats(t *testing.T) {
	mgr := NewSSHConnManager(nil, nil).(*sshConnManager)
	writer := newMockWriteCloser(0, nil)
	session := &NodeSession{
		NodeName: "web-1",
		Stdin:    writer,
	}

	ctx := context.Background()
	err := mgr.StartKeepalive(ctx, session, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("StartKeepalive failed: %v", err)
	}

	// Let a few heartbeats happen
	time.Sleep(60 * time.Millisecond)
	mgr.StopKeepalive(session)

	countAtStop := writer.getWriteCount()
	// Wait more and verify no new heartbeats
	time.Sleep(60 * time.Millisecond)
	countAfter := writer.getWriteCount()

	if countAfter != countAtStop {
		t.Errorf("heartbeats continued after StopKeepalive: %d at stop, %d after", countAtStop, countAfter)
	}
}

func TestStartKeepalive_ReplacesExisting(t *testing.T) {
	mgr := NewSSHConnManager(nil, nil).(*sshConnManager)
	writer1 := newMockWriteCloser(0, nil)
	writer2 := newMockWriteCloser(0, nil)
	session1 := &NodeSession{NodeName: "web-1", Stdin: writer1}
	session2 := &NodeSession{NodeName: "web-1", Stdin: writer2}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start first keepalive
	mgr.StartKeepalive(ctx, session1, 20*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	// Start second keepalive for same node name - should replace
	mgr.StartKeepalive(ctx, session2, 20*time.Millisecond)

	count1AtReplace := writer1.getWriteCount()
	time.Sleep(60 * time.Millisecond)

	// First writer should have stopped receiving heartbeats
	count1After := writer1.getWriteCount()
	if count1After != count1AtReplace {
		t.Errorf("old keepalive continued after replacement: %d at replace, %d after", count1AtReplace, count1After)
	}

	// Second writer should be receiving heartbeats
	count2 := writer2.getWriteCount()
	if count2 < 1 {
		t.Error("new keepalive should be sending heartbeats")
	}
}

func TestStartKeepalive_StopsOnContextCancel(t *testing.T) {
	mgr := NewSSHConnManager(nil, nil).(*sshConnManager)
	writer := newMockWriteCloser(0, nil)
	session := &NodeSession{
		NodeName: "web-1",
		Stdin:    writer,
	}

	ctx, cancel := context.WithCancel(context.Background())
	mgr.StartKeepalive(ctx, session, 20*time.Millisecond)

	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(10 * time.Millisecond) // let goroutine exit

	countAtCancel := writer.getWriteCount()
	time.Sleep(60 * time.Millisecond)
	countAfter := writer.getWriteCount()

	if countAfter != countAtCancel {
		t.Errorf("heartbeats continued after context cancel: %d at cancel, %d after", countAtCancel, countAfter)
	}
}

func TestStartKeepalive_HeartbeatIsCarriageReturn(t *testing.T) {
	// Verify the heartbeat sends exactly "\r" (PTY mode requires \r)
	mgr := NewSSHConnManager(nil, nil).(*sshConnManager)

	var captured []byte
	var capMu sync.Mutex
	pr, pw := io.Pipe()

	go func() {
		buf := make([]byte, 64)
		for {
			n, err := pr.Read(buf)
			if err != nil {
				return
			}
			capMu.Lock()
			captured = append(captured, buf[:n]...)
			capMu.Unlock()
		}
	}()

	session := &NodeSession{
		NodeName: "web-1",
		Stdin:    pw,
	}

	ctx, cancel := context.WithCancel(context.Background())
	mgr.StartKeepalive(ctx, session, 15*time.Millisecond)
	time.Sleep(40 * time.Millisecond)
	cancel()
	time.Sleep(10 * time.Millisecond)
	pw.Close()

	capMu.Lock()
	defer capMu.Unlock()

	if len(captured) == 0 {
		t.Fatal("no heartbeat data captured")
	}
	for i, b := range captured {
		if b != '\r' {
			t.Errorf("heartbeat byte %d is %q, expected '\\r'", i, b)
		}
	}
}
