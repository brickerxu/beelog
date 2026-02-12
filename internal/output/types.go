package output

import (
	"io"

	"github.com/brickerxu/beelog/internal/executor"
)

// OutputMode 输出模式枚举
type OutputMode string

const (
	ModeGrouped OutputMode = "grouped"
	ModeMerged  OutputMode = "merged"
	ModeStream  OutputMode = "stream"
)

// TimestampFormat 固定时间戳格式，用于 merged 模式解析
const TimestampFormat = "2006-01-02 15:04:05"

// OutputAggregator 输出聚合接口
type OutputAggregator interface {
	// RenderGrouped 按节点分组展示
	RenderGrouped(results []executor.ExecResult) string
	// RenderMerged 按时间戳合并展示（固定格式: 2006-01-02 15:04:05）
	RenderMerged(lines []executor.OutputLine) string
	// RenderStream 实时流式展示（写入 writer）
	RenderStream(line executor.OutputLine, writer io.Writer) error
	// RenderSummary 展示执行汇总
	RenderSummary(summary executor.ExecutionSummary) string
}
