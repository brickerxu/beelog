package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/brickerxu/beelog/internal/config"
	"github.com/brickerxu/beelog/internal/executor"
	"github.com/brickerxu/beelog/internal/output"
	"github.com/brickerxu/beelog/internal/saver"
	"github.com/brickerxu/beelog/internal/shell"
	"github.com/brickerxu/beelog/internal/ssh"
	"github.com/brickerxu/beelog/internal/totp"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "beelog",
		Short: "JumpServer 多节点批量命令执行工具",
		Long:  "通过 JumpServer 跳板机同时连接多个目标节点，提供交互式 shell 界面，批量执行命令并聚合展示输出。",
		RunE:  run,
	}

	rootCmd.Flags().StringP("config", "c", "~/.config/beelog/config.yaml", "配置文件路径")
	rootCmd.Flags().StringP("mode", "m", "", "输出模式: grouped | merged | stream")
	rootCmd.Flags().StringP("group", "g", "", "节点分组名称")
	rootCmd.Flags().BoolP("verbose", "v", false, "详细输出")
	rootCmd.Flags().Bool("debug", false, "调试模式")
	rootCmd.Flags().Int("timeout", 0, "命令超时时间(秒), 0 表示使用配置文件值")
	rootCmd.Flags().Int("concurrency", 0, "最大并发数, 0 表示使用配置文件值")

	_ = rootCmd.MarkFlagRequired("group")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	// 1. 解析 CLI 参数
	configPath, _ := cmd.Flags().GetString("config")
	mode, _ := cmd.Flags().GetString("mode")
	group, _ := cmd.Flags().GetString("group")
	verbose, _ := cmd.Flags().GetBool("verbose")
	debug, _ := cmd.Flags().GetBool("debug")
	timeout, _ := cmd.Flags().GetInt("timeout")
	concurrency, _ := cmd.Flags().GetInt("concurrency")

	// 2. 加载配置
	cfgMgr := config.NewConfigManager()
	cfg, err := cfgMgr.Load(configPath)
	if err != nil {
		return fmt.Errorf("加载配置文件失败: %w", err)
	}
	if err := cfgMgr.Validate(cfg); err != nil {
		return fmt.Errorf("配置文件验证失败: %w", err)
	}

	// 3. CLI 参数覆盖配置默认值
	if mode != "" {
		cfg.Defaults.OutputMode = mode
	}
	if timeout > 0 {
		cfg.Defaults.Timeout = timeout
	}
	if concurrency > 0 {
		cfg.Defaults.Concurrency = concurrency
	}

	// 4. 获取目标节点列表
	nodeNames, err := cfgMgr.GetNodesByGroup(cfg, group)
	if err != nil {
		return fmt.Errorf("获取节点分组失败: %w", err)
	}
	if len(nodeNames) == 0 {
		return fmt.Errorf("分组 %q 中没有节点", group)
	}

	// 检查 SSH 私钥文件权限
	if warn := ssh.CheckKeyPermissions(cfg.JumpServer.PrivateKey); warn != nil {
		fmt.Fprintln(os.Stderr, warn)
	}

	if verbose {
		sanitized := config.SanitizeConfig(cfg)
		fmt.Printf("配置已加载: JumpServer=%s\n", sanitized.JumpServer)
		fmt.Printf("分组 %q 包含 %d 个节点\n", group, len(nodeNames))
	}

	// 5. 创建核心组件
	totpGen := totp.NewTOTPGenerator()
	interactor := ssh.NewJumpServerInteractorWithDebug(time.Duration(cfg.Defaults.Timeout)*time.Second, debug)
	var sshMgr ssh.SSHConnManager
	if debug {
		sshMgr = ssh.NewSSHConnManagerWithDebug(totpGen, interactor, true)
	} else {
		sshMgr = ssh.NewSSHConnManager(totpGen, interactor)
	}

	// 6. 并发连接所有节点
	ctx := context.Background()
	sessions, failedNodes := connectAllNodes(ctx, sshMgr, cfg, nodeNames, verbose)

	// 7. 显示连接汇总
	total := len(nodeNames)
	connected := len(sessions)
	fmt.Printf("已连接 %d/%d 个节点\n", connected, total)
	for _, f := range failedNodes {
		fmt.Fprintf(os.Stderr, "[ERROR] [%s] %s\n", f.NodeName, f.Reason)
	}

	if connected == 0 {
		return fmt.Errorf("没有成功连接的节点，退出")
	}

	// 8. 创建 SessionManager
	sessMgr := newSessionManager(sessions, sshMgr)

	// 9. 创建 Executor（包装 SSHConnManager 的 Execute/Stream 方法）
	execFn := func(ctx context.Context, s executor.Session, command string) (*executor.ExecResult, error) {
		ns, ok := s.(*ssh.NodeSession)
		if !ok {
			return &executor.ExecResult{
				NodeName: s.GetNodeName(),
				Error:    fmt.Errorf("invalid session type"),
				ExitCode: -1,
			}, fmt.Errorf("invalid session type")
		}
		return sshMgr.Execute(ctx, ns, command)
	}
	streamFn := func(ctx context.Context, s executor.Session, command string, out chan<- executor.OutputLine) error {
		ns, ok := s.(*ssh.NodeSession)
		if !ok {
			return fmt.Errorf("invalid session type")
		}
		return sshMgr.Stream(ctx, ns, command, out)
	}
	exec := executor.NewExecutor(execFn, streamFn, cfg.Defaults.Concurrency)

	// 10. 创建 OutputAggregator
	outputAgg := output.NewOutputAggregator()

	// 11. 启动心跳保活
	keepaliveInterval := time.Duration(cfg.Defaults.KeepaliveInterval) * time.Second
	for _, s := range sessions {
		if err := sshMgr.StartKeepalive(ctx, s, keepaliveInterval); err != nil {
			fmt.Fprintf(os.Stderr, "[WARN] [%s] 启动心跳保活失败: %v\n", s.NodeName, err)
		}
	}

	// 12. 如果配置了默认工作目录，连接后自动 cd
	if workdir, ok := cfg.WorkDirs[group]; ok && workdir != "" {
		fmt.Printf("切换到默认工作目录: %s\n", workdir)
		for _, s := range sessions {
			cdCtx, cdCancel := context.WithTimeout(ctx, 5*time.Second)
			_, err := sshMgr.Execute(cdCtx, s, "cd "+workdir)
			cdCancel()
			if err != nil {
				fmt.Fprintf(os.Stderr, "[WARN] [%s] 切换工作目录失败: %v\n", s.NodeName, err)
			}
		}
	}

	// 13. 创建并运行 InteractiveShell
	outputMode := output.OutputMode(cfg.Defaults.OutputMode)
	saveCfg := saver.Config{
		DefaultDir:    cfg.Defaults.SaveDir,
		DefaultFormat: saver.Format(cfg.Defaults.SaveFormat),
	}
	var sh shell.InteractiveShell
	if debug {
		sh = shell.NewInteractiveShellWithDebug(group, exec, sessMgr, outputAgg, outputMode, execFn, true, saveCfg)
	} else {
		sh = shell.NewInteractiveShell(group, exec, sessMgr, outputAgg, outputMode, execFn, saveCfg)
	}

	err = sh.Run(ctx)

	// 13. 退出时关闭所有连接
	sessMgr.DisconnectAll()

	return err
}

// connectAllNodes 并发连接所有节点，显示连接进度
func connectAllNodes(
	ctx context.Context,
	sshMgr ssh.SSHConnManager,
	cfg *config.Config,
	nodeNames []string,
	verbose bool,
) ([]*ssh.NodeSession, []executor.FailureInfo) {
	type connResult struct {
		session *ssh.NodeSession
		failure *executor.FailureInfo
	}

	sem := make(chan struct{}, cfg.Defaults.Concurrency)
	resultCh := make(chan connResult, len(nodeNames))
	var wg sync.WaitGroup

	retryDelay := time.Duration(cfg.Defaults.RetryDelay) * time.Second

	for _, name := range nodeNames {
		wg.Add(1)
		go func(nodeName string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			fmt.Printf("正在连接 [%s]...\n", nodeName)

			session, err := ssh.ConnectWithRetry(
				ctx, sshMgr,
				cfg.JumpServer, nodeName,
				cfg.Defaults.MaxRetries, retryDelay,
			)
			if err != nil {
				resultCh <- connResult{
					failure: &executor.FailureInfo{
						NodeName: nodeName,
						Reason:   err.Error(),
						Retries:  cfg.Defaults.MaxRetries,
					},
				}
				return
			}

			if verbose {
				fmt.Printf("[%s] 连接成功\n", nodeName)
			}
			resultCh <- connResult{session: session}
		}(name)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	var sessions []*ssh.NodeSession
	var failures []executor.FailureInfo
	for r := range resultCh {
		if r.session != nil {
			sessions = append(sessions, r.session)
		}
		if r.failure != nil {
			failures = append(failures, *r.failure)
		}
	}

	return sessions, failures
}

// sessionManager 实现 shell.SessionManager 接口
// 管理已连接的节点会话，支持断开单个或全部节点
type sessionManager struct {
	mu       sync.Mutex
	sessions []*ssh.NodeSession
	sshMgr   ssh.SSHConnManager
}

func newSessionManager(sessions []*ssh.NodeSession, sshMgr ssh.SSHConnManager) *sessionManager {
	return &sessionManager{
		sessions: sessions,
		sshMgr:   sshMgr,
	}
}

// GetActiveSessions 返回所有未断开的活跃会话
func (m *sessionManager) GetActiveSessions() []*ssh.NodeSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	var active []*ssh.NodeSession
	for _, s := range m.sessions {
		if !s.IsDisconnected() {
			active = append(active, s)
		}
	}
	return active
}

// DisconnectNode 断开指定节点的连接
func (m *sessionManager) DisconnectNode(nodeName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, s := range m.sessions {
		if s.NodeName == nodeName {
			s.MarkDisconnected(fmt.Errorf("用户主动断开"))
			return m.sshMgr.Close(s)
		}
	}
	return fmt.Errorf("节点 %q 未找到", nodeName)
}

// DisconnectAll 断开所有节点连接
func (m *sessionManager) DisconnectAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []string
	for _, s := range m.sessions {
		if !s.IsDisconnected() {
			s.MarkDisconnected(fmt.Errorf("全部断开"))
			if err := m.sshMgr.Close(s); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("断开连接时出错: %v", errs)
	}
	return nil
}
