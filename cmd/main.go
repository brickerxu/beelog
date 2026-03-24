package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	// 注册 --group flag 的动态补全：从配置文件读取分组名
	_ = rootCmd.RegisterFlagCompletionFunc("group", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		configPath, _ := cmd.Flags().GetString("config")
		cfgMgr := config.NewConfigManager()
		cfg, err := cfgMgr.Load(configPath)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		groups := make([]string, 0, len(cfg.Groups))
		for name := range cfg.Groups {
			groups = append(groups, name)
		}
		return groups, cobra.ShellCompDirectiveNoFileComp
	})

	// 注册 install-completion 子命令：自动检测 shell 并安装补全脚本
	installCompletionCmd := &cobra.Command{
		Use:          "install-completion",
		Short:        "自动安装 shell 补全脚本（支持 bash/zsh）",
		Long:         "检测当前 shell 类型，自动将补全脚本安装到正确位置并更新 shell 配置文件。\n支持 zsh（含 oh-my-zsh）和 bash（含 Homebrew）。",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstallCompletion(rootCmd)
		},
	}
	rootCmd.AddCommand(installCompletionCmd)

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

// ─── install-completion 实现 ───────────────────────────────────────────────

// runInstallCompletion 检测当前 shell 并自动安装补全脚本
func runInstallCompletion(rootCmd *cobra.Command) error {
	shellPath := os.Getenv("SHELL")
	if shellPath == "" {
		return fmt.Errorf("无法检测当前 shell（$SHELL 未设置），请手动安装:\n  beelog completion bash/zsh")
	}
	shellName := filepath.Base(shellPath)
	fmt.Printf("检测到 shell: %s\n", shellName)

	switch shellName {
	case "zsh":
		return installZshCompletion(rootCmd)
	case "bash":
		return installBashCompletion(rootCmd)
	default:
		return fmt.Errorf("暂不支持 %s，请手动安装:\n  beelog completion %s", shellName, shellName)
	}
}

// installZshCompletion 安装 zsh 补全脚本，自动检测 oh-my-zsh
func installZshCompletion(rootCmd *cobra.Command) error {
	var buf bytes.Buffer
	if err := rootCmd.GenZshCompletion(&buf); err != nil {
		return fmt.Errorf("生成补全脚本失败: %w", err)
	}
	script := buf.Bytes()

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("无法获取用户主目录: %w", err)
	}

	var installPath string
	var needFpath bool
	var rcFile string

	// 检测 oh-my-zsh（$ZSH 已设置）
	if zshDir := os.Getenv("ZSH"); zshDir != "" {
		customDir := os.Getenv("ZSH_CUSTOM")
		if customDir == "" {
			customDir = filepath.Join(zshDir, "custom")
		}
		completionsDir := filepath.Join(customDir, "completions")
		if err := os.MkdirAll(completionsDir, 0755); err == nil {
			installPath = filepath.Join(completionsDir, "_beelog")
			fmt.Printf("检测到 oh-my-zsh，安装路径: %s\n", installPath)
		}
	}

	// 标准 zsh：~/.zsh/completions/
	if installPath == "" {
		completionsDir := filepath.Join(home, ".zsh", "completions")
		if err := os.MkdirAll(completionsDir, 0755); err != nil {
			return fmt.Errorf("创建目录失败: %w", err)
		}
		installPath = filepath.Join(completionsDir, "_beelog")
		rcFile = filepath.Join(home, ".zshrc")
		needFpath = true
	}

	if err := writeCompletionFile(installPath, script); err != nil {
		return err
	}

	// 非 oh-my-zsh：确保 ~/.zshrc 中包含 fpath
	if needFpath {
		fpathLine := fmt.Sprintf("fpath=(%s $fpath)", filepath.Dir(installPath))
		if err := addLineToFileIfAbsent(rcFile, fpathLine, "# beelog 补全目录"); err != nil {
			fmt.Fprintf(os.Stderr, "[WARN] 无法更新 %s，请手动添加:\n  %s\n", rcFile, fpathLine)
		} else {
			fmt.Printf("✓ 已更新 fpath: %s\n", rcFile)
		}
	}

	printReloadHint("zsh", home)
	return nil
}

// installBashCompletion 安装 bash 补全脚本，支持 Homebrew 和 Linux 用户目录
func installBashCompletion(rootCmd *cobra.Command) error {
	var buf bytes.Buffer
	if err := rootCmd.GenBashCompletionV2(&buf, true); err != nil {
		return fmt.Errorf("生成补全脚本失败: %w", err)
	}
	script := buf.Bytes()

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("无法获取用户主目录: %w", err)
	}

	var installPath string
	var rcFile string
	var needSource bool

	// macOS：尝试 Homebrew bash-completion 目录
	for _, dir := range []string{
		"/opt/homebrew/etc/bash_completion.d", // Apple Silicon
		"/usr/local/etc/bash_completion.d",    // Intel Mac
	} {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			installPath = filepath.Join(dir, "beelog")
			fmt.Printf("检测到 Homebrew bash-completion 目录: %s\n", dir)
			break
		}
	}

	// Linux：用户级 bash-completion 目录
	if installPath == "" {
		completionsDir := filepath.Join(home, ".local", "share", "bash-completion", "completions")
		if err := os.MkdirAll(completionsDir, 0755); err == nil {
			installPath = filepath.Join(completionsDir, "beelog")
		}
	}

	// 兜底：~/.bash_completions/ + source 写入 .bashrc
	if installPath == "" {
		completionsDir := filepath.Join(home, ".bash_completions")
		if err := os.MkdirAll(completionsDir, 0755); err != nil {
			return fmt.Errorf("创建目录失败: %w", err)
		}
		installPath = filepath.Join(completionsDir, "beelog")
		rcFile = filepath.Join(home, ".bashrc")
		needSource = true
	}

	if err := writeCompletionFile(installPath, script); err != nil {
		return err
	}

	if needSource {
		sourceLine := fmt.Sprintf("source %s", installPath)
		if err := addLineToFileIfAbsent(rcFile, sourceLine, "# beelog 补全"); err != nil {
			fmt.Fprintf(os.Stderr, "[WARN] 无法更新 %s，请手动添加:\n  %s\n", rcFile, sourceLine)
		} else {
			fmt.Printf("✓ 已更新 source: %s\n", rcFile)
		}
	}

	printReloadHint("bash", home)
	return nil
}

// writeCompletionFile 写入补全脚本文件，内容无变化时跳过
func writeCompletionFile(path string, content []byte) error {
	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, content) {
			fmt.Printf("✓ 补全脚本无变化: %s\n", path)
			return nil
		}
		fmt.Printf("✓ 更新补全脚本: %s\n", path)
	} else {
		fmt.Printf("✓ 写入补全脚本: %s\n", path)
	}
	return os.WriteFile(path, content, 0644)
}

// addLineToFileIfAbsent 向文件追加一行（幂等：已存在则跳过）
func addLineToFileIfAbsent(filePath, line, comment string) error {
	content, err := os.ReadFile(filePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(string(content), line) {
		return nil // 已存在，幂等跳过
	}
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := "\n"
	if len(content) > 0 && content[len(content)-1] == '\n' {
		prefix = ""
	}
	_, err = fmt.Fprintf(f, "%s\n%s\n%s\n", prefix, comment, line)
	return err
}

// printReloadHint 打印安装后的重载提示
func printReloadHint(shell, home string) {
	rcFile := filepath.Join(home, ".zshrc")
	if shell == "bash" {
		rcFile = filepath.Join(home, ".bashrc")
	}
	fmt.Printf("\n安装完成！执行以下命令使补全立即生效:\n")
	fmt.Printf("  source %s\n", rcFile)
	fmt.Println("或重新打开终端。")
	if shell == "zsh" {
		fmt.Println("\n如补全未生效，尝试重建补全缓存:")
		fmt.Println("  rm -f ~/.zcompdump && compinit")
	}
}
