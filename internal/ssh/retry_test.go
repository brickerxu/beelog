package ssh

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brickerxu/beelog/internal/config"
	"github.com/brickerxu/beelog/internal/executor"
)

// mockSSHConnManager 用于测试的 mock
type mockSSHConnManager struct {
	connectFunc        func(ctx context.Context, jumpCfg config.JumpServerConfig, nodeName string) (*NodeSession, error)
	executeFunc        func(ctx context.Context, session *NodeSession, command string) (*executor.ExecResult, error)
	streamFunc         func(ctx context.Context, session *NodeSession, command string, output chan<- executor.OutputLine) error
	closeFunc          func(session *NodeSession) error
	startKeepaliveFunc func(ctx context.Context, session *NodeSession, interval time.Duration) error
	stopKeepaliveFunc  func(session *NodeSession) error
}

func (m *mockSSHConnManager) Connect(ctx context.Context, jumpCfg config.JumpServerConfig, nodeName string) (*NodeSession, error) {
	if m.connectFunc != nil {
		return m.connectFunc(ctx, jumpCfg, nodeName)
	}
	return &NodeSession{NodeName: nodeName}, nil
}

func (m *mockSSHConnManager) Execute(ctx context.Context, session *NodeSession, command string) (*executor.ExecResult, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, session, command)
	}
	return &executor.ExecResult{NodeName: session.NodeName}, nil
}

func (m *mockSSHConnManager) Stream(ctx context.Context, session *NodeSession, command string, output chan<- executor.OutputLine) error {
	if m.streamFunc != nil {
		return m.streamFunc(ctx, session, command, output)
	}
	return nil
}

func (m *mockSSHConnManager) Close(session *NodeSession) error {
	if m.closeFunc != nil {
		return m.closeFunc(session)
	}
	return nil
}

func (m *mockSSHConnManager) StartKeepalive(ctx context.Context, session *NodeSession, interval time.Duration) error {
	if m.startKeepaliveFunc != nil {
		return m.startKeepaliveFunc(ctx, session, interval)
	}
	return nil
}

func (m *mockSSHConnManager) StopKeepalive(session *NodeSession) error {
	if m.stopKeepaliveFunc != nil {
		return m.stopKeepaliveFunc(session)
	}
	return nil
}

func TestConnectWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	var attempts int32
	mock := &mockSSHConnManager{
		connectFunc: func(ctx context.Context, jumpCfg config.JumpServerConfig, nodeName string) (*NodeSession, error) {
			atomic.AddInt32(&attempts, 1)
			return &NodeSession{NodeName: nodeName}, nil
		},
	}

	session, err := ConnectWithRetry(
		context.Background(), mock,
		config.JumpServerConfig{}, "web-1",
		3, 10*time.Millisecond,
	)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if session.NodeName != "web-1" {
		t.Errorf("expected NodeName web-1, got %s", session.NodeName)
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("expected 1 attempt, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestConnectWithRetry_SuccessAfterRetries(t *testing.T) {
	var attempts int32
	mock := &mockSSHConnManager{
		connectFunc: func(ctx context.Context, jumpCfg config.JumpServerConfig, nodeName string) (*NodeSession, error) {
			n := atomic.AddInt32(&attempts, 1)
			if n < 3 {
				return nil, fmt.Errorf("connection refused")
			}
			return &NodeSession{NodeName: nodeName}, nil
		},
	}

	session, err := ConnectWithRetry(
		context.Background(), mock,
		config.JumpServerConfig{}, "web-1",
		3, 10*time.Millisecond,
	)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if session.NodeName != "web-1" {
		t.Errorf("expected NodeName web-1, got %s", session.NodeName)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestConnectWithRetry_ExhaustedRetries(t *testing.T) {
	var attempts int32
	mock := &mockSSHConnManager{
		connectFunc: func(ctx context.Context, jumpCfg config.JumpServerConfig, nodeName string) (*NodeSession, error) {
			atomic.AddInt32(&attempts, 1)
			return nil, fmt.Errorf("connection refused")
		},
	}

	session, err := ConnectWithRetry(
		context.Background(), mock,
		config.JumpServerConfig{}, "db-1",
		3, 10*time.Millisecond,
	)

	if session != nil {
		t.Fatal("expected nil session")
	}
	if err == nil {
		t.Fatal("expected error")
	}
	// 1 initial + 3 retries = 4 total attempts
	if atomic.LoadInt32(&attempts) != 4 {
		t.Errorf("expected 4 attempts (1 initial + 3 retries), got %d", atomic.LoadInt32(&attempts))
	}
	// Error should mention retry count and node name
	errMsg := err.Error()
	if !contains(errMsg, "db-1") {
		t.Errorf("error should contain node name 'db-1': %s", errMsg)
	}
	if !contains(errMsg, "3") {
		t.Errorf("error should contain retry count: %s", errMsg)
	}
}

func TestConnectWithRetry_ZeroRetries(t *testing.T) {
	var attempts int32
	mock := &mockSSHConnManager{
		connectFunc: func(ctx context.Context, jumpCfg config.JumpServerConfig, nodeName string) (*NodeSession, error) {
			atomic.AddInt32(&attempts, 1)
			return nil, fmt.Errorf("connection refused")
		},
	}

	_, err := ConnectWithRetry(
		context.Background(), mock,
		config.JumpServerConfig{}, "web-1",
		0, 10*time.Millisecond,
	)

	if err == nil {
		t.Fatal("expected error")
	}
	// 0 retries means only 1 attempt
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("expected 1 attempt with 0 retries, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestConnectWithRetry_ContextCancelledDuringWait(t *testing.T) {
	var attempts int32
	mock := &mockSSHConnManager{
		connectFunc: func(ctx context.Context, jumpCfg config.JumpServerConfig, nodeName string) (*NodeSession, error) {
			atomic.AddInt32(&attempts, 1)
			return nil, fmt.Errorf("connection refused")
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay to interrupt the retry wait
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	_, err := ConnectWithRetry(
		ctx, mock,
		config.JumpServerConfig{}, "web-1",
		5, 500*time.Millisecond, // Long delay so cancel hits during wait
	)

	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
	// Should have attempted at least once but not all 6 times
	a := atomic.LoadInt32(&attempts)
	if a < 1 || a > 2 {
		t.Errorf("expected 1-2 attempts before cancel, got %d", a)
	}
}

func TestConnectWithRetry_ContextAlreadyCancelled(t *testing.T) {
	mock := &mockSSHConnManager{
		connectFunc: func(ctx context.Context, jumpCfg config.JumpServerConfig, nodeName string) (*NodeSession, error) {
			return nil, ctx.Err()
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := ConnectWithRetry(
		ctx, mock,
		config.JumpServerConfig{}, "web-1",
		3, 10*time.Millisecond,
	)

	if err == nil {
		t.Fatal("expected error")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
