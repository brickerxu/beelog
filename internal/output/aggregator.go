package output

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/brickerxu/beelog/internal/executor"
)

// FormatBriefDuration 返回紧凑的耗时显示，如 "156ms" / "1.5s" / "1m23s"。
func FormatBriefDuration(d time.Duration) string {
	if d < time.Second {
		return d.Truncate(time.Millisecond).String()
	}
	if d < time.Minute {
		return d.Truncate(100 * time.Millisecond).String()
	}
	return d.Truncate(time.Second).String()
}

// formatNodeDurationTag 返回附加到节点标题的耗时/退出码标注，
// 已断开或未执行的节点返回 "跳过"。
func formatNodeDurationTag(r executor.ExecResult) string {
	if r.Duration == 0 && r.ExitCode == -1 {
		return "跳过"
	}
	dur := FormatBriefDuration(r.Duration)
	if r.ExitCode > 0 {
		return fmt.Sprintf("%s, exit=%d", dur, r.ExitCode)
	}
	return dur
}

// FormatDurationSummary 生成一行灰色汇总：`⏱  总耗时 234ms · web-1 156ms · web-2 189ms`。
// 空结果返回空串。
func FormatDurationSummary(results []executor.ExecResult, total time.Duration, colorEnabled bool) string {
	if len(results) == 0 {
		return ""
	}
	const gray = "\033[90m"
	const reset = "\033[0m"

	var parts []string
	for _, r := range results {
		if r.Duration > 0 {
			parts = append(parts, fmt.Sprintf("%s %s", r.NodeName, FormatBriefDuration(r.Duration)))
		}
	}
	body := fmt.Sprintf("⏱  总耗时 %s", FormatBriefDuration(total))
	if len(parts) > 0 {
		body += " · " + strings.Join(parts, " · ")
	}
	if colorEnabled {
		return gray + body + reset + "\n"
	}
	return body + "\n"
}

// FormatStreamDuration 生成 stream 模式退出后的耗时行 `⏱  已运行 12s`。
func FormatStreamDuration(total time.Duration, colorEnabled bool) string {
	const gray = "\033[90m"
	const reset = "\033[0m"
	body := fmt.Sprintf("⏱  已运行 %s", FormatBriefDuration(total))
	if colorEnabled {
		return gray + body + reset + "\n"
	}
	return body + "\n"
}

// outputAggregator 实现 OutputAggregator 接口
type outputAggregator struct {
	colors *ColorPalette
}

// NewOutputAggregator 创建 OutputAggregator 实例
func NewOutputAggregator() OutputAggregator {
	return &outputAggregator{
		colors: NewColorPalette(IsColorSupported()),
	}
}

// RenderGrouped 按节点分组展示，每个节点输出为连续块，节点间有分隔符
// RenderGrouped 按节点分组展示，每个节点输出为连续块，节点间有醒目分隔符
func (o *outputAggregator) RenderGrouped(results []executor.ExecResult) string {
	if len(results) == 0 {
		return ""
	}

	// 亮青色 + 粗体
	const colorOn = "\033[1;36m"
	const colorOff = "\033[0m"

	var sb strings.Builder
	for i, r := range results {
		if i > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "%s==================== [%s] (%s) ====================%s\n", colorOn, r.NodeName, formatNodeDurationTag(r), colorOff)
		sb.WriteString(r.Output)
		sb.WriteString("\n")
	}
	return sb.String()
}

// RenderMerged 按时间戳升序合并所有节点输出
func (o *outputAggregator) RenderMerged(lines []executor.OutputLine) string {
	if len(lines) == 0 {
		return ""
	}

	// 复制切片避免修改原始数据
	sorted := make([]executor.OutputLine, len(lines))
	copy(sorted, lines)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})

	var sb strings.Builder
	for _, line := range sorted {
		fmt.Fprintf(&sb, "[%s] %s\n",
			o.colors.Colorize(line.NodeName, line.NodeName),
			line.Content,
		)
	}
	return sb.String()
}

// RenderStream 实时流式展示，写入单行到 writer，带节点名称前缀
func (o *outputAggregator) RenderStream(line executor.OutputLine, writer io.Writer) error {
	_, err := fmt.Fprintf(writer, "[%s] %s\n", o.colors.Colorize(line.NodeName, line.NodeName), line.Content)
	return err
}

// RenderSummary 展示执行汇总：总数、成功、失败、跳过、耗时，以及失败详情
func (o *outputAggregator) RenderSummary(summary executor.ExecutionSummary) string {
	var sb strings.Builder

	sb.WriteString("Execution Summary\n")
	sb.WriteString(fmt.Sprintf("  Total:     %d\n", summary.Total))
	sb.WriteString(fmt.Sprintf("  Succeeded: %d\n", summary.Succeeded))
	sb.WriteString(fmt.Sprintf("  Failed:    %d\n", summary.Failed))
	sb.WriteString(fmt.Sprintf("  Skipped:   %d\n", summary.Skipped))
	sb.WriteString(fmt.Sprintf("  Duration:  %s\n", summary.Duration))

	if len(summary.Failures) > 0 {
		sb.WriteString("\nFailures:\n")
		for _, f := range summary.Failures {
			sb.WriteString(fmt.Sprintf("  [%s] %s\n", f.NodeName, f.Reason))
		}
	}

	return sb.String()
}
