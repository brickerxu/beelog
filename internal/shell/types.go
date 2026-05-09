package shell

import (
	"context"

	"github.com/brickerxu/beelog/internal/ssh"
)

// InteractiveShell 交互式 shell 接口
type InteractiveShell interface {
	// Run 启动 REPL 循环，阻塞直到用户退出
	Run(ctx context.Context) error
}

// SessionManager 管理活跃的节点连接
type SessionManager interface {
	// GetActiveSessions 获取所有活跃的节点会话
	GetActiveSessions() []*ssh.NodeSession
	// GetAllSessions 获取所有节点会话（含已断开的）
	GetAllSessions() []*ssh.NodeSession
	// DisconnectNode 断开指定节点
	DisconnectNode(nodeName string) error
	// DisconnectAll 断开所有节点
	DisconnectAll() error
}
