package shell

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/brickerxu/beelog/internal/executor"
	"github.com/brickerxu/beelog/internal/ssh"
)

// remoteCompleter 实现 readline.AutoCompleter 接口
// 通过在远程节点上执行 ls 命令来提供文件路径补全
type remoteCompleter struct {
	sessMgr SessionManager
	execFn  executor.ExecFunc
	debug   bool
}

// NewRemoteCompleter 创建远程文件路径补全器
// sessMgr: 用于获取活跃会话
// execFn: 用于在远程节点上执行 ls 命令
func NewRemoteCompleter(sessMgr SessionManager, execFn executor.ExecFunc) *remoteCompleter {
	return &remoteCompleter{
		sessMgr: sessMgr,
		execFn:  execFn,
	}
}

// NewRemoteCompleterWithDebug 创建带调试模式的远程文件路径补全器
func NewRemoteCompleterWithDebug(sessMgr SessionManager, execFn executor.ExecFunc, debug bool) *remoteCompleter {
	return &remoteCompleter{
		sessMgr: sessMgr,
		execFn:  execFn,
		debug:   debug,
	}
}

// Do 实现 readline.AutoCompleter 接口
// line: 当前输入行的 rune 切片
// pos: 光标位置
// 返回: 补全候选项列表和共享前缀长度
//
// chzyer/readline 的 Do 返回值语义:
//   - newLine: 候选项列表，每个候选项是需要 **追加** 到当前输入的后缀
//   - length: 候选项与已输入内容共享的字符数（仅用于候选列表显示时的前缀展示）
//   - buf.WriteRunes 始终是追加操作，不会替换已有内容
//
// 示例 (来自 readline 源码):
//
//	Do("g", 1) => ["o", "it", "it-shell", "rep"], 1
//	Do("gi", 2) => ["t", "t-shell"], 2
func (c *remoteCompleter) Do(line []rune, pos int) ([][]rune, int) {
	// 提取光标前的文本
	lineStr := string(line[:pos])

	// 提取最后一个 word（即用户正在输入的路径片段）
	lastWord := extractLastWord(lineStr)

	// 获取第一个活跃会话
	session := c.getFirstActiveSession()
	if session == nil {
		return nil, 0
	}

	// 在远程节点上执行 ls 获取补全候选
	candidates := c.fetchCandidates(session, lastWord)
	if len(candidates) == 0 {
		return nil, 0
	}

	if c.debug {
		fmt.Fprintf(os.Stderr, "[COMPLETER] lastWord=%q, candidates=%v\n", lastWord, candidates)
		for i, cand := range candidates {
			fmt.Fprintf(os.Stderr, "[COMPLETER]   [%d] %q (bytes: %x) hasPrefix=%v\n",
				i, cand, []byte(cand), strings.HasPrefix(cand, lastWord))
		}
	}

	// 唯一匹配：计算需要追加的后缀
	if len(candidates) == 1 {
		suffix := candidateSuffix(candidates[0], lastWord)
		if suffix == "" {
			// 已经完全匹配，无需追加
			return nil, 0
		}
		return [][]rune{[]rune(suffix)}, 0
	}

	// 多个匹配：先尝试补全公共前缀
	commonPrefix := longestCommonPrefix(candidates)
	if len(commonPrefix) > len(lastWord) {
		suffix := commonPrefix[len(lastWord):]
		return [][]rune{[]rune(suffix)}, 0
	}

	// 没有更多公共前缀可补全，返回所有候选项的后缀供用户选择
	// length 设为 lastWord 的长度，用于候选列表显示时展示共享前缀
	result := make([][]rune, len(candidates))
	for i, cand := range candidates {
		suffix := candidateSuffix(cand, lastWord)
		result[i] = []rune(suffix)
	}
	return result, len([]rune(lastWord))
}

// candidateSuffix 计算候选项相对于已输入内容的后缀
// 如果候选项以 lastWord 开头，返回差异部分
// 否则返回整个候选项（降级处理）
func candidateSuffix(candidate, lastWord string) string {
	if strings.HasPrefix(candidate, lastWord) {
		return candidate[len(lastWord):]
	}
	// 降级：候选项不以 lastWord 开头（可能是相对路径 vs 绝对路径）
	// 返回空避免错误追加
	return ""
}

// longestCommonPrefix 计算字符串切片的最长公共前缀
func longestCommonPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	prefix := strs[0]
	for _, s := range strs[1:] {
		for !strings.HasPrefix(s, prefix) {
			prefix = prefix[:len(prefix)-1]
			if prefix == "" {
				return ""
			}
		}
	}
	return prefix
}

// extractLastWord 从输入行中提取最后一个 word（路径片段）
// 以空格分隔，返回最后一个 token
func extractLastWord(line string) string {
	line = strings.TrimRight(line, " ")
	if line == "" {
		return ""
	}

	// 从后往前找到第一个空格
	lastSpace := strings.LastIndex(line, " ")
	if lastSpace == -1 {
		// 整行就是一个 word（可能是命令本身，也提供补全）
		return line
	}
	return line[lastSpace+1:]
}

// getFirstActiveSession 获取第一个活跃的节点会话
// 如果没有活跃会话，返回 nil
func (c *remoteCompleter) getFirstActiveSession() *ssh.NodeSession {
	sessions := c.sessMgr.GetActiveSessions()
	if len(sessions) == 0 {
		return nil
	}
	return sessions[0]
}

// fetchCandidates 在远程节点上执行 ls 命令获取补全候选项
func (c *remoteCompleter) fetchCandidates(session *ssh.NodeSession, partial string) []string {
	// 构建 ls 命令
	// 使用 ls -1 -d 来列出匹配的文件/目录，-d 防止展开目录内容
	// 对于空 partial，列出当前目录
	var lsCmd string
	if partial == "" {
		lsCmd = "ls -1 -dF */ * 2>/dev/null"
	} else {
		// 对 partial 路径进行通配符匹配
		lsCmd = "ls -1 -dF " + shellEscape(partial) + "* 2>/dev/null"
	}

	// 使用短超时执行，避免阻塞用户输入
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := c.execFn(ctx, session, lsCmd)
	if err != nil || result == nil {
		return nil
	}

	if c.debug {
		fmt.Fprintf(os.Stderr, "[COMPLETER] lsCmd=%q\n", lsCmd)
		fmt.Fprintf(os.Stderr, "[COMPLETER] raw output=%q\n", result.Output)
		fmt.Fprintf(os.Stderr, "[COMPLETER] raw output bytes=%x\n", []byte(result.Output))
	}

	// 解析 ls 输出
	return parseLsOutput(result.Output)
}

// parseLsOutput 解析 ls -F 命令的输出为候选项列表
// -F 标志会给目录追加 /，可执行文件追加 *，符号链接追加 @，管道追加 |
// 保留目录的 / 后缀，去掉其他类型标记
func parseLsOutput(output string) []string {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil
	}

	lines := strings.Split(output, "\n")
	candidates := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// 过滤掉 BEELOG 命令标记行
		if strings.Contains(line, "__BEELOG_END_") {
			continue
		}
		// 过滤掉命令回显行（包含 ls -1 -d 的行）
		if strings.Contains(line, "ls -1 -d") {
			continue
		}
		// 过滤掉包含 $? 的行（marker 命令残留）
		if strings.Contains(line, "$?") {
			continue
		}
		// 去除 ANSI 转义序列
		cleaned := stripANSIString(line)
		cleaned = strings.TrimSpace(cleaned)
		if cleaned == "" {
			continue
		}
		// 过滤掉 shell 提示符行
		if isShellPromptLine(cleaned) {
			continue
		}
		// 处理 ls -F 的类型标记：保留 / (目录)，去掉 * @ | = (其他类型)
		cleaned = cleanLsClassifier(cleaned)
		candidates = append(candidates, cleaned)
	}
	return candidates
}

// cleanLsClassifier 处理 ls -F 追加的分类标记
// 保留目录的 / 后缀，去掉可执行文件的 *、符号链接的 @、管道的 | 等标记
func cleanLsClassifier(name string) string {
	if name == "" {
		return name
	}
	last := name[len(name)-1]
	switch last {
	case '/':
		// 目录，保留 /
		return name
	case '*', '@', '|', '=':
		// 可执行文件、符号链接、管道、socket，去掉标记
		return name[:len(name)-1]
	default:
		return name
	}
}

// isShellPromptLine 判断一行是否是 shell 提示符
// 常见模式: "[root@hostname path]#", "user@host:~$", "hostname>"
func isShellPromptLine(line string) bool {
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

// stripANSIString 去除字符串中的 ANSI 转义序列
// stripANSIString 去除字符串中的 ANSI 转义序列（包括 CSI 和 OSC 序列）
func stripANSIString(s string) string {
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

// shellEscape 对路径中的特殊字符进行转义
// 仅处理常见的需要转义的字符，保留通配符功能
func shellEscape(s string) string {
	// 不转义 / . - _ 这些路径中常见的字符
	// 转义空格和其他 shell 特殊字符
	replacer := strings.NewReplacer(
		" ", "\\ ",
		"(", "\\(",
		")", "\\)",
		"'", "\\'",
		"\"", "\\\"",
		"&", "\\&",
		";", "\\;",
		"|", "\\|",
	)
	return replacer.Replace(s)
}
