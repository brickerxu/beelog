package ssh

import (
	"strings"
	"testing"
)

func TestNormalizeLineEndings(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"a\r\nb", "a\nb"},
		{"a\rb", "a\nb"},
		{"a\nb", "a\nb"},
		{"a\r\n\r\nb", "a\n\nb"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeLineEndings(tt.in); got != tt.want {
			t.Errorf("normalizeLineEndings(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// PTY 输出里如果命令回显行中间夹了裸 \r，之前会被终端渲染成"覆盖式"显示，
// 且我们的 marker 判定会错过——命令回显泄漏进结果。归一化后应完全过滤掉。
func TestParseExecuteOutput_EchoWithBareCR(t *testing.T) {
	cmd := "cat statistic.log|grep '/login'"
	marker := "__BEELOG_END_1790058827878432000__"
	// 回显行中间夹了 \r，把 marker 后半段"盖住"
	raw := "cat statistic.log|grep '/login' ; echo " + marker + " $?\r{\"data\":\"payload\"}\n" +
		"actual output line 1\n" +
		"actual output line 2\n" +
		marker + " 0\r\n"

	out, exit := parseExecuteOutput(raw, cmd, marker)
	if exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
	if strings.Contains(out, "echo "+marker) {
		t.Errorf("echo line leaked into output:\n%s", out)
	}
	if !strings.Contains(out, "actual output line 1") || !strings.Contains(out, "actual output line 2") {
		t.Errorf("real output lost:\n%s", out)
	}
	// \r 切开后的 JSON 片段本身也应该作为独立行保留（它其实是 grep 的匹配结果）
	if !strings.Contains(out, `{"data":"payload"}`) {
		t.Errorf("post-\\r content lost:\n%s", out)
	}
}

// 兜底：即使 marker 完整字符串被破坏（比如 __BEELOG_END_ 前缀断裂），
// "; echo __" 前缀还在，isCommandEcho 应仍能识别为回显。
func TestIsCommandEcho_BrokenMarker(t *testing.T) {
	cmd := "cat statistic.log|grep '/login'"
	brokenCases := []string{
		// marker 中段被 \r 覆盖
		"cat statistic.log|grep '/login' ; echo __BEELO0058827878432000__ $?",
		// marker 被截到只剩 "__"
		"cat statistic.log|grep '/login' ; echo __",
		// marker 被截到 "__BEELOG"
		"cat statistic.log|grep '/login' ; echo __BEELOG",
	}
	for _, s := range brokenCases {
		if !isCommandEcho(s, cmd) {
			t.Errorf("broken-marker echo not recognized: %q", s)
		}
	}

	// 完整 marker 也应命中（回归）
	fullEcho := "cat statistic.log|grep '/login' ; echo __BEELOG_END_123__ $?"
	if !isCommandEcho(fullEcho, cmd) {
		t.Errorf("full-marker echo not recognized: %q", fullEcho)
	}

	// 普通输出行不应误判
	if isCommandEcho(`{"data":"ok"}`, cmd) {
		t.Error("plain output misclassified as echo")
	}
	// 只有命令没有 echo 前缀也不算回显（避免用户输出里恰巧含命令文本被过滤）
	if isCommandEcho("cat statistic.log|grep '/login' matched something", cmd) {
		t.Error("command-only line misclassified as echo")
	}
}

// marker 碎片：\r 把 "__BEELOG_END_179..." 切成孤立片段（无命令上下文、
// 前后下划线丢失），containsMarkerFragment 依关键字兜底过滤。
func TestContainsMarkerFragment(t *testing.T) {
	positives := []string{
		"__BEELOG_END_1790058827878432000__",
		"BEELOG_END_179005...", // 前置 __ 被 \r 切走
		"BEELOG_DRAIN_179_1__", // drain 片段
		"prefix BEELOG_END_ suffix",
	}
	for _, s := range positives {
		if !containsMarkerFragment(s) {
			t.Errorf("expected marker fragment: %q", s)
		}
	}
	negatives := []string{
		"",
		`{"data":"ok"}`,
		"BEELOG_something_else",
		"__BEELOG__",
	}
	for _, s := range negatives {
		if containsMarkerFragment(s) {
			t.Errorf("false positive on: %q", s)
		}
	}
}
