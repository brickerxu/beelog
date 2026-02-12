package output

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/brickerxu/beelog/internal/executor"
)

// outputAggregator 实现 OutputAggregator 接口
type outputAggregator struct{}

// NewOutputAggregator 创建 OutputAggregator 实例
func NewOutputAggregator() OutputAggregator {
	return &outputAggregator{}
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
		fmt.Fprintf(&sb, "%s==================== [%s] ====================%s\n", colorOn, r.NodeName, colorOff)
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
		fmt.Fprintf(&sb, "[%s] [%s] %s\n",
			line.Timestamp.Format(TimestampFormat),
			line.NodeName,
			line.Content,
		)
	}
	return sb.String()
}

// RenderStream 实时流式展示，写入单行到 writer，带节点名称前缀
func (o *outputAggregator) RenderStream(line executor.OutputLine, writer io.Writer) error {
	_, err := fmt.Fprintf(writer, "[%s] %s\n", line.NodeName, line.Content)
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
