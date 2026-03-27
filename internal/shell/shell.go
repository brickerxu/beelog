package shell

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/brickerxu/beelog/internal/config"
	"github.com/brickerxu/beelog/internal/executor"
	"github.com/brickerxu/beelog/internal/output"
	"github.com/brickerxu/beelog/internal/saver"
	"github.com/ergochat/readline"
)

// DefaultHistoryDir 默认命令历史文件目录
const DefaultHistoryDir = "~/.config/beelog/history"

// MaxHistoryLines 历史记录最大行数
const MaxHistoryLines = 1000

// interactiveShell 实现 InteractiveShell 接口
type interactiveShell struct {
	group      string
	exec       executor.Executor
	sessMgr    SessionManager
	outputAgg  output.OutputAggregator
	mode       output.OutputMode
	completer  *remoteCompleter
	execFn     executor.ExecFunc
	cwd        string                // 远程节点当前工作目录
	lastResult *executor.BatchResult // 上一条命令的执行结果
	saveCfg    saver.Config          // 保存配置
}

// NewInteractiveShell 创建交互式 shell 实例
func NewInteractiveShell(
	group string,
	exec executor.Executor,
	sessMgr SessionManager,
	outputAgg output.OutputAggregator,
	mode output.OutputMode,
	execFn executor.ExecFunc,
	saveCfg saver.Config,
) InteractiveShell {
	return &interactiveShell{
		group:     group,
		exec:      exec,
		sessMgr:   sessMgr,
		outputAgg: outputAgg,
		mode:      mode,
		completer: NewRemoteCompleter(sessMgr, execFn),
		execFn:    execFn,
		saveCfg:   saveCfg,
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
	saveCfg saver.Config,
) InteractiveShell {
	return &interactiveShell{
		group:     group,
		exec:      exec,
		sessMgr:   sessMgr,
		outputAgg: outputAgg,
		mode:      mode,
		completer: NewRemoteCompleterWithDebug(sessMgr, execFn, debug),
		execFn:    execFn,
		saveCfg:   saveCfg,
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
	// 忽略 SIGTSTP（Ctrl+Z），让 readline 将其作为 undo 处理
	setupSignals()
	defer resetSignals()

	// 显示欢迎信息和快捷键提示
	s.printWelcomeMessage()

	historyFile, err := ensureHistoryFile(s.group)
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
		Undo:              true,
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

		// 每次读取前刷新 prompt（反映最新的活跃节点数）
		rl.SetPrompt(s.prompt())

		// 所有节点断开时自动退出
		if len(s.sessMgr.GetActiveSessions()) == 0 {
			fmt.Fprintln(os.Stderr, "所有节点已断开，退出 beelog")
			return nil
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
			// :save 需要访问 lastResult，在 shell 层直接处理
			if cmd == "save" {
				msg := s.handleSave(args)
				fmt.Fprintln(os.Stderr, msg)
				rl.SetPrompt(s.prompt())
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

	// 保存本次结果，供 :save 使用
	s.lastResult = result

	// 提取 grep 搜索信息，用于高亮显示
	grepInfo := output.ExtractGrepInfo(command)
	colorEnabled := output.IsColorSupported()

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
				content := output.HighlightPatterns(line, grepInfo, colorEnabled)
				lines = append(lines, executor.OutputLine{
					NodeName:  r.NodeName,
					Content:   content,
					Timestamp: resolveTimestamp(line),
					IsError:   r.ExitCode != 0,
				})
			}
		}
		rendered := s.outputAgg.RenderMerged(lines)
		if rendered != "" {
			fmt.Print(rendered)
		}
	} else {
		// grouped 模式：对每个节点的输出内容应用高亮（不影响 lastResult 原始数据）
		highlightedResults := highlightExecResults(result.Results, grepInfo, colorEnabled)
		rendered := s.outputAgg.RenderGrouped(highlightedResults)
		if rendered != "" {
			fmt.Print(rendered)
		}
	}
}

// highlightExecResults 对 ExecResult 切片中的 Output 应用 grep 高亮。
// 返回新切片，不修改原始数据。
func highlightExecResults(results []executor.ExecResult, info output.GrepInfo, colorEnabled bool) []executor.ExecResult {
	if !colorEnabled || len(info.Patterns) == 0 {
		return results
	}
	highlighted := make([]executor.ExecResult, len(results))
	copy(highlighted, results)
	for i := range highlighted {
		highlighted[i].Output = output.HighlightPatterns(highlighted[i].Output, info, colorEnabled)
	}
	return highlighted
}

// dispatchStream 使用 StreamOnAll 执行流式命令（stream 模式）
func (s *interactiveShell) dispatchStream(ctx context.Context, sessions []executor.Session, command string) {
	outputCh := make(chan executor.OutputLine, 100)

	// 提取 grep 搜索信息，用于高亮显示
	grepInfo := output.ExtractGrepInfo(command)
	colorEnabled := output.IsColorSupported()

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.exec.StreamOnAll(ctx, sessions, command, outputCh)
		close(outputCh)
	}()

	for line := range outputCh {
		if len(grepInfo.Patterns) > 0 && colorEnabled {
			highlighted := line
			highlighted.Content = output.HighlightPatterns(line.Content, grepInfo, colorEnabled)
			s.outputAgg.RenderStream(highlighted, os.Stdout)
		} else {
			s.outputAgg.RenderStream(line, os.Stdout)
		}
	}

	if err := <-errCh; err != nil {
		fmt.Fprintf(os.Stderr, "流式执行错误: %v\n", err)
	}
}

// ensureHistoryFile 展开历史文件路径并确保其父目录存在
// 每个节点分组使用独立的历史文件：~/.config/beelog/history/{group}
func ensureHistoryFile(group string) (string, error) {
	dir, err := config.ExpandPath(DefaultHistoryDir)
	if err != nil {
		return "", fmt.Errorf("expand history dir: %w", err)
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create history dir %s: %w", dir, err)
	}

	historyFile := filepath.Join(dir, group)

	// 截断过长的历史文件
	truncateHistory(historyFile, MaxHistoryLines)

	return historyFile, nil
}

// truncateHistory 将历史文件截断到最近 maxLines 行
func truncateHistory(path string, maxLines int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return // 文件不存在或读取失败，忽略
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) <= maxLines {
		return
	}

	// 保留最近的 maxLines 行
	kept := lines[len(lines)-maxLines:]
	os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0600)
}

// resolveTimestamp tries to parse a log timestamp from the line content.
// Falls back to time.Now() if parsing fails.
func resolveTimestamp(line string) time.Time {
	if ts, ok := output.ParseLogTimestamp(line); ok {
		return ts
	}
	return time.Now()
}

// printWelcomeMessage 显示欢迎信息和快捷键提示
func (s *interactiveShell) printWelcomeMessage() {
	const (
		bold   = "\033[1m"
		cyan   = "\033[36m"
		yellow = "\033[33m"
		reset  = "\033[0m"
	)

	fmt.Printf("\n%s%sbeelog 交互式 Shell%s\n", bold, cyan, reset)
	fmt.Printf("%s快捷键:%s\n", bold, reset)
	fmt.Println("  • 历史记录: ↑/↓")
	fmt.Println("  • 路径补全: Tab")
	fmt.Println("  • 终止命令: Ctrl+C")

	// 根据操作系统显示不同的撤销提示
	if runtime.GOOS == "darwin" {
		fmt.Printf("  • 撤销输入: Ctrl+Z 或 Ctrl+_ %s(终端限制，无法使用 Command+Z)%s\n", yellow, reset)
	} else {
		fmt.Println("  • 撤销输入: Ctrl+Z 或 Ctrl+_")
	}

	fmt.Println("\n输入 :help 查看所有命令")
	fmt.Println()
}

// handleSave 处理 :save 命令，返回要显示给用户的消息
// 用法: :save [filename|filepath] [--format text|structured|json|csv]
func (s *interactiveShell) handleSave(args []string) string {
	if s.lastResult == nil {
		return "没有可保存的内容，请先执行一条命令"
	}

	var pathArg, formatArg string
	for i := 0; i < len(args); i++ {
		if args[i] == "--format" && i+1 < len(args) {
			formatArg = args[i+1]
			i++
		} else if pathArg == "" {
			pathArg = args[i]
		}
	}

	filePath, err := saver.Save(s.lastResult, pathArg, formatArg, s.saveCfg)
	if err != nil {
		return fmt.Sprintf("保存失败: %v", err)
	}
	return fmt.Sprintf("已保存到: %s", filePath)
}
