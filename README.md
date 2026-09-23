# beelog

通过 JumpServer 跳板机同时连接多个目标节点的交互式批量命令工具。

## 功能

- 通过 JumpServer SSH 连接，自动完成 TOTP 二次验证
- 交互式 shell，支持命令历史（上下键）和远程 Tab 文件路径补全
- 并发连接多节点，命令一次输入、所有节点同时执行
- 三种输出模式：按节点分组 / 按时间戳合并 / 实时流式，可用 `:mode` 动态切换
- 彩色输出区分不同节点，`tail -f` 等持续输出命令自动切换到 stream 模式
- **grep 搜索词高亮**：使用 `grep` 过滤日志时，自动以青色加粗高亮匹配的关键词
- **命令运行时长**：命令执行时实时显示 `[⏱ Xs]` 读秒，完成后追加节点耗时汇总
- **可编号命令历史**：`:history [pattern]` 按子串/正则过滤历史，`!N` 把第 N 条回填到输入行可再改再执行
- **节点子集执行**：`:only <nodes>` 持续限定节点子集；`@n1,n2 <命令>` 一次性限定
- **本地管道 `|>`**：把所有节点输出合并后交给本地命令处理（如 `... |> grep ERROR | sort`）
- **结果保存/对比**：`:save` 保存上一条命令输出（text/structured/json/csv）；`:diff` 做节点间逐行对比
- **配置管理子命令**：`beelog config init/show/edit` 交互式向导、脱敏查看、`$EDITOR` 编辑
- **shell 补全**：`beelog install-completion` 自动检测 bash/zsh（含 oh-my-zsh、Homebrew）并安装脚本
- Ctrl+C 可即时中断远程命令；连接保活，防止 JumpServer 空闲超时断开

## 构建

```bash
go build -o beelog ./cmd/
```

## 配置

创建配置文件 `~/.config/beelog/config.yaml`，参考 [examples/config.yaml](examples/config.yaml)：

```yaml
jumpserver:
  host: "bastion.example.com"
  port: 22
  user: "your-username"
  private_key: "~/.ssh/id_rsa"
  totp_seed: "YOUR_TOTP_SEED"

groups:
  web: ["web-1", "web-2"]

# 每个分组的默认工作目录（可选）
# 连接后自动 cd 到指定目录，未配置的分组保持默认主目录
workdirs:
  web: "/www/webapp/logs"
  database: "/var/lib/mysql"

defaults:
  output_mode: "grouped"            # 输出模式: grouped | merged | stream
  concurrency: 5                   # 最大并发连接数 (2-20)
  timeout: 60                       # 命令超时秒数
  max_retries: 3                    # 连接失败重试次数
  retry_delay: 5                    # 重试间隔秒数
  keepalive_interval: 300           # 心跳间隔秒数（默认 5 分钟，防止 JumpServer 空闲断开）
  save_dir: "~/beelog_log/"         # :save 命令的默认保存目录
  save_format: "text"               # :save 默认格式: text | structured | json | csv
```

确保 SSH 私钥权限为 600：

```bash
chmod 600 ~/.ssh/id_rsa
```

## 使用

### 基本用法

```bash
# 连接 web 分组的所有节点
beelog --group web

# 指定配置文件
beelog --group web --config /path/to/config.yaml

# 使用分组输出模式
beelog --group web --mode grouped

# 详细输出（显示连接过程）
beelog --group web --verbose
```

### 命令行参数

| 参数 | 缩写 | 默认值 | 说明 |
|------|------|--------|------|
| `--config` | `-c` | `~/.config/beelog/config.yaml` | 配置文件路径 |
| `--group` | `-g` | （运行 shell 时必填） | 节点分组名称 |
| `--list-groups` | `-l` | `false` | 列出配置中所有分组后直接退出，不建立连接 |
| `--mode` | `-m` | 配置文件值 | 输出模式：`grouped` / `merged` / `stream` |
| `--verbose` | `-v` | `false` | 详细输出 |
| `--debug` | | `false` | 调试模式 |
| `--timeout` | | 配置文件值 | 命令超时秒数 |
| `--concurrency` | | 配置文件值 | 最大并发数 |

### 子命令

| 子命令 | 说明 |
|--------|------|
| `beelog config init` | 交互式向导设置 JumpServer 账号（host/port/user/private_key/passphrase/totp_seed），已有 `groups`/`workdirs`/`defaults` 保留不动 |
| `beelog config show` | 打印当前配置（TOTP 种子与 passphrase 显示为 `***`） |
| `beelog config edit` | 用 `$EDITOR`（fallback: vim / vi / nano）打开配置文件，退出后自动校验 |
| `beelog install-completion` | 自动检测当前 shell 并安装补全脚本（支持 bash、zsh，含 oh-my-zsh、Homebrew） |

`beelog config` 支持别名 `beelog c`。所有子命令都遵循顶层 `--config` 指定的路径。

```bash
# 首次使用：向导式生成配置
beelog config init

# 检查配置（脱敏）
beelog config show

# 用 vim 直接编辑
EDITOR=vim beelog config edit

# 查看有哪些分组可用
beelog --list-groups
```

### 交互式 Shell

启动后进入交互式 shell，提示符格式为 `beelog [分组名:节点数]>`：

```
beelog [web:3]> tail -100 /var/log/app.log
[web-1] 2024-01-15 10:30:00 INFO  Application started
[web-2] 2024-01-15 10:30:01 INFO  Application started
[web-3] 2024-01-15 10:30:00 INFO  Application started

beelog [web:3]> df -h
=== [web-1] ===
Filesystem      Size  Used Avail Use% Mounted on
/dev/sda1        50G   20G   28G  42% /

=== [web-2] ===
Filesystem      Size  Used Avail Use% Mounted on
/dev/sda1        50G   15G   33G  31% /
```

### grep 搜索词高亮

命令中含有 `grep` 时，匹配到的关键词会以**青色加粗**显示。支持管道过滤、多模式、大小写不敏感等场景：

```
# 单词高亮
beelog [web:3]> tail -f /var/log/app.log | grep "ERROR"
[web-1] 2024-01-15 10:31:00 [ERROR] connection refused  ← ERROR 青色高亮

# 多关键词（-e 参数或多级 grep 管道，两个词都高亮）
beelog [web:3]> grep -e "ERROR" -e "TIMEOUT" app.log
beelog [web:3]> grep 206092 | grep 正常IP

# 大小写不敏感
beelog [web:3]> grep -i "warn" app.log

# -v 反向过滤（无高亮，因为无需标记）
beelog [web:3]> grep -v "DEBUG" app.log
```

> **注意**：beelog 使用 PTY 连接远程，远程 `grep --color=auto` 会输出红色转义码。beelog 会自动剥离这些原始颜色码，再统一用青色标注，确保显示一致。

### 快捷操作

| 操作 | 说明 |
|------|------|
| `↑` / `↓` | 浏览历史命令 |
| `Tab` | 远程文件路径补全；`:disconnect` / `:only` 后补全节点名；`:mode` 后补全模式名；`--group` 后补全分组名（需先装补全脚本） |
| `Ctrl+C` | 立即终止当前命令并拿回提示符（不退出 shell），下一条命令不会读到上一条残留输出 |
| `Ctrl+Z` / `Ctrl+_` | 撤销输入 |

**macOS 用户提示**：由于终端协议限制，Command+Z 无法被命令行程序捕获。请使用 `Ctrl+Z` 或 `Ctrl+_` 进行撤销。如果您习惯 Command 键，可以在终端设置中自定义键盘映射（iTerm2: Preferences → Keys，Terminal.app: Preferences → Profiles → Keyboard）。

### 会话管理命令

| 命令 | 说明 |
|------|------|
| `:quit` / `:exit` | 断开所有连接并退出 |
| `:disconnect <node>` | 断开指定节点 |
| `:nodes` | 列出所有节点及连接状态（✓ 活跃 / ✗ 已断开） |
| `:only <n1> [n2 ...]` | 将后续命令持续限定到指定节点子集 |
| `:all` | 恢复对全部节点执行（取消 `:only` 限定） |
| `:mode <grouped\|merged\|stream>` | 动态切换输出模式，无需重启 |
| `:diff` | 对上一条命令的结果做节点间逐行对比 |
| `:save` | 保存上一条命令的输出到默认目录 |
| `:save <filename>` | 保存到默认目录，使用指定文件名 |
| `:save <filepath>` | 保存到指定完整路径（如 `./result.txt`） |
| `:save <name> --format <fmt>` | 指定格式保存（`text` / `structured` / `json` / `csv`） |
| `:history [N\|all]` | 显示最近 N 条命令（默认 30），编号从 1 起递增 |
| `:history <pattern> [N\|all]` | 按子串/正则过滤历史（大小写不敏感），保留原编号 |
| `!<N>` | 把编号 N 的历史命令回填到输入行，可编辑再回车执行 |
| `:help` | 显示帮助信息 |

```
beelog [web:3]> :nodes
节点列表 (3):
  ✓ web-1
  ✓ web-2
  ✗ web-3    (已断开)

beelog [web:3]> :only web-1 web-2
已限定至 2 个节点

beelog [web:3→2]> :history nginx
匹配 "nginx" (3/12):
   4  tail /var/log/nginx/access.log
   7  systemctl status nginx
  12  grep ERROR /var/log/nginx/error.log
用 !N 把某条命令回填到输入行

beelog [web:3→2]> !7
beelog [web:3→2]> systemctl status nginx    # 已回填，可再修改再回车

beelog [web:3→2]> :all
已恢复全部节点

beelog [web:3]> :quit
```

### 节点子集与本地管道

**一次性子集执行**（不影响后续命令）：

```
beelog [web:5]> @web-1,web-2 systemctl restart nginx
```

**本地管道**：把所有节点输出合并后（无节点前缀）交给本地 shell 处理，适合做二次过滤/排序/统计：

```
beelog [web:5]> tail -100 /var/log/app.log |> grep ERROR | sort | uniq -c
beelog [web:5]> ss -tnp |> awk '{print $NF}' | sort | uniq -c
```

### 命令运行时长

每条命令执行期间会打印一行黄色 `$ <命令>  [⏱ 0s]` 并实时读秒；命令返回后读秒被替换为最终耗时。多节点执行完成后会追加一段节点耗时汇总（最快/最慢/平均），便于识别慢节点。

### 输出模式

**stream** — 实时流式，每行带节点前缀：
```
[web-1] log line 1
[web-2] log line 1
[web-1] log line 2
```

**grouped**（默认）— 按节点分组展示：
```
=== [web-1] ===
log line 1
log line 2

=== [web-2] ===
log line 1
log line 2
```

**merged** — 按时间戳排序合并：
```
[web-1] log line 1
[web-2] log line 1
[web-1] log line 2
```

## 常见问题 (FAQ)

### Q: 为什么 macOS 无法使用 Command+Z 撤销？

A: 这是终端环境的技术限制：

1. **终端协议限制**：标准终端协议（ANSI/VT100）不传递 Command 键事件
2. **系统级拦截**：macOS 的 Command 键被终端模拟器（Terminal.app、iTerm2）在应用层之前拦截
3. **解决方案**：使用 `Ctrl+Z` 或 `Ctrl+_`（所有平台通用）

如果您希望使用更舒适的快捷键，可以在终端设置中自定义键盘映射：
- **iTerm2**: Preferences → Keys → Key Bindings
- **Terminal.app**: Preferences → Profiles → Keyboard

### Q: JumpServer 连接超时怎么办？

A: 检查以下几点：
1. 网络连接是否正常
2. JumpServer 地址和端口是否正确
3. SSH 私钥权限是否为 0600
4. TOTP seed 是否正确
5. 系统时钟是否与 TOTP 服务器同步

### Q: 如何自定义输出颜色？

A: 当前版本使用内置颜色方案。如需禁用颜色，可设置环境变量：
```bash
NO_COLOR=1 beelog --group web
```

## 运行测试

```bash
go test ./...
```

## 项目结构

```
cmd/                    # CLI 入口（含 config / install-completion 子命令）
internal/
  config/               # 配置管理（YAML 加载、验证、默认值、脱敏、序列化）
  totp/                 # TOTP 验证码生成
  ssh/                  # SSH 连接管理（JumpServer 菜单交互、重试、保活、PTY 输出解析）
  executor/             # 并发执行引擎
  output/               # 输出聚合（分组、合并、流式、彩色、耗时汇总）
  saver/                # 结果保存（text/structured/json/csv 格式）
  shell/                # 交互式 shell（REPL、历史、补全、会话命令、本地管道）
examples/
  config.yaml           # 示例配置文件
```
