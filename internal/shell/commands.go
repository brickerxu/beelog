package shell

import (
	"fmt"
	"runtime"
	"strings"
)

// ParseSessionCommand 解析以冒号开头的会话管理命令
// 输入格式: ":command arg1 arg2 ..."
// 返回命令名（小写，不含冒号）和参数列表
func ParseSessionCommand(input string) (command string, args []string) {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, ":") {
		return "", nil
	}

	// 去掉冒号前缀
	input = input[1:]
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return "", nil
	}

	command = strings.ToLower(parts[0])
	if len(parts) > 1 {
		args = parts[1:]
	}
	return command, args
}

// HandleSessionCommand 处理会话管理命令
// 返回 quit 表示是否应退出 shell，message 为要显示给用户的消息
func HandleSessionCommand(cmd string, args []string, sessMgr SessionManager) (quit bool, message string) {
	switch cmd {
	case "quit", "exit":
		sessMgr.DisconnectAll()
		return true, ""

	case "disconnect":
		if len(args) == 0 {
			return false, "用法: :disconnect <node-name>\n" + helpText()
		}
		nodeName := args[0]
		err := sessMgr.DisconnectNode(nodeName)
		if err != nil {
			return false, fmt.Sprintf("断开节点 %s 失败: %v", nodeName, err)
		}
		return false, fmt.Sprintf("已断开节点: %s", nodeName)

	case "help":
		return false, helpText()

	default:
		return false, fmt.Sprintf("未知会话命令: :%s\n%s", cmd, helpText())
	}
}

// helpText 返回会话命令帮助文本
func helpText() string {
	help := `
beelog 交互式 Shell 帮助

会话管理命令:
  :quit / :exit                    断开所有连接并退出
  :disconnect <node>               断开指定节点的连接
  :save                            保存上一条命令的输出到默认目录
  :save <filename>                 保存到默认目录，使用指定文件名
  :save <filepath>                 保存到指定完整路径（如 ./result.txt）
  :save <name> --format <fmt>      指定格式保存（text/structured/json/csv）
  :diff                            对上一条命令的结果做节点间逐行对比
  :mode <grouped|merged|stream>    动态切换输出模式（无需重启）
  :nodes                           列出所有节点及连接状态（✓ 活跃 / ✗ 已断开）
  :only <node1> [node2 ...]        将后续命令限定到指定节点子集
  :all                             恢复对全部节点执行（取消 :only 限定）
  :help                            显示此帮助信息

节点子集执行（一次性）:
  @node1,node2 <命令>              仅对指定节点执行本条命令，不影响后续命令
  例: @web-1,web-2 systemctl restart nginx

本地管道:
  <远程命令> |> <本地命令>         将所有节点输出合并后，管道到本地命令处理
  例: tail -100 /var/log/app.log |> grep ERROR | sort

快捷键:
  ↑ / ↓                            浏览历史命令
  Tab                              远程文件路径补全；:disconnect/:only 后补全节点名；:mode 后补全模式名
  Ctrl+C                           终止当前命令（不退出 shell）
`

	// 根据操作系统显示不同的撤销提示
	if runtime.GOOS == "darwin" {
		help += "  Ctrl+Z / Ctrl+_              撤销输入（macOS 终端不支持 Command+Z）\n"
	} else {
		help += "  Ctrl+Z / Ctrl+_              撤销输入\n"
	}

	return help
}
