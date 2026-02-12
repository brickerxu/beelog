package executor

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Disconnectable 可检查断开状态的会话接口
// ssh.NodeSession 通过 IsDisconnected() 方法实现此接口
type Disconnectable interface {
	IsDisconnected() bool
}

// ExecFunc 执行命令的函数类型（避免与 ssh 包循环依赖）
type ExecFunc func(ctx context.Context, session Session, command string) (*ExecResult, error)

// StreamFunc 流式执行命令的函数类型
type StreamFunc func(ctx context.Context, session Session, command string, output chan<- OutputLine) error

// concurrentExecutor 并发执行引擎实现
type concurrentExecutor struct {
	execFn      ExecFunc
	streamFn    StreamFunc
	concurrency int
}

// NewExecutor 创建并发执行引擎
// execFn: 在单个节点上执行命令的函数
// streamFn: 在单个节点上执行流式命令的函数
// concurrency: 最大并发数 (2-20)
func NewExecutor(execFn ExecFunc, streamFn StreamFunc, concurrency int) Executor {
	if concurrency < 2 {
		concurrency = 2
	}
	if concurrency > 20 {
		concurrency = 20
	}
	return &concurrentExecutor{
		execFn:      execFn,
		streamFn:    streamFn,
		concurrency: concurrency,
	}
}

// ExecOnAll 在所有活跃节点上并发执行命令并收集结果
// 使用 goroutine + semaphore channel 控制并发度
// 已断开的节点会被跳过，单个节点失败不影响其他节点
func (e *concurrentExecutor) ExecOnAll(ctx context.Context, sessions []Session, command string) (*BatchResult, error) {
	start := time.Now()
	n := len(sessions)
	if n == 0 {
		return &BatchResult{
			Results: []ExecResult{},
			Summary: ExecutionSummary{Duration: time.Since(start)},
		}, nil
	}

	// 结果收集 channel
	resultCh := make(chan ExecResult, n)
	sem := make(chan struct{}, e.concurrency)
	var wg sync.WaitGroup

	for _, sess := range sessions {
		wg.Add(1)
		go func(s Session) {
			defer wg.Done()

			nodeName := s.GetNodeName()

			// 检查节点是否已断开
			if dc, ok := s.(Disconnectable); ok && dc.IsDisconnected() {
				resultCh <- ExecResult{
					NodeName: nodeName,
					Error:    fmt.Errorf("[%s] 节点已断开，跳过执行", nodeName),
					ExitCode: -1,
				}
				return
			}

			// 获取信号量
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				resultCh <- ExecResult{
					NodeName: nodeName,
					Error:    fmt.Errorf("[%s] 上下文已取消: %w", nodeName, ctx.Err()),
					ExitCode: -1,
				}
				return
			}

			// 执行命令
			result, err := e.execFn(ctx, s, command)
			if err != nil {
				if result != nil {
					resultCh <- *result
				} else {
					resultCh <- ExecResult{
						NodeName: nodeName,
						Error:    err,
						ExitCode: -1,
					}
				}
				return
			}
			resultCh <- *result
		}(sess)
	}

	// 等待所有 goroutine 完成后关闭 channel
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// 收集结果
	results := make([]ExecResult, 0, n)
	for r := range resultCh {
		results = append(results, r)
	}

	// 构建汇总
	summary := buildSummary(results, time.Since(start))

	return &BatchResult{
		Results: results,
		Summary: summary,
	}, nil
}

// StreamOnAll 在所有活跃节点上并发执行流式命令
// 各节点的输出行通过共享的 output channel 转发
// 支持 context 取消（Ctrl+C 终止当前命令）
func (e *concurrentExecutor) StreamOnAll(ctx context.Context, sessions []Session, command string, output chan<- OutputLine) error {
	n := len(sessions)
	if n == 0 {
		return nil
	}

	sem := make(chan struct{}, e.concurrency)
	var wg sync.WaitGroup
	errCh := make(chan error, n)

	for _, sess := range sessions {
		wg.Add(1)
		go func(s Session) {
			defer wg.Done()

			nodeName := s.GetNodeName()

			// 检查节点是否已断开
			if dc, ok := s.(Disconnectable); ok && dc.IsDisconnected() {
				output <- OutputLine{
					NodeName:  nodeName,
					Content:   fmt.Sprintf("[%s] 节点已断开，跳过流式执行", nodeName),
					Timestamp: time.Now(),
					IsError:   true,
				}
				return
			}

			// 获取信号量
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			// 执行流式命令
			if err := e.streamFn(ctx, s, command, output); err != nil {
				// context 取消不算错误（用户 Ctrl+C）
				if ctx.Err() != nil {
					return
				}
				errCh <- fmt.Errorf("[%s] 流式执行失败: %w", nodeName, err)
			}
		}(sess)
	}

	// 等待所有 goroutine 完成
	wg.Wait()
	close(errCh)

	// 收集错误
	var errs []string
	for err := range errCh {
		errs = append(errs, err.Error())
	}

	if len(errs) > 0 {
		return fmt.Errorf("部分节点流式执行失败: %s", joinErrors(errs))
	}
	return nil
}

// buildSummary 从执行结果构建汇总信息
func buildSummary(results []ExecResult, duration time.Duration) ExecutionSummary {
	summary := ExecutionSummary{
		Total:    len(results),
		Duration: duration,
	}

	for _, r := range results {
		if r.Error != nil {
			summary.Failed++
			summary.Failures = append(summary.Failures, FailureInfo{
				NodeName: r.NodeName,
				Reason:   r.Error.Error(),
			})
		} else if r.ExitCode != 0 {
			summary.Failed++
			summary.Failures = append(summary.Failures, FailureInfo{
				NodeName: r.NodeName,
				Reason:   fmt.Sprintf("exit code: %d", r.ExitCode),
			})
		} else {
			summary.Succeeded++
		}
	}

	// 跳过的节点（已断开）计入 Skipped
	// 通过检查 ExitCode == -1 且有 Error 来识别跳过的节点
	// 重新计算：断开的节点算 Skipped 而非 Failed
	skipped := 0
	failed := 0
	var failures []FailureInfo
	for _, r := range results {
		if r.ExitCode == -1 && r.Error != nil {
			skipped++
		} else if r.Error != nil || r.ExitCode != 0 {
			failed++
			reason := ""
			if r.Error != nil {
				reason = r.Error.Error()
			} else {
				reason = fmt.Sprintf("exit code: %d", r.ExitCode)
			}
			failures = append(failures, FailureInfo{
				NodeName: r.NodeName,
				Reason:   reason,
			})
		}
	}
	summary.Skipped = skipped
	summary.Failed = failed
	summary.Succeeded = summary.Total - summary.Failed - summary.Skipped
	summary.Failures = failures

	return summary
}

// joinErrors 将多个错误信息合并为一个字符串
func joinErrors(errs []string) string {
	result := ""
	for i, e := range errs {
		if i > 0 {
			result += "; "
		}
		result += e
	}
	return result
}
