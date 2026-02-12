package ssh

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/brickerxu/beelog/internal/config"
	"github.com/brickerxu/beelog/internal/executor"
	gossh "golang.org/x/crypto/ssh"
)

// NodeSession 表示通过 JumpServer 到目标节点的会话
// 每个 NodeSession 对应一个独立的 JumpServer SSH 连接
type NodeSession struct {
	NodeName     string
	JumpClient   *gossh.Client  // 到 JumpServer 的 SSH 连接
	ShellSession *gossh.Session // JumpServer 上的 shell 会话（已导航到目标节点）
	Stdin        io.WriteCloser // 向目标节点发送输入
	Stdout       io.Reader      // 从目标节点读取输出
	Stderr       io.Reader      // 从目标节点读取错误输出

	// 连接状态跟踪（线程安全）
	mu            sync.Mutex
	Disconnected  bool  // 节点是否已断开
	DisconnectErr error // 断开原因
}

// GetNodeName 实现 executor.Session 接口
func (s *NodeSession) GetNodeName() string {
	return s.NodeName
}

// GetStdin 实现 executor.Session 接口
func (s *NodeSession) GetStdin() io.WriteCloser {
	return s.Stdin
}

// GetStdout 实现 executor.Session 接口
func (s *NodeSession) GetStdout() io.Reader {
	return s.Stdout
}

// GetStderr 实现 executor.Session 接口
func (s *NodeSession) GetStderr() io.Reader {
	return s.Stderr
}

// MarkDisconnected 标记节点为断开状态（线程安全）
func (s *NodeSession) MarkDisconnected(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Disconnected = true
	s.DisconnectErr = err
}

// IsDisconnected 检查节点是否已断开（线程安全）
func (s *NodeSession) IsDisconnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Disconnected
}

// GetDisconnectErr 获取断开原因（线程安全）
func (s *NodeSession) GetDisconnectErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.DisconnectErr
}

// SSHConnManager 管理通过 JumpServer 到目标节点的连接
type SSHConnManager interface {
	// Connect 建立到目标节点的连接
	// 流程: SSH连接JumpServer → keyboard-interactive提交TOTP → 解析资产菜单 → 选择目标节点
	Connect(ctx context.Context, jumpCfg config.JumpServerConfig, nodeName string) (*NodeSession, error)
	// Execute 在目标节点上执行一次性命令
	Execute(ctx context.Context, session *NodeSession, command string) (*executor.ExecResult, error)
	// Stream 在目标节点上执行流式命令
	Stream(ctx context.Context, session *NodeSession, command string, output chan<- executor.OutputLine) error
	// Close 关闭连接
	Close(session *NodeSession) error
	// StartKeepalive 启动心跳保活，定期发送无害命令防止 JumpServer 空闲断开
	StartKeepalive(ctx context.Context, session *NodeSession, interval time.Duration) error
	// StopKeepalive 停止心跳保活
	StopKeepalive(session *NodeSession) error
}

// JumpServerInteractor 处理 JumpServer 的交互式菜单
type JumpServerInteractor interface {
	// NavigateToNode 在 JumpServer shell 中导航到目标节点
	// 解析资产菜单输出，输入目标节点名称/编号，等待目标节点 shell 就绪
	NavigateToNode(session *gossh.Session, stdin io.WriteCloser, stdout io.Reader, nodeName string) error
}
