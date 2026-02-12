package ssh

import (
	"context"
	"fmt"
	"time"

	"github.com/brickerxu/beelog/internal/config"
)

// ConnectWithRetry 带重试逻辑的连接包装器
// 在连接失败时自动重试，重试次数和延迟从配置读取。
// maxRetries 表示重试次数（不含首次尝试），retryDelay 为重试间隔。
// 超过最大重试次数后返回包含重试计数信息的错误。
// 支持 context 取消，在重试等待期间可被中断。
func ConnectWithRetry(
	ctx context.Context,
	sshMgr SSHConnManager,
	jumpCfg config.JumpServerConfig,
	nodeName string,
	maxRetries int,
	retryDelay time.Duration,
) (*NodeSession, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// 非首次尝试时，等待 retryDelay
		if attempt > 0 {
			fmt.Printf("[%s] 连接失败，第 %d/%d 次重试（等待 %v）: %v\n",
				nodeName, attempt, maxRetries, retryDelay, lastErr)

			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("[%s] 连接重试被取消（已尝试 %d 次）: %w",
					nodeName, attempt, ctx.Err())
			case <-time.After(retryDelay):
				// 等待完成，继续重试
			}
		}

		session, err := sshMgr.Connect(ctx, jumpCfg, nodeName)
		if err == nil {
			if attempt > 0 {
				fmt.Printf("[%s] 第 %d 次重试连接成功\n", nodeName, attempt)
			}
			return session, nil
		}

		lastErr = err

		// 如果 context 已取消，不再重试
		if ctx.Err() != nil {
			return nil, fmt.Errorf("[%s] 连接失败且 context 已取消（已尝试 %d 次）: %w",
				nodeName, attempt+1, lastErr)
		}
	}

	return nil, fmt.Errorf("[%s] 连接失败，已重试 %d 次，跳过该节点: %w",
		nodeName, maxRetries, lastErr)
}
