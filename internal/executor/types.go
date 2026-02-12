package executor

import (
	"context"
	"io"
	"time"
)

// ExecResult 表示单个节点的命令执行结果
type ExecResult struct {
	NodeName string
	Output   string
	ExitCode int
	Error    error
	Duration time.Duration
}

// OutputLine 单行输出
type OutputLine struct {
	NodeName  string
	Content   string
	Timestamp time.Time
	IsError   bool
}

// BatchResult 批量执行结果
type BatchResult struct {
	Results []ExecResult
	Summary ExecutionSummary
}

// ExecutionSummary 执行汇总
type ExecutionSummary struct {
	Total     int
	Succeeded int
	Failed    int
	Skipped   int
	Duration  time.Duration
	Failures  []FailureInfo
}

// FailureInfo 失败信息
type FailureInfo struct {
	NodeName string
	Reason   string
	Retries  int
}

// Session 表示到目标节点的会话（避免与 ssh 包循环依赖）
// 由 ssh.NodeSession 实现
type Session interface {
	// GetNodeName 返回节点名称
	GetNodeName() string
	// GetStdin 返回向目标节点发送输入的 writer
	GetStdin() io.WriteCloser
	// GetStdout 返回从目标节点读取输出的 reader
	GetStdout() io.Reader
	// GetStderr 返回从目标节点读取错误输出的 reader
	GetStderr() io.Reader
}

// Executor 并发执行引擎接口
type Executor interface {
	// ExecOnAll 在所有活跃节点上执行命令并收集结果
	ExecOnAll(ctx context.Context, sessions []Session, command string) (*BatchResult, error)
	// StreamOnAll 在所有活跃节点上执行流式命令（如 tail -f），通过 Ctrl+C 终止当前命令
	StreamOnAll(ctx context.Context, sessions []Session, command string, output chan<- OutputLine) error
}
