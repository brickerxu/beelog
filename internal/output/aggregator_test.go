package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/brickerxu/beelog/internal/executor"
)

func TestNewOutputAggregator(t *testing.T) {
	agg := NewOutputAggregator()
	if agg == nil {
		t.Fatal("NewOutputAggregator returned nil")
	}
}

// --- RenderGrouped ---

func TestRenderGrouped_Empty(t *testing.T) {
	agg := NewOutputAggregator()
	result := agg.RenderGrouped(nil)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestRenderGrouped_SingleNode(t *testing.T) {
	agg := NewOutputAggregator()
	results := []executor.ExecResult{
		{NodeName: "web-1", Output: "hello world"},
	}
	got := agg.RenderGrouped(results)
	if !strings.Contains(got, "[web-1]") {
		t.Errorf("missing node header, got:\n%s", got)
	}
	if !strings.Contains(got, "====================") {
		t.Errorf("missing separator, got:\n%s", got)
	}
	if !strings.Contains(got, "hello world") {
		t.Errorf("missing output content, got:\n%s", got)
	}
}

func TestRenderGrouped_MultipleNodes_OrderPreserved(t *testing.T) {
	agg := NewOutputAggregator()
	results := []executor.ExecResult{
		{NodeName: "web-1", Output: "output-1"},
		{NodeName: "web-2", Output: "output-2"},
		{NodeName: "db-1", Output: "output-3"},
	}
	got := agg.RenderGrouped(results)

	idx1 := strings.Index(got, "[web-1]")
	idx2 := strings.Index(got, "[web-2]")
	idx3 := strings.Index(got, "[db-1]")

	if idx1 >= idx2 || idx2 >= idx3 {
		t.Errorf("node order not preserved, got:\n%s", got)
	}
}

// --- RenderMerged ---

func TestRenderMerged_Empty(t *testing.T) {
	agg := NewOutputAggregator()
	result := agg.RenderMerged(nil)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestRenderMerged_SortedByTimestamp(t *testing.T) {
	agg := NewOutputAggregator()
	t1 := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 15, 10, 29, 0, 0, time.UTC) // earlier
	t3 := time.Date(2024, 1, 15, 10, 31, 0, 0, time.UTC) // later

	lines := []executor.OutputLine{
		{NodeName: "web-1", Content: "line-a", Timestamp: t1},
		{NodeName: "web-2", Content: "line-b", Timestamp: t2},
		{NodeName: "db-1", Content: "line-c", Timestamp: t3},
	}
	got := agg.RenderMerged(lines)
	outputLines := strings.Split(strings.TrimSpace(got), "\n")

	if len(outputLines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(outputLines))
	}
	// Should be sorted: t2 (web-2), t1 (web-1), t3 (db-1)
	if !strings.Contains(outputLines[0], "[web-2]") {
		t.Errorf("first line should be web-2, got: %s", outputLines[0])
	}
	if !strings.Contains(outputLines[1], "[web-1]") {
		t.Errorf("second line should be web-1, got: %s", outputLines[1])
	}
	if !strings.Contains(outputLines[2], "[db-1]") {
		t.Errorf("third line should be db-1, got: %s", outputLines[2])
	}
}

func TestRenderMerged_TimestampFormat(t *testing.T) {
	agg := NewOutputAggregator()
	ts := time.Date(2024, 3, 15, 14, 30, 45, 0, time.UTC)
	lines := []executor.OutputLine{
		{NodeName: "web-1", Content: "test", Timestamp: ts},
	}
	got := agg.RenderMerged(lines)
	// Output format: [node] content (no timestamp prefix, node may have ANSI color)
	if !strings.Contains(got, "web-1") {
		t.Errorf("expected node name in output, got %q", got)
	}
	if !strings.Contains(got, "test") {
		t.Errorf("expected content in output, got %q", got)
	}
	if strings.Contains(got, "2024-03-15") {
		t.Errorf("timestamp should not appear in output, got %q", got)
	}
}

// --- RenderStream ---

func TestRenderStream_WritesWithPrefix(t *testing.T) {
	agg := NewOutputAggregator()
	var buf bytes.Buffer
	line := executor.OutputLine{
		NodeName: "web-1",
		Content:  "hello stream",
	}
	err := agg.RenderStream(line, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "[web-1] hello stream\n"
	if buf.String() != expected {
		t.Errorf("expected %q, got %q", expected, buf.String())
	}
}

func TestRenderStream_ErrorWriter(t *testing.T) {
	agg := NewOutputAggregator()
	line := executor.OutputLine{NodeName: "web-1", Content: "test"}
	err := agg.RenderStream(line, &failWriter{})
	if err == nil {
		t.Error("expected error from failing writer")
	}
}

type failWriter struct{}

func (f *failWriter) Write(p []byte) (n int, err error) {
	return 0, bytes.ErrTooLarge
}

// --- RenderSummary ---

func TestRenderSummary_Basic(t *testing.T) {
	agg := NewOutputAggregator()
	summary := executor.ExecutionSummary{
		Total:     5,
		Succeeded: 3,
		Failed:    1,
		Skipped:   1,
		Duration:  2 * time.Second,
	}
	got := agg.RenderSummary(summary)

	checks := []string{"Total:     5", "Succeeded: 3", "Failed:    1", "Skipped:   1", "2s"}
	for _, c := range checks {
		if !strings.Contains(got, c) {
			t.Errorf("missing %q in summary:\n%s", c, got)
		}
	}
}

func TestRenderSummary_WithFailures(t *testing.T) {
	agg := NewOutputAggregator()
	summary := executor.ExecutionSummary{
		Total:     3,
		Succeeded: 1,
		Failed:    2,
		Skipped:   0,
		Duration:  5 * time.Second,
		Failures: []executor.FailureInfo{
			{NodeName: "web-1", Reason: "connection timeout"},
			{NodeName: "db-1", Reason: "auth failed"},
		},
	}
	got := agg.RenderSummary(summary)

	if !strings.Contains(got, "Failures:") {
		t.Error("missing Failures section")
	}
	if !strings.Contains(got, "[web-1] connection timeout") {
		t.Error("missing web-1 failure detail")
	}
	if !strings.Contains(got, "[db-1] auth failed") {
		t.Error("missing db-1 failure detail")
	}
}

func TestRenderSummary_NoFailures(t *testing.T) {
	agg := NewOutputAggregator()
	summary := executor.ExecutionSummary{
		Total:     2,
		Succeeded: 2,
		Duration:  1 * time.Second,
	}
	got := agg.RenderSummary(summary)

	if strings.Contains(got, "Failures:") {
		t.Error("should not contain Failures section when no failures")
	}
}
