package shell

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/brickerxu/beelog/internal/config"
	"github.com/brickerxu/beelog/internal/executor"
	"github.com/brickerxu/beelog/internal/output"
	"github.com/chzyer/readline"
)

// DefaultHistoryPath 默认命令历史文件路径
const DefaultHistoryPath = "~/.config/beelog/history"

// interactiveShell 实现 InteractiveShell 接口
type interactiveShell struct {
	group     string
	exec      executor.Executor
	sessMgr   SessionManager
	outputAgg output.OutputAggregator
	mode      output.OutputMode
	completer *remoteCompleter
	execFn    executor.ExecFunc
	cwd       string // 远程节点当前工作目录
}

// NewInteractiveShell 创建交互式 shell 实例
func NewInteractiveShell(
	group string,
	exec executor.Executor,
	sessMgr SessionManager,
	outputAgg output.OutputAggregator,
	mode output.OutputMode,
	execFn executor.ExecFunc,
) InteractiveShell {
	return &interactiveShell{
		group:     group,
		exec:      exec,
		sessMgr:   sessMgr,
		outputAgg: outputAgg,
		mode:      mode,
		completer: NewRemoteCompleter(sessMgr, execFn),
		execFn:    execFn,
	}
}

// NewInteractiveShellWithDebug 创建带调试模式的交互式 shell 实例
func NewInteractiveShellWithDebug(
	group string,
	exec executor.Executor,
	sessMgr SessionManager,
	outputAgg output.OutputAggregator,
	mode output.OutputMode,
	execFn executor.ExecFunc,
	debug bool,
) InteractiveShell {
	return &interactiveShell{
		group:     group,
		exec:      exec,
		sessMgr:   sessMgr,
		outputAgg: outputAgg,
		mode:      mode,
		completer: NewRemoteCompleterWithDebug(sessMgr, execFn, debug),
		execFn:    execFn,
	}
}

// prompt 返回当前提示符，包含分组名、活跃节点数和当前目录（带颜色）
func (s *interactiveShell) prompt() string {
	active := s.sessMgr.GetActiveSessions()
	const (
		green = "\033[1;32m"
		cyan  = "\033[1;36m"
		reset = "\033[0m"
	)
	if s.cwd != "" {
		return fmt.Sprintf("%sbeelog%s [%s:%d] %s%s%s> ", green, reset, s.group, len(active), cyan, s.cwd, reset)
	}
	return fmt.Sprintf("%sbeelog%s [%s:%d]> ", green, reset, s.group, len(active))
}

// refreshCwd 在远程节点上执行 pwd 获取当前工作目录
func (s *interactiveShell) refreshCwd() {
	session := s.completer.getFirstActiveSession()
	if session == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := s.execFn(ctx, session, "pwd")
	if err != nil || result == nil || result.Output == "" {
		return
	}
	s.cwd = strings.TrimSpace(result.Output)
}

// toExecutorSessions 将 []*ssh.NodeSession 转换为 []executor.Session
func (s *interactiveShell) toExecutorSessions() []executor.Session {
	nodeSessions := s.sessMgr.GetActiveSessions()
	sessions := make([]executor.Session, len(nodeSessions))
	for i, ns := range nodeSessions {
		sessions[i] = ns
	}
	return sessions
}

// isSessionCommand 检查输入是否为会话管理命令（以 : 开头）
func isSessionCommand(input string) bool {
	return strings.HasPrefix(input, ":")
}

// Run 启动 REPL 循环，阻塞直到用户退出或 context 取消
func (s *interactiveShell) Run(ctx context.Context) error {
	historyFile, err := ensureHistoryFile()
	if err != nil {
		fmt.Fprintf(os.Stderr, "警告: 无法初始化命令历史文件: %v\n", err)
		historyFile = ""
	}

	// 启动时获取远程当前目录
	s.refreshCwd()

	rl, err := readline.NewEx(&readline.Config{
		Prompt:            s.prompt(),
		InterruptPrompt:   "^C",
		EOFPrompt:         ":quit",
		HistoryFile:       historyFile,
		HistorySearchFold: true,
		AutoComplete:      s.completer,
	})
	if err != nil {
		return fmt.Errorf("初始化 readline 失败: %w", err)
	}
	defer rl.Close()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := rl.Readline()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			if err == readline.ErrInterrupt {
				continue
			}
			return fmt.Errorf("读取输入失败: %w", err)
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		// 会话管理命令
		if isSessionCommand(input) {
			cmd, args := ParseSessionCommand(input)
			if cmd == "" {
				continue
			}
			quit, msg := HandleSessionCommand(cmd, args, s.sessMgr)
			if msg != "" {
				fmt.Fprintln(os.Stderr, msg)
			}
			if quit {
				return nil
			}
			rl.SetPrompt(s.prompt())
			continue
		}

		// 分发命令到所有活跃节点
		s.dispatchCommand(ctx, input, rl)
	}
}

// dispatchCommand 将命令分发到所有活跃节点并展示结果
func (s *interactiveShell) dispatchCommand(ctx context.Context, command string, rl *readline.Instance) {
	sessions := s.toExecutorSessions()
	if len(sessions) == 0 {
		fmt.Fprintln(os.Stderr, "没有活跃的节点连接")
		return
	}

	// 用亮黄色打印正在执行的命令
	const yellow = "\033[1;33m"
	const reset = "\033[0m"
	fmt.Fprintf(os.Stdout, "%s$ %s%s\n", yellow, command, reset)

	cmdCtx, cmdCancel := context.WithCancel(ctx)
	defer cmdCancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT)
	defer signal.Stop(sigChan)

	go func() {
		select {
		case <-sigChan:
			cmdCancel()
		case <-cmdCtx.Done():
		}
	}()

	switch s.mode {
	case output.ModeStream:
		s.dispatchStream(cmdCtx, sessions, command)
	default:
		s.dispatchExec(cmdCtx, sessions, command)
	}

	// cd 命令后刷新当前目录并更新提示符
	trimmed := strings.TrimSpace(command)
	if trimmed == "cd" || strings.HasPrefix(trimmed, "cd ") {
		s.refreshCwd()
		rl.SetPrompt(s.prompt())
	}
}

// dispatchExec 使用 ExecOnAll 执行命令并展示结果（grouped/merged 模式）
func (s *interactiveShell) dispatchExec(ctx context.Context, sessions []executor.Session, command string) {
	result, err := s.exec.ExecOnAll(ctx, sessions, command)
	if err != nil {
		fmt.Fprintf(os.Stderr, "命令执行失败: %v\n", err)
		return
	}

	if s.mode == output.ModeMerged {
		var lines []executor.OutputLine
		for _, r := range result.Results {
			if r.Output == "" {
				continue
			}
			for _, line := range strings.Split(r.Output, "\n") {
				if strings.TrimSpace(line) == "" {
					continue
				}
				lines = append(lines, executor.OutputLine{
					NodeName:  r.NodeName,
					Content:   line,
					Timestamp: time.Now(),
					IsError:   r.ExitCode != 0,
				})
			}
		}
		rendered := s.outputAgg.RenderMerged(lines)
		if rendered != "" {
			fmt.Print(rendered)
		}
	} else {
		rendered := s.outputAgg.RenderGrouped(result.Results)
		if rendered != "" {
			fmt.Print(rendered)
		}
	}
}

// dispatchStream 使用 StreamOnAll 执行流式命令（stream 模式）
func (s *interactiveShell) dispatchStream(ctx context.Context, sessions []executor.Session, command string) {
	outputCh := make(chan executor.OutputLine, 100)

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.exec.StreamOnAll(ctx, sessions, command, outputCh)
		close(outputCh)
	}()

	for line := range outputCh {
		s.outputAgg.RenderStream(line, os.Stdout)
	}

	if err := <-errCh; err != nil {
		fmt.Fprintf(os.Stderr, "流式执行错误: %v\n", err)
	}
}

// ensureHistoryFile 展开历史文件路径并确保其父目录存在
func ensureHistoryFile() (string, error) {
	expanded, err := config.ExpandPath(DefaultHistoryPath)
	if err != nil {
		return "", fmt.Errorf("expand history path: %w", err)
	}

	dir := filepath.Dir(expanded)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create history dir %s: %w", dir, err)
	}

	return expanded, nil
}
