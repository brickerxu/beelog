package executor

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// mockSession 模拟 Session 接口
type mockSession struct {
	nodeName     string
	disconnected bool
}

func (m *mockSession) GetNodeName() string      { return m.nodeName }
func (m *mockSession) GetStdin() io.WriteCloser { return nil }
func (m *mockSession) GetStdout() io.Reader     { return nil }
func (m *mockSession) GetStderr() io.Reader     { return nil }
func (m *mockSession) IsDisconnected() bool     { return m.disconnected }

// newMockSession 创建模拟会话
func newMockSession(name string) *mockSession {
	return &mockSession{nodeName: name}
}

// newDisconnectedSession 创建已断开的模拟会话
func newDisconnectedSession(name string) *mockSession {
	return &mockSession{nodeName: name, disconnected: true}
}

// --- ExecOnAll Tests ---

func TestExecOnAll_EmptySessions(t *testing.T) {
	exec := NewExecutor(
		func(ctx context.Context, s Session, cmd string) (*ExecResult, error) {
			return nil, fmt.Errorf("should not be called")
		},
		nil,
		5,
	)

	result, err := exec.ExecOnAll(context.Background(), []Session{}, "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(result.Results))
	}
	if result.Summary.Total != 0 {
		t.Fatalf("expected total 0, got %d", result.Summary.Total)
	}
}

func TestExecOnAll_AllSucceed(t *testing.T) {
	sessions := []Session{
		newMockSession("node-1"),
		newMockSession("node-2"),
		newMockSession("node-3"),
	}

	exec := NewExecutor(
		func(ctx context.Context, s Session, cmd string) (*ExecResult, error) {
			return &ExecResult{
				NodeName: s.GetNodeName(),
				Output:   "ok from " + s.GetNodeName(),
				ExitCode: 0,
				Duration: 10 * time.Millisecond,
			}, nil
		},
		nil,
		5,
	)

	result, err := exec.ExecOnAll(context.Background(), sessions, "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result.Results))
	}
	if result.Summary.Total != 3 {
		t.Fatalf("expected total 3, got %d", result.Summary.Total)
	}
	if result.Summary.Succeeded != 3 {
		t.Fatalf("expected 3 succeeded, got %d", result.Summary.Succeeded)
	}
	if result.Summary.Failed != 0 {
		t.Fatalf("expected 0 failed, got %d", result.Summary.Failed)
	}
}

func TestExecOnAll_PartialFailure(t *testing.T) {
	sessions := []Session{
		newMockSession("node-1"),
		newMockSession("node-2"),
		newMockSession("node-3"),
	}

	exec := NewExecutor(
		func(ctx context.Context, s Session, cmd string) (*ExecResult, error) {
			if s.GetNodeName() == "node-2" {
				return &ExecResult{
					NodeName: s.GetNodeName(),
					Error:    fmt.Errorf("connection lost"),
					ExitCode: -1,
				}, fmt.Errorf("connection lost")
			}
			return &ExecResult{
				NodeName: s.GetNodeName(),
				Output:   "ok",
				ExitCode: 0,
			}, nil
		},
		nil,
		5,
	)

	result, err := exec.ExecOnAll(context.Background(), sessions, "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result.Results))
	}

	// node-2 failed with ExitCode -1 → counted as Skipped
	if result.Summary.Skipped != 1 {
		t.Fatalf("expected 1 skipped, got %d", result.Summary.Skipped)
	}
	if result.Summary.Succeeded != 2 {
		t.Fatalf("expected 2 succeeded, got %d", result.Summary.Succeeded)
	}
}

func TestExecOnAll_DisconnectedSessionSkipped(t *testing.T) {
	sessions := []Session{
		newMockSession("node-1"),
		newDisconnectedSession("node-2"),
		newMockSession("node-3"),
	}

	callCount := int32(0)
	exec := NewExecutor(
		func(ctx context.Context, s Session, cmd string) (*ExecResult, error) {
			atomic.AddInt32(&callCount, 1)
			return &ExecResult{
				NodeName: s.GetNodeName(),
				Output:   "ok",
				ExitCode: 0,
			}, nil
		},
		nil,
		5,
	)

	result, err := exec.ExecOnAll(context.Background(), sessions, "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result.Results))
	}

	// execFn should only be called for 2 non-disconnected nodes
	if atomic.LoadInt32(&callCount) != 2 {
		t.Fatalf("expected execFn called 2 times, got %d", callCount)
	}

	// node-2 should be skipped
	if result.Summary.Skipped != 1 {
		t.Fatalf("expected 1 skipped, got %d", result.Summary.Skipped)
	}
	if result.Summary.Succeeded != 2 {
		t.Fatalf("expected 2 succeeded, got %d", result.Summary.Succeeded)
	}
}

func TestExecOnAll_ConcurrencyLimit(t *testing.T) {
	const maxConcurrency = 2
	const nodeCount = 6

	sessions := make([]Session, nodeCount)
	for i := 0; i < nodeCount; i++ {
		sessions[i] = newMockSession(fmt.Sprintf("node-%d", i))
	}

	var currentConcurrency int32
	var maxObserved int32

	exec := NewExecutor(
		func(ctx context.Context, s Session, cmd string) (*ExecResult, error) {
			cur := atomic.AddInt32(&currentConcurrency, 1)
			// Track max observed concurrency
			for {
				old := atomic.LoadInt32(&maxObserved)
				if cur <= old || atomic.CompareAndSwapInt32(&maxObserved, old, cur) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond) // simulate work
			atomic.AddInt32(&currentConcurrency, -1)
			return &ExecResult{
				NodeName: s.GetNodeName(),
				Output:   "ok",
				ExitCode: 0,
			}, nil
		},
		nil,
		maxConcurrency,
	)

	result, err := exec.ExecOnAll(context.Background(), sessions, "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Results) != nodeCount {
		t.Fatalf("expected %d results, got %d", nodeCount, len(result.Results))
	}

	observed := atomic.LoadInt32(&maxObserved)
	if observed > int32(maxConcurrency) {
		t.Fatalf("concurrency exceeded limit: observed %d, limit %d", observed, maxConcurrency)
	}
}

func TestExecOnAll_ContextCancellation(t *testing.T) {
	sessions := []Session{
		newMockSession("node-1"),
		newMockSession("node-2"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	exec := NewExecutor(
		func(ctx context.Context, s Session, cmd string) (*ExecResult, error) {
			return &ExecResult{
				NodeName: s.GetNodeName(),
				Output:   "ok",
				ExitCode: 0,
			}, nil
		},
		nil,
		5,
	)

	result, err := exec.ExecOnAll(ctx, sessions, "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// All results should be present (either executed or cancelled)
	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}
}

func TestExecOnAll_NonZeroExitCode(t *testing.T) {
	sessions := []Session{
		newMockSession("node-1"),
	}

	exec := NewExecutor(
		func(ctx context.Context, s Session, cmd string) (*ExecResult, error) {
			return &ExecResult{
				NodeName: s.GetNodeName(),
				Output:   "command not found",
				ExitCode: 127,
			}, nil
		},
		nil,
		5,
	)

	result, err := exec.ExecOnAll(context.Background(), sessions, "badcmd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Summary.Failed != 1 {
		t.Fatalf("expected 1 failed, got %d", result.Summary.Failed)
	}
	if len(result.Summary.Failures) != 1 {
		t.Fatalf("expected 1 failure info, got %d", len(result.Summary.Failures))
	}
	if !strings.Contains(result.Summary.Failures[0].Reason, "exit code: 127") {
		t.Fatalf("expected failure reason to contain exit code, got: %s", result.Summary.Failures[0].Reason)
	}
}

// --- StreamOnAll Tests ---

func TestStreamOnAll_EmptySessions(t *testing.T) {
	exec := NewExecutor(
		nil,
		func(ctx context.Context, s Session, cmd string, output chan<- OutputLine) error {
			return fmt.Errorf("should not be called")
		},
		5,
	)

	output := make(chan OutputLine, 10)
	err := exec.StreamOnAll(context.Background(), []Session{}, "tail -f /var/log/app.log", output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStreamOnAll_AllSucceed(t *testing.T) {
	sessions := []Session{
		newMockSession("node-1"),
		newMockSession("node-2"),
	}

	exec := NewExecutor(
		nil,
		func(ctx context.Context, s Session, cmd string, output chan<- OutputLine) error {
			output <- OutputLine{
				NodeName:  s.GetNodeName(),
				Content:   "line from " + s.GetNodeName(),
				Timestamp: time.Now(),
			}
			return nil
		},
		5,
	)

	output := make(chan OutputLine, 10)
	err := exec.StreamOnAll(context.Background(), sessions, "tail -f log", output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Collect output lines
	close(output)
	var lines []OutputLine
	for line := range output {
		lines = append(lines, line)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 output lines, got %d", len(lines))
	}
}

func TestStreamOnAll_DisconnectedSkipped(t *testing.T) {
	sessions := []Session{
		newMockSession("node-1"),
		newDisconnectedSession("node-2"),
	}

	streamCalled := int32(0)
	exec := NewExecutor(
		nil,
		func(ctx context.Context, s Session, cmd string, output chan<- OutputLine) error {
			atomic.AddInt32(&streamCalled, 1)
			output <- OutputLine{
				NodeName:  s.GetNodeName(),
				Content:   "data",
				Timestamp: time.Now(),
			}
			return nil
		},
		5,
	)

	output := make(chan OutputLine, 10)
	err := exec.StreamOnAll(context.Background(), sessions, "tail -f log", output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if atomic.LoadInt32(&streamCalled) != 1 {
		t.Fatalf("expected streamFn called 1 time, got %d", streamCalled)
	}
}

func TestStreamOnAll_ContextCancellation(t *testing.T) {
	sessions := []Session{
		newMockSession("node-1"),
	}

	exec := NewExecutor(
		nil,
		func(ctx context.Context, s Session, cmd string, output chan<- OutputLine) error {
			<-ctx.Done()
			return ctx.Err()
		},
		5,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	output := make(chan OutputLine, 10)
	err := exec.StreamOnAll(ctx, sessions, "tail -f log", output)
	// context cancellation should not be treated as error
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStreamOnAll_PartialFailure(t *testing.T) {
	sessions := []Session{
		newMockSession("node-1"),
		newMockSession("node-2"),
	}

	exec := NewExecutor(
		nil,
		func(ctx context.Context, s Session, cmd string, output chan<- OutputLine) error {
			if s.GetNodeName() == "node-2" {
				return fmt.Errorf("stream error")
			}
			output <- OutputLine{
				NodeName:  s.GetNodeName(),
				Content:   "data",
				Timestamp: time.Now(),
			}
			return nil
		},
		5,
	)

	output := make(chan OutputLine, 10)
	err := exec.StreamOnAll(context.Background(), sessions, "tail -f log", output)
	if err == nil {
		t.Fatal("expected error for partial failure")
	}
	if !strings.Contains(err.Error(), "node-2") {
		t.Fatalf("expected error to mention node-2, got: %v", err)
	}
}

func TestStreamOnAll_ConcurrencyLimit(t *testing.T) {
	const maxConcurrency = 2
	const nodeCount = 5

	sessions := make([]Session, nodeCount)
	for i := 0; i < nodeCount; i++ {
		sessions[i] = newMockSession(fmt.Sprintf("node-%d", i))
	}

	var currentConcurrency int32
	var maxObserved int32

	exec := NewExecutor(
		nil,
		func(ctx context.Context, s Session, cmd string, output chan<- OutputLine) error {
			cur := atomic.AddInt32(&currentConcurrency, 1)
			for {
				old := atomic.LoadInt32(&maxObserved)
				if cur <= old || atomic.CompareAndSwapInt32(&maxObserved, old, cur) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt32(&currentConcurrency, -1)
			return nil
		},
		maxConcurrency,
	)

	output := make(chan OutputLine, 100)
	err := exec.StreamOnAll(context.Background(), sessions, "tail -f log", output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	observed := atomic.LoadInt32(&maxObserved)
	if observed > int32(maxConcurrency) {
		t.Fatalf("concurrency exceeded limit: observed %d, limit %d", observed, maxConcurrency)
	}
}

// --- NewExecutor boundary tests ---

func TestNewExecutor_ConcurrencyClampedToMin(t *testing.T) {
	exec := NewExecutor(
		func(ctx context.Context, s Session, cmd string) (*ExecResult, error) {
			return &ExecResult{NodeName: s.GetNodeName(), ExitCode: 0}, nil
		},
		nil,
		0, // below minimum
	)
	// Should still work (clamped to 2)
	result, err := exec.ExecOnAll(context.Background(), []Session{newMockSession("n1")}, "echo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
}

func TestNewExecutor_ConcurrencyClampedToMax(t *testing.T) {
	exec := NewExecutor(
		func(ctx context.Context, s Session, cmd string) (*ExecResult, error) {
			return &ExecResult{NodeName: s.GetNodeName(), ExitCode: 0}, nil
		},
		nil,
		100, // above maximum
	)
	result, err := exec.ExecOnAll(context.Background(), []Session{newMockSession("n1")}, "echo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
}

// --- buildSummary Tests ---

func TestBuildSummary_MixedResults(t *testing.T) {
	results := []ExecResult{
		{NodeName: "n1", ExitCode: 0},
		{NodeName: "n2", ExitCode: 1, Error: nil},
		{NodeName: "n3", ExitCode: -1, Error: fmt.Errorf("disconnected")},
		{NodeName: "n4", ExitCode: 0},
	}

	summary := buildSummary(results, 100*time.Millisecond)

	if summary.Total != 4 {
		t.Fatalf("expected total 4, got %d", summary.Total)
	}
	if summary.Succeeded != 2 {
		t.Fatalf("expected 2 succeeded, got %d", summary.Succeeded)
	}
	if summary.Failed != 1 {
		t.Fatalf("expected 1 failed, got %d", summary.Failed)
	}
	if summary.Skipped != 1 {
		t.Fatalf("expected 1 skipped, got %d", summary.Skipped)
	}
	if len(summary.Failures) != 1 {
		t.Fatalf("expected 1 failure info, got %d", len(summary.Failures))
	}
	if summary.Failures[0].NodeName != "n2" {
		t.Fatalf("expected failure for n2, got %s", summary.Failures[0].NodeName)
	}
}
