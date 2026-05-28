package ssh

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/brickerxu/beelog/internal/config"
	"github.com/brickerxu/beelog/internal/executor"
	"github.com/brickerxu/beelog/internal/output"
	"github.com/brickerxu/beelog/internal/totp"
	gossh "golang.org/x/crypto/ssh"
)

// sshConnManager 实现 SSHConnManager 接口
type sshConnManager struct {
	totpGen    totp.TOTPGenerator
	interactor JumpServerInteractor
	debug      bool // 调试模式

	// keepalive 管理: nodeName -> cancel func
	keepaliveMu sync.Mutex
	keepalives  map[string]context.CancelFunc
}

// NewSSHConnManager 创建 SSHConnManager 实例
func NewSSHConnManager(totpGen totp.TOTPGenerator, interactor JumpServerInteractor) SSHConnManager {
	return &sshConnManager{
		totpGen:    totpGen,
		interactor: interactor,
		keepalives: make(map[string]context.CancelFunc),
	}
}

// NewSSHConnManagerWithDebug 创建带调试模式的 SSHConnManager 实例
func NewSSHConnManagerWithDebug(totpGen totp.TOTPGenerator, interactor JumpServerInteractor, debug bool) SSHConnManager {
	return &sshConnManager{
		totpGen:    totpGen,
		interactor: interactor,
		debug:      debug,
		keepalives: make(map[string]context.CancelFunc),
	}
}

// Connect 建立到目标节点的连接
// 流程: SSH连接JumpServer → keyboard-interactive提交TOTP → 打开shell → 导航到目标节点
func (m *sshConnManager) Connect(ctx context.Context, jumpCfg config.JumpServerConfig, nodeName string) (*NodeSession, error) {
	// 1. 读取 SSH 私钥
	signer, err := m.loadPrivateKey(jumpCfg.PrivateKey, jumpCfg.Passphrase)
	if err != nil {
		return nil, fmt.Errorf("[%s] 加载 SSH 私钥失败: %w", nodeName, err)
	}

	// 2. 构建 SSH 客户端配置
	sshConfig := &gossh.ClientConfig{
		User: jumpCfg.User,
		Auth: []gossh.AuthMethod{
			gossh.PublicKeys(signer),
			gossh.KeyboardInteractive(m.keyboardInteractiveCallback(jumpCfg.TOTPSeed)),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	// 3. 连接 JumpServer（支持 context 超时）
	addr := fmt.Sprintf("%s:%d", jumpCfg.Host, jumpCfg.Port)
	client, err := m.dialWithContext(ctx, "tcp", addr, sshConfig)
	if err != nil {
		return nil, fmt.Errorf("[%s] SSH 连接 JumpServer 失败: %w", nodeName, err)
	}

	// 4. 打开 shell 会话
	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("[%s] 创建 SSH 会话失败: %w", nodeName, err)
	}

	// 5. 获取 stdin/stdout/stderr 管道
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("[%s] 获取 stdin 管道失败: %w", nodeName, err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		stdin.Close()
		session.Close()
		client.Close()
		return nil, fmt.Errorf("[%s] 获取 stdout 管道失败: %w", nodeName, err)
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		stdin.Close()
		session.Close()
		client.Close()
		return nil, fmt.Errorf("[%s] 获取 stderr 管道失败: %w", nodeName, err)
	}

	// 6. 请求 PTY 并启动 shell
	if err := session.RequestPty("xterm", 80, 200, gossh.TerminalModes{
		gossh.ECHO:          0,
		gossh.TTY_OP_ISPEED: 14400,
		gossh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		stdin.Close()
		session.Close()
		client.Close()
		return nil, fmt.Errorf("[%s] 请求 PTY 失败: %w", nodeName, err)
	}

	if err := session.Shell(); err != nil {
		stdin.Close()
		session.Close()
		client.Close()
		return nil, fmt.Errorf("[%s] 启动 shell 失败: %w", nodeName, err)
	}

	// 7. 通过 JumpServerInteractor 导航到目标节点（placeholder 调用）
	if m.interactor != nil {
		if err := m.interactor.NavigateToNode(session, stdin, stdout, nodeName); err != nil {
			stdin.Close()
			session.Close()
			client.Close()
			return nil, fmt.Errorf("[%s] 导航到目标节点失败: %w", nodeName, err)
		}
	}

	return &NodeSession{
		NodeName:     nodeName,
		JumpClient:   client,
		ShellSession: session,
		Stdin:        stdin,
		Stdout:       stdout,
		Stderr:       stderr,
	}, nil
}

// Execute 在目标节点上执行一次性命令，读取输出直到超时或完成
// PTY 模式下使用 \r 发送命令，通过唯一 marker 检测命令完成，
// 并过滤掉命令回显、ANSI 转义序列和 marker 行本身。
func (m *sshConnManager) Execute(ctx context.Context, session *NodeSession, command string) (*executor.ExecResult, error) {
	start := time.Now()
	result := &executor.ExecResult{
		NodeName: session.NodeName,
	}

	// 使用唯一 marker 检测命令完成
	marker := fmt.Sprintf("__BEELOG_END_%d__", time.Now().UnixNano())
	// PTY 模式下用 \r 作为回车
	fullCmd := fmt.Sprintf("%s ; echo %s $?\r", command, marker)

	if _, err := io.WriteString(session.Stdin, fullCmd); err != nil {
		result.Error = fmt.Errorf("[%s] 发送命令失败: %w", session.NodeName, err)
		result.Duration = time.Since(start)
		return result, result.Error
	}

	// 读取输出直到看到当前 marker 或超时
	// 使用字节级读取而非 bufio.Scanner，因为 PTY 输出不是干净的行流
	var accumulated bytes.Buffer
	readBuf := make([]byte, 4096)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			// 每次 Read 前检查 context，确保取消后能及时退出，
			// 避免与下一条命令的读取 goroutine 竞争同一 stdout 管道
			select {
			case <-ctx.Done():
				return
			default:
			}
			n, err := session.Stdout.Read(readBuf)
			if n > 0 {
				accumulated.Write(readBuf[:n])
				// 检查当前 marker 是否出现在行首（即 echo 的实际输出，而非命令回显）
				// 命令回显中 marker 嵌在 "... ; echo __BEELOG_END_xxx $?" 里，
				// 而 echo 的实际输出是独立一行 "__BEELOG_END_xxx 0"
				if markerOnOwnLine(accumulated.Bytes(), []byte(marker)) {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		// 发送 Ctrl+C 终止远端命令（与 Stream() 保持一致）
		session.Stdin.Write([]byte{0x03})
		// 等待读取 goroutine 退出，避免残留 goroutine 与下一条命令竞争 stdout。
		// 发送 0x03 后远端会输出 ^C 并返回提示符，Read() 将很快解除阻塞，
		// goroutine 检查到 ctx.Done() 后退出。
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		errMsg := "命令被中断"
		if ctx.Err() == context.DeadlineExceeded {
			errMsg = "命令执行超时"
		}
		result.Error = fmt.Errorf("[%s] %s", session.NodeName, errMsg)
		result.Duration = time.Since(start)
		return result, result.Error
	case <-done:
		result.Duration = time.Since(start)
		// 解析输出：过滤命令回显、marker 行、ANSI 转义
		raw := accumulated.String()
		if m.debug {
			fmt.Fprintf(os.Stderr, "[EXEC DEBUG] [%s] command=%q\n", session.NodeName, command)
			fmt.Fprintf(os.Stderr, "[EXEC DEBUG] [%s] raw output=%q\n", session.NodeName, raw)
		}
		result.Output, result.ExitCode = parseExecuteOutput(raw, command, marker)
		if m.debug {
			fmt.Fprintf(os.Stderr, "[EXEC DEBUG] [%s] parsed output=%q exitCode=%d\n", session.NodeName, result.Output, result.ExitCode)
		}
		return result, nil
	}
}

// parseExecuteOutput 从 PTY 原始输出中提取干净的命令结果
// 过滤掉：命令回显行、marker 行、ANSI 转义序列、shell 提示符、终端标题序列
func parseExecuteOutput(raw, command, marker string) (string, int) {
	exitCode := 0

	lines := strings.Split(raw, "\n")

	// 第一遍：找到命令回显行的位置和当前 marker 输出行的位置
	// 命令回显行：包含原始命令 AND 当前 marker（PTY 回显整条 fullCmd）
	// marker 输出行：以当前 marker 开头，后跟退出码（echo 的实际输出）
	echoIdx := -1
	markerIdx := -1

	for i, line := range lines {
		cleaned := stripAllEscapes(strings.TrimRight(line, "\r"))
		cleaned = strings.TrimRight(cleaned, " \t")

		// 命令回显行：包含命令文本和当前 marker
		if echoIdx == -1 && strings.Contains(cleaned, strings.TrimSpace(command)) && strings.Contains(cleaned, marker) {
			echoIdx = i
			continue
		}

		// 当前 marker 的 echo 输出行：以 marker 开头（可能前面有空白）
		trimmed := strings.TrimLeft(cleaned, " \t")
		if strings.HasPrefix(trimmed, marker) {
			markerIdx = i
			// 解析退出码
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				fmt.Sscanf(parts[len(parts)-1], "%d", &exitCode)
			}
			break // marker 输出行之后的内容不需要
		}
	}

	// 第二遍：提取命令回显和 marker 输出之间的行作为实际输出
	startIdx := echoIdx + 1 // 命令回显之后
	if echoIdx == -1 {
		startIdx = 0 // 没找到回显，从头开始
	}
	endIdx := markerIdx
	if markerIdx == -1 {
		endIdx = len(lines) // 没找到 marker，取到末尾
	}

	var output []string
	for i := startIdx; i < endIdx; i++ {
		line := lines[i]
		line = strings.TrimRight(line, "\r")

		cleaned := stripAllEscapes(line)
		cleaned = strings.TrimRight(cleaned, " \t")

		// 跳过空行
		if cleaned == "" {
			continue
		}

		// 跳过任何包含 __BEELOG_END_ 的行（旧 marker 残留）
		if strings.Contains(cleaned, "__BEELOG_END_") {
			continue
		}

		// 跳过 shell 提示符行（如 [user@host dir]$ 或 user@host:~$）
		if isShellPrompt(cleaned) {
			continue
		}

		output = append(output, cleaned)
	}

	return strings.Join(output, "\n"), exitCode
}

// isShellPrompt 判断一行是否是 shell 提示符
// 常见模式: "[user@host dir]$", "[user@host dir]#", "user@host:~$"
func isShellPrompt(line string) bool {
	trimmed := strings.TrimRight(line, " \t")
	if trimmed == "" {
		return false
	}
	lastChar := trimmed[len(trimmed)-1]
	// 以 $ 或 # 结尾，且包含 @ 或 ] 等提示符特征
	if (lastChar == '$' || lastChar == '#') &&
		(strings.Contains(trimmed, "@") || strings.Contains(trimmed, "]")) {
		return true
	}
	return false
}

// stripAllEscapes 去除字符串中的所有终端转义序列
// 包括 CSI 序列 (ESC[...X) 和 OSC 序列 (ESC]...BEL)
func stripAllEscapes(s string) string {
	result := make([]byte, 0, len(s))
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == '\x1b' {
			if s[i+1] == '[' {
				// CSI 序列: ESC[ ... 字母
				i += 2
				for i < len(s) && !((s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z')) {
					i++
				}
				if i < len(s) {
					i++ // 跳过终止字母
				}
			} else if s[i+1] == ']' {
				// OSC 序列: ESC] ... BEL(\a) 或 ESC] ... ST(ESC\)
				i += 2
				for i < len(s) {
					if s[i] == '\a' {
						i++
						break
					}
					if i+1 < len(s) && s[i] == '\x1b' && s[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
			} else {
				// 其他 ESC 序列，跳过 ESC + 下一个字符
				i += 2
			}
		} else {
			result = append(result, s[i])
			i++
		}
	}
	return string(result)
}

// isCommandEcho 判断一行是否是命令回显
func isCommandEcho(line, command string) bool {
	// 去除 ANSI 后的行如果包含原始命令的主要部分，认为是回显
	cleanLine := strings.TrimSpace(line)
	cleanCmd := strings.TrimSpace(command)

	if cleanCmd == "" {
		return false
	}

	// 完全匹配或包含完整命令（可能带 ; echo __BEELOG_END_... 后缀）
	if strings.Contains(cleanLine, cleanCmd) && strings.Contains(cleanLine, "__BEELOG_END_") {
		return true
	}

	return false
}

// markerOnOwnLine 检查 marker 是否出现在独立行的开头
// PTY 回显会把 marker 嵌在命令行中（如 "... ; echo __BEELOG_END_xxx $?"），
// 而 echo 的实际输出是独立一行（如 "\n__BEELOG_END_xxx 0\r\n"）。
// 只有后者才表示命令真正执行完毕。
func markerOnOwnLine(buf []byte, marker []byte) bool {
	// 在 buf 中查找所有 marker 出现的位置
	searchFrom := 0
	for {
		idx := bytes.Index(buf[searchFrom:], marker)
		if idx < 0 {
			return false
		}
		absIdx := searchFrom + idx

		// 检查 marker 前面是否是行首（\n 或 \r\n 或 buf 开头）
		if absIdx == 0 {
			return true
		}
		prev := buf[absIdx-1]
		if prev == '\n' || prev == '\r' {
			return true
		}

		// 这个 marker 出现在行中间（命令回显），继续搜索下一个
		searchFrom = absIdx + len(marker)
	}
}

// resolveTimestamp tries to parse a log timestamp from the line content.
// Falls back to time.Now() if parsing fails.
func resolveTimestamp(line string) time.Time {
	if ts, ok := output.ParseLogTimestamp(line); ok {
		return ts
	}
	return time.Now()
}

// Stream 在目标节点上执行流式命令，持续读取输出发送到 channel
// PTY 模式下使用 \r 发送命令，过滤命令回显、ANSI 转义和 marker 行
func (m *sshConnManager) Stream(ctx context.Context, session *NodeSession, command string, output chan<- executor.OutputLine) error {
	// PTY 模式下用 \r 作为回车
	if _, err := io.WriteString(session.Stdin, command+"\r"); err != nil {
		return fmt.Errorf("[%s] 发送流式命令失败: %w", session.NodeName, err)
	}

	type readResult struct {
		data []byte
		err  error
	}
	// stopCh 用于通知读取 goroutine 停止向 readCh 发送数据
	stopCh := make(chan struct{})
	readCh := make(chan readResult, 1)
	doneCh := make(chan struct{})

	// 在独立 goroutine 中做阻塞读，避免 Read() 阻塞时无法响应 ctx 取消。
	// 通过 stopCh 协调退出：ctx 取消时主循环关闭 stopCh，goroutine 下次发送前感知并退出。
	go func() {
		defer close(doneCh)
		readBuf := make([]byte, 4096)
		for {
			n, err := session.Stdout.Read(readBuf)
			var data []byte
			if n > 0 {
				data = make([]byte, n)
				copy(data, readBuf[:n])
			}
			select {
			case readCh <- readResult{data: data, err: err}:
			case <-stopCh:
				return
			}
			if err != nil {
				return
			}
		}
	}()

	var lineBuf bytes.Buffer

	for {
		select {
		case <-ctx.Done():
			// 发送 Ctrl+C 终止远程命令
			session.Stdin.Write([]byte{0x03}) // ETX (Ctrl+C)
			// 关闭 stopCh，解除 goroutine 在 readCh 上的阻塞，让其退出
			close(stopCh)
			// 等待 goroutine 退出，避免残留 goroutine 与下一条命令竞争 stdout
			select {
			case <-doneCh:
			case <-time.After(2 * time.Second):
			}
			return ctx.Err()
		case r := <-readCh:
			for _, b := range r.data {
				if b == '\n' {
					line := strings.TrimRight(lineBuf.String(), "\r")
					lineBuf.Reset()

					// 过滤：ANSI 转义、命令回显、marker 行、shell 提示符
					cleaned := stripAllEscapes(line)
					cleaned = strings.TrimRight(cleaned, " \t")
					if cleaned == "" || strings.Contains(cleaned, "__BEELOG_END_") ||
						isCommandEcho(cleaned, command) || isShellPrompt(cleaned) {
						continue
					}

					output <- executor.OutputLine{
						NodeName:  session.NodeName,
						Content:   cleaned,
						Timestamp: resolveTimestamp(cleaned),
						IsError:   false,
					}
				} else {
					lineBuf.WriteByte(b)
				}
			}
			if r.err != nil {
				if r.err == io.EOF {
					// 输出剩余缓冲
					if lineBuf.Len() > 0 {
						line := strings.TrimRight(lineBuf.String(), "\r")
						cleaned := stripAllEscapes(line)
						if cleaned != "" && !strings.Contains(cleaned, "__BEELOG_END_") && !isCommandEcho(cleaned, command) && !isShellPrompt(cleaned) {
							output <- executor.OutputLine{
								NodeName:  session.NodeName,
								Content:   cleaned,
								Timestamp: resolveTimestamp(cleaned),
								IsError:   false,
							}
						}
					}
					return nil
				}
				return fmt.Errorf("[%s] 读取流式输出失败: %w", session.NodeName, r.err)
			}
		}
	}
}

// Close 关闭节点会话和 JumpServer 连接
func (m *sshConnManager) Close(session *NodeSession) error {
	if session == nil {
		return nil
	}

	// 先停止 keepalive
	m.StopKeepalive(session)

	var errs []string

	if session.Stdin != nil {
		if err := session.Stdin.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("关闭 stdin: %v", err))
		}
	}

	if session.ShellSession != nil {
		if err := session.ShellSession.Close(); err != nil {
			// session.Close() 在 shell 已结束时可能返回错误，忽略
			_ = err
		}
	}

	if session.JumpClient != nil {
		if err := session.JumpClient.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("关闭 SSH 连接: %v", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("[%s] 关闭连接时出错: %s", session.NodeName, strings.Join(errs, "; "))
	}
	return nil
}

// StartKeepalive 启动心跳保活，定期发送空行防止 JumpServer 空闲断开
// 心跳失败时标记节点为断开状态并记录日志
func (m *sshConnManager) StartKeepalive(ctx context.Context, session *NodeSession, interval time.Duration) error {
	m.keepaliveMu.Lock()
	defer m.keepaliveMu.Unlock()

	// 如果已有 keepalive 运行，先停止
	if cancel, ok := m.keepalives[session.NodeName]; ok {
		cancel()
	}

	keepCtx, cancel := context.WithCancel(ctx)
	m.keepalives[session.NodeName] = cancel

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-keepCtx.Done():
				return
			case <-ticker.C:
				// 发送空行作为心跳（PTY 模式下用 \r）
				if _, err := session.Stdin.Write([]byte("\r")); err != nil {
					fmt.Fprintf(os.Stderr, "[%s] 心跳发送失败，标记节点为断开: %v\n", session.NodeName, err)
					session.MarkDisconnected(err)
					return
				}
			}
		}
	}()

	return nil
}

// StopKeepalive 停止心跳保活
func (m *sshConnManager) StopKeepalive(session *NodeSession) error {
	m.keepaliveMu.Lock()
	defer m.keepaliveMu.Unlock()

	if cancel, ok := m.keepalives[session.NodeName]; ok {
		cancel()
		delete(m.keepalives, session.NodeName)
	}
	return nil
}

// loadPrivateKey 从文件加载 SSH 私钥，支持 passphrase 解密
func (m *sshConnManager) loadPrivateKey(keyPath string, passphrase string) (gossh.Signer, error) {
	// 展开 ~ 路径
	if strings.HasPrefix(keyPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("获取用户主目录失败: %w", err)
		}
		keyPath = home + keyPath[1:]
	}

	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("读取私钥文件 %s 失败: %w", keyPath, err)
	}

	if passphrase != "" {
		signer, err := gossh.ParsePrivateKeyWithPassphrase(keyData, []byte(passphrase))
		if err != nil {
			return nil, fmt.Errorf("使用 passphrase 解析私钥失败: %w", err)
		}
		return signer, nil
	}

	signer, err := gossh.ParsePrivateKey(keyData)
	if err != nil {
		return nil, fmt.Errorf("解析私钥失败: %w", err)
	}
	return signer, nil
}

// keyboardInteractiveCallback 创建 keyboard-interactive 认证回调
// 在 JumpServer 的 TOTP 挑战中自动提交验证码
func (m *sshConnManager) keyboardInteractiveCallback(totpSeed string) gossh.KeyboardInteractiveChallenge {
	return func(user, instruction string, questions []string, echos []bool) ([]string, error) {
		answers := make([]string, len(questions))
		for i := range questions {
			// 对每个问题都尝试用 TOTP 验证码回答
			// JumpServer 通常只有一个 TOTP 挑战问题
			code, err := m.totpGen.Generate(totpSeed)
			if err != nil {
				return nil, fmt.Errorf("生成 TOTP 验证码失败: %w", err)
			}
			answers[i] = code
		}
		return answers, nil
	}
}

// dialWithContext 使用 context 控制超时的 SSH 连接
func (m *sshConnManager) dialWithContext(ctx context.Context, network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
	type dialResult struct {
		client *gossh.Client
		err    error
	}

	resultCh := make(chan dialResult, 1)
	go func() {
		client, err := gossh.Dial(network, addr, config)
		resultCh <- dialResult{client: client, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("连接超时: %w", ctx.Err())
	case result := <-resultCh:
		return result.client, result.err
	}
}
