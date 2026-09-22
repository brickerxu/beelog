package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/brickerxu/beelog/internal/config"
)

func TestPrintGroups_SortedAndFormatted(t *testing.T) {
	cfg := &config.Config{
		Groups: map[string][]string{
			"web":    {"web-1", "web-2", "web-3"},
			"api":    {"api-1", "api-2"},
			"worker": {"worker-1"},
		},
	}
	var buf bytes.Buffer
	printGroups(&buf, cfg)
	out := buf.String()

	// 表头包含总数
	if !strings.Contains(out, "分组列表 (3):") {
		t.Errorf("missing header line, got:\n%s", out)
	}

	// 分组按字典序排列，api 应在 web 之前，web 应在 worker 之前
	apiIdx := strings.Index(out, "api")
	webIdx := strings.Index(out, "web")
	workerIdx := strings.Index(out, "worker")
	if apiIdx < 0 || webIdx < 0 || workerIdx < 0 {
		t.Fatalf("expected all groups in output:\n%s", out)
	}
	if !(apiIdx < webIdx && webIdx < workerIdx) {
		t.Errorf("expected sorted order api < web < worker, indices: api=%d web=%d worker=%d",
			apiIdx, webIdx, workerIdx)
	}

	// 节点数与成员都在行内
	if !strings.Contains(out, "(2 nodes)  api-1, api-2") {
		t.Errorf("expected api line with node count and members, got:\n%s", out)
	}
	if !strings.Contains(out, "(3 nodes)  web-1, web-2, web-3") {
		t.Errorf("expected web line with node count and members, got:\n%s", out)
	}
}

func TestPrintGroups_Empty(t *testing.T) {
	cfg := &config.Config{Groups: map[string][]string{}}
	var buf bytes.Buffer
	printGroups(&buf, cfg)
	out := buf.String()
	if !strings.Contains(out, "分组列表 (0):") {
		t.Errorf("expected 分组列表 (0), got:\n%s", out)
	}
}
