package output

import (
	"strings"
	"testing"

	"github.com/brickerxu/beelog/internal/executor"
)

// ── computeLineDiff ───────────────────────────────────────────────────────────

func TestComputeLineDiff_Identical(t *testing.T) {
	a := []string{"line1", "line2", "line3"}
	diff := computeLineDiff(a, a)
	for _, d := range diff {
		if d.typ != diffSame {
			t.Errorf("expected all same, got %v %q", d.typ, d.content)
		}
	}
}

func TestComputeLineDiff_AllAdded(t *testing.T) {
	diff := computeLineDiff(nil, []string{"a", "b"})
	for _, d := range diff {
		if d.typ != diffAdded {
			t.Errorf("expected all added, got %v %q", d.typ, d.content)
		}
	}
}

func TestComputeLineDiff_AllRemoved(t *testing.T) {
	diff := computeLineDiff([]string{"a", "b"}, nil)
	for _, d := range diff {
		if d.typ != diffRemoved {
			t.Errorf("expected all removed, got %v %q", d.typ, d.content)
		}
	}
}

func TestComputeLineDiff_OneLine(t *testing.T) {
	a := []string{"version: 1.2.3"}
	b := []string{"version: 1.2.4"}
	diff := computeLineDiff(a, b)
	if len(diff) != 2 {
		t.Fatalf("expected 2 diff lines, got %d", len(diff))
	}
	if diff[0].typ != diffRemoved || diff[0].content != "version: 1.2.3" {
		t.Errorf("expected removed line, got %+v", diff[0])
	}
	if diff[1].typ != diffAdded || diff[1].content != "version: 1.2.4" {
		t.Errorf("expected added line, got %+v", diff[1])
	}
}

func TestComputeLineDiff_MiddleChange(t *testing.T) {
	a := []string{"header", "old", "footer"}
	b := []string{"header", "new", "footer"}
	diff := computeLineDiff(a, b)
	types := make([]diffLineType, len(diff))
	for i, d := range diff {
		types[i] = d.typ
	}
	// Expect: same, removed, added, same
	expected := []diffLineType{diffSame, diffRemoved, diffAdded, diffSame}
	if len(types) != len(expected) {
		t.Fatalf("expected %d diff lines, got %d: %v", len(expected), len(types), diff)
	}
	for i, want := range expected {
		if types[i] != want {
			t.Errorf("line %d: got type %v, want %v", i, types[i], want)
		}
	}
}

func TestComputeLineDiff_TooLarge(t *testing.T) {
	large := make([]string, 501)
	if computeLineDiff(large, large) != nil {
		t.Error("expected nil for large inputs")
	}
}

// ── RenderDiff ────────────────────────────────────────────────────────────────

func makeResult(nodeOutputs []struct{ node, output string }) *executor.BatchResult {
	var results []executor.ExecResult
	for _, no := range nodeOutputs {
		results = append(results, executor.ExecResult{NodeName: no.node, Output: no.output})
	}
	return &executor.BatchResult{Results: results}
}

func TestRenderDiff_Nil(t *testing.T) {
	out := RenderDiff(nil, false)
	if !strings.Contains(out, "没有可对比") {
		t.Errorf("expected 没有可对比 message, got %q", out)
	}
}

func TestRenderDiff_SingleNode(t *testing.T) {
	result := makeResult([]struct{ node, output string }{{"web-1", "hello"}})
	out := RenderDiff(result, false)
	if !strings.Contains(out, "只有一个节点") {
		t.Errorf("expected 只有一个节点 message, got %q", out)
	}
}

func TestRenderDiff_AllSame(t *testing.T) {
	result := makeResult([]struct{ node, output string }{
		{"web-1", "version: 1.0\n"},
		{"web-2", "version: 1.0\n"},
		{"web-3", "version: 1.0\n"},
	})
	out := RenderDiff(result, false)
	if !strings.Contains(out, "完全一致") {
		t.Errorf("expected 完全一致 message, got %q", out)
	}
	if strings.Contains(out, "差异") {
		t.Error("unexpected 差异 in all-same result")
	}
}

func TestRenderDiff_OneDiffers(t *testing.T) {
	result := makeResult([]struct{ node, output string }{
		{"web-1", "version: 1.0\n"},
		{"web-2", "version: 1.0\n"},
		{"web-3", "version: 1.1\n"},
	})
	out := RenderDiff(result, false)
	if !strings.Contains(out, "相同") {
		t.Errorf("expected 相同 section, got %q", out)
	}
	if !strings.Contains(out, "差异") {
		t.Errorf("expected 差异 section, got %q", out)
	}
	if !strings.Contains(out, "web-3") {
		t.Errorf("expected web-3 to appear in diff, got %q", out)
	}
	if !strings.Contains(out, "1.1") {
		t.Errorf("expected version 1.1 in diff output, got %q", out)
	}
}

func TestRenderDiff_DiffMarkers(t *testing.T) {
	// web-1 is reference (inserted first), web-2 differs
	result := makeResult([]struct{ node, output string }{
		{"web-1", "app: myapp\nversion: 1.0\n"},
		{"web-2", "app: myapp\nversion: 2.0\n"},
	})
	out := RenderDiff(result, false)
	// One version should be marked as removed and the other as added
	hasRemoval := strings.Contains(out, "- version: 1.0") || strings.Contains(out, "- version: 2.0")
	hasAddition := strings.Contains(out, "+ version: 1.0") || strings.Contains(out, "+ version: 2.0")
	if !hasRemoval {
		t.Errorf("expected a removal marker line, got %q", out)
	}
	if !hasAddition {
		t.Errorf("expected an addition marker line, got %q", out)
	}
	// Context line (same line) should appear with two leading spaces
	if !strings.Contains(out, "  app: myapp") {
		t.Errorf("expected context line '  app: myapp', got %q", out)
	}
}

func TestRenderDiff_ReferenceIsLargestGroup(t *testing.T) {
	result := makeResult([]struct{ node, output string }{
		{"web-1", "v1\n"},
		{"web-2", "v1\n"},
		{"web-3", "v1\n"},
		{"web-4", "v2\n"},
	})
	out := RenderDiff(result, false)
	if !strings.Contains(out, "相同 (3/4)") {
		t.Errorf("expected 相同 (3/4), got %q", out)
	}
	if !strings.Contains(out, "web-4") {
		t.Errorf("expected web-4 in diff section, got %q", out)
	}
}
