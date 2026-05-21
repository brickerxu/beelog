package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	onlyNodes  map[string]bool       // nil = 全部节点；非 nil = :only 设置的子集
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
// 当 :only 过滤器生效时显示 group:filtered/total 格式。
func (s *interactiveShell) prompt() string {
	active := s.sessMgr.GetActiveSessions()
	const (
		green = "\033[1;32m"
		cyan  = "\033[1;36m"
		reset = "\033[0m"
	)
	var nodeInfo string
	if s.onlyNodes != nil {
		count := 0
		for _, ns := range active {
			if s.onlyNodes[ns.NodeName] {
				count++
			}
		}
		nodeInfo = fmt.Sprintf("%s:%d/%d", s.group, count, len(active))
	} else {
		nodeInfo = fmt.Sprintf("%s:%d", s.group, len(active))
	}
	if s.cwd != "" {
		return fmt.Sprintf("%sbeelog%s [%s] %s%s%s> ", green, reset, nodeInfo, cyan, s.cwd, reset)
	}
	return fmt.Sprintf("%sbeelog%s [%s]> ", green, reset, nodeInfo)
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

// getTargetSessions 返回本次命令的目标会话。
// overrideNodes 非空时只选这些节点（来自 @node1,node2 前缀）；
// 否则应用 onlyNodes 过滤器；否则返回全部活跃节点。
func (s *interactiveShell) getTargetSessions(overrideNodes []string) []executor.Session {
	nodeSessions := s.sessMgr.GetActiveSessions()
	filter := s.onlyNodes
	if len(overrideNodes) > 0 {
		filter = make(map[string]bool, len(overrideNodes))
		for _, n := range overrideNodes {
			filter[n] = true
		}
	}
	if filter == nil {
		sessions := make([]executor.Session, len(nodeSessions))
		for i, ns := range nodeSessions {
			sessions[i] = ns
		}
		return sessions
	}
	var sessions []executor.Session
	for _, ns := range nodeSessions {
		if filter[ns.NodeName] {
			sessions = append(sessions, ns)
		}
	}
	return sessions
}

// parseNodePrefix 解析 @node1,node2 前缀，返回节点列表和实际命令。
// 输入不含前缀时返回 ok=false。
func parseNodePrefix(input string) (nodes []string, command string, ok bool) {
	if !strings.HasPrefix(input, "@") {
		return nil, input, false
	}
	spaceIdx := strings.IndexByte(input, ' ')
	if spaceIdx < 0 {
		return nil, input, false
	}
	nodeStr := input[1:spaceIdx]
	cmd := strings.TrimSpace(input[spaceIdx+1:])
	if nodeStr == "" || cmd == "" {
		return nil, input, false
	}
	for _, part := range strings.Split(nodeStr, ",") {
		if n := strings.TrimSpace(part); n != "" {
			nodes = append(nodes, n)
		}
	}
	if len(nodes) == 0 {
		return nil, input, false
	}
	return nodes, cmd, true
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
			var msg string
			var quit bool
			switch cmd {
			case "save":
				msg = s.handleSave(args)
			case "diff":
				msg = s.handleDiff()
			case "mode":
				msg = s.handleMode(args)
			case "only":
				msg = s.handleOnly(args)
			case "all":
				s.onlyNodes = nil
				msg = "已恢复全部节点"
			case "nodes":
				msg = s.handleNodes()
			default:
				quit, msg = HandleSessionCommand(cmd, args, s.sessMgr)
			}
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

// parseLocalPipeline 从命令中提取 |> 操作符，分离远程命令和本地管道。
// 返回 (remoteCmd, localCmd, hasLocal)。
// 只识别第一个 |>，后续 | 属于本地管道部分。
// 若 |> 两侧任一为空，则视为不含管道，返回原始命令和 false。
func parseLocalPipeline(command string) (string, string, bool) {
	idx := strings.Index(command, "|>")
	if idx < 0 {
		return command, "", false
	}
	remoteCmd := strings.TrimSpace(command[:idx])
	localCmd := strings.TrimSpace(command[idx+2:])
	if remoteCmd == "" || localCmd == "" {
		return command, "", false
	}
	return remoteCmd, localCmd, true
}

// dispatchCommand 将命令分发到目标节点并展示结果。
// 支持 @node1,node2 前缀临时指定目标节点。
func (s *interactiveShell) dispatchCommand(ctx context.Context, command string, rl *readline.Instance) {
	// 解析 @node1,node2 前缀
	var overrideNodes []string
	if nodes, cmd, ok := parseNodePrefix(command); ok {
		overrideNodes = nodes
		command = cmd
	}

	sessions := s.getTargetSessions(overrideNodes)
	if len(sessions) == 0 {
		if len(overrideNodes) > 0 {
			fmt.Fprintf(os.Stderr, "节点 [%s] 未找到或已断开\n", strings.Join(overrideNodes, ", "))
		} else if s.onlyNodes != nil {
			fmt.Fprintln(os.Stderr, "限定的节点均已断开，执行 :all 恢复全部节点")
		} else {
			fmt.Fprintln(os.Stderr, "没有活跃的节点连接")
		}
		return
	}

	// 用亮黄色打印正在执行的命令（含节点前缀提示）
	const yellow = "\033[1;33m"
	const reset = "\033[0m"
	if len(overrideNodes) > 0 {
		fmt.Fprintf(os.Stdout, "%s$ @%s %s%s\n", yellow, strings.Join(overrideNodes, ","), command, reset)
	} else {
		fmt.Fprintf(os.Stdout, "%s$ %s%s\n", yellow, command, reset)
	}

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

	// 检测 |> 本地管道操作符
	if remoteCmd, localCmd, ok := parseLocalPipeline(command); ok {
		if s.mode == output.ModeStream {
			fmt.Fprintln(os.Stderr, "提示: stream 模式不支持 |> 本地管道，请切换到 grouped 或 merged 模式")
			return
		}
		s.dispatchLocalPipeline(cmdCtx, sessions, remoteCmd, localCmd)
		return
	}

	effectiveMode := s.mode
	if effectiveMode != output.ModeStream && requiresStreamMode(command) {
		fmt.Fprintln(os.Stderr, "检测到持续输出命令，自动切换到 stream 模式")
		effectiveMode = output.ModeStream
	}

	switch effectiveMode {
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

// dispatchLocalPipeline 先在所有节点执行 remoteCmd，收集输出后合并（无节点前缀），
// 再将合并内容作为 stdin 传给本地 localCmd（通过 sh -c 执行）。
// 保存远程结果到 lastResult 供 :save 使用。
func (s *interactiveShell) dispatchLocalPipeline(ctx context.Context, sessions []executor.Session, remoteCmd, localCmd string) {
	result, err := s.exec.ExecOnAll(ctx, sessions, remoteCmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "远程命令执行失败: %v\n", err)
		return
	}

	// 保存远程结果，供 :save 使用
	s.lastResult = result

	// 合并所有节点输出（无节点前缀）
	var sb strings.Builder
	for _, r := range result.Results {
		if r.Output == "" {
			continue
		}
		sb.WriteString(r.Output)
		if !strings.HasSuffix(r.Output, "\n") {
			sb.WriteByte('\n')
		}
	}

	// 执行本地命令，将合并输出作为 stdin
	cmd := exec.CommandContext(ctx, "sh", "-c", localCmd)
	cmd.Stdin = strings.NewReader(sb.String())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			// 真实错误（命令不存在等），打印到 stderr
			fmt.Fprintf(os.Stderr, "本地命令执行失败: %v\n", err)
		}
		// 非零退出码（如 grep 无匹配）静默处理
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

// handleDiff 处理 :diff 命令，对上一条命令的结果做节点间逐行对比。
func (s *interactiveShell) handleDiff() string {
	if s.lastResult == nil {
		return "没有可对比的内容，请先执行一条命令"
	}
	fmt.Print(output.RenderDiff(s.lastResult, output.IsColorSupported()))
	return ""
}

// handleMode 处理 :mode 命令，动态切换输出模式。
func (s *interactiveShell) handleMode(args []string) string {
	if len(args) == 0 {
		return fmt.Sprintf("当前模式: %s\n用法: :mode grouped|merged|stream", s.mode)
	}
	newMode := output.OutputMode(strings.ToLower(args[0]))
	switch newMode {
	case output.ModeGrouped, output.ModeMerged, output.ModeStream:
		s.mode = newMode
		return fmt.Sprintf("已切换到 %s 模式", newMode)
	default:
		return fmt.Sprintf("未知模式: %s，可用模式: grouped merged stream", args[0])
	}
}

// handleOnly 处理 :only 命令，将命令目标限定到指定节点子集。
func (s *interactiveShell) handleOnly(args []string) string {
	if len(args) == 0 {
		return "用法: :only <node1> [node2 ...]\n输入 :all 恢复全部节点"
	}
	active := s.sessMgr.GetActiveSessions()
	activeMap := make(map[string]bool, len(active))
	for _, ns := range active {
		activeMap[ns.NodeName] = true
	}
	filter := make(map[string]bool, len(args))
	var unknown []string
	for _, n := range args {
		filter[n] = true
		if !activeMap[n] {
			unknown = append(unknown, n)
		}
	}
	s.onlyNodes = filter
	msg := fmt.Sprintf("已限定节点: %s", strings.Join(args, ", "))
	if len(unknown) > 0 {
		msg += fmt.Sprintf("\n警告: 以下节点不在活跃连接中: %s", strings.Join(unknown, ", "))
	}
	return msg
}

// requiresStreamMode 检测命令是否需要流式输出模式。
// 匹配 tail -f/-F/--follow 和 watch 命令。
func requiresStreamMode(command string) bool {
	cmd := strings.TrimSpace(command)
	// 只取第一个管道前的部分检测
	if idx := strings.IndexByte(cmd, '|'); idx >= 0 {
		cmd = strings.TrimSpace(cmd[:idx])
	}
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return false
	}
	switch parts[0] {
	case "watch":
		return true
	case "tail":
		for _, p := range parts[1:] {
			if p == "--follow" || p == "-f" || p == "-F" {
				return true
			}
			// 合并短标志，如 -fn、-Fn
			if strings.HasPrefix(p, "-") && !strings.HasPrefix(p, "--") {
				if strings.ContainsAny(p[1:], "fF") {
					return true
				}
			}
		}
	}
	return false
}

// handleNodes 处理 :nodes 命令，列出所有节点及其连接状态。
func (s *interactiveShell) handleNodes() string {
	all := s.sessMgr.GetAllSessions()
	if len(all) == 0 {
		return "没有节点"
	}

	colorEnabled := output.IsColorSupported()
	const (
		colorGreen  = "\033[32m"
		colorRed    = "\033[31m"
		colorYellow = "\033[33m"
		colorGray   = "\033[90m"
		reset       = "\033[0m"
	)

	active := 0
	for _, ns := range all {
		if !ns.IsDisconnected() {
			active++
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("节点列表 (%d 个, 活跃 %d 个)", len(all), active))
	if s.onlyNodes != nil {
		sb.WriteString(fmt.Sprintf(", :only 限定 %d 个", len(s.onlyNodes)))
	}
	sb.WriteString(":\n")

	for _, ns := range all {
		disconnected := ns.IsDisconnected()
		inOnly := s.onlyNodes != nil && s.onlyNodes[ns.NodeName]

		var line string
		if disconnected {
			reason := ""
			if err := ns.GetDisconnectErr(); err != nil {
				reason = fmt.Sprintf("  (%s)", err.Error())
			}
			if colorEnabled {
				line = fmt.Sprintf("  %s✗ %-20s%s%s%s\n",
					colorRed, ns.NodeName, reset, colorGray, reason+reset)
			} else {
				line = fmt.Sprintf("  ✗ %-20s [已断开%s]\n", ns.NodeName, reason)
			}
		} else if inOnly {
			if colorEnabled {
				line = fmt.Sprintf("  %s✓ %-20s%s%s← 限定中%s\n",
					colorGreen, ns.NodeName, reset, colorYellow, reset)
			} else {
				line = fmt.Sprintf("  ✓ %-20s ← 限定中\n", ns.NodeName)
			}
		} else {
			if colorEnabled {
				line = fmt.Sprintf("  %s✓ %s%s\n", colorGreen, ns.NodeName, reset)
			} else {
				line = fmt.Sprintf("  ✓ %s\n", ns.NodeName)
			}
		}
		sb.WriteString(line)
	}
	return sb.String()
}
