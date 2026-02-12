# beelog

通过 JumpServer 跳板机同时连接多个目标节点的交互式批量命令工具。

## 功能

- 通过 JumpServer SSH 连接，自动完成 TOTP 二次验证
- 交互式 shell，支持命令历史（上下键）和远程 Tab 文件路径补全
- 并发连接多节点，命令一次输入、所有节点同时执行
- 三种输出模式：按节点分组 / 按时间戳合并 / 实时流式
- 彩色输出区分不同节点
- 会话管理命令（`:quit`、`:disconnect`、`:help`）
- 连接保活，防止 JumpServer 空闲超时断开

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

defaults:
  output_mode: "stream"
  concurrency: 5
  timeout: 60
  max_retries: 3
  retry_delay: 5
  keepalive_interval: 300
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
| `--group` | `-g` | （必须） | 节点分组名称 |
| `--mode` | `-m` | 配置文件值 | 输出模式：`grouped` / `merged` / `stream` |
| `--verbose` | `-v` | `false` | 详细输出 |
| `--debug` | | `false` | 调试模式 |
| `--timeout` | | 配置文件值 | 命令超时秒数 |
| `--concurrency` | | 配置文件值 | 最大并发数 |

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

### 快捷操作

| 操作 | 说明 |
|------|------|
| `↑` / `↓` | 浏览历史命令 |
| `Tab` | 远程文件路径补全 |
| `Ctrl+C` | 终止当前命令（不退出 shell） |

### 会话管理命令

| 命令 | 说明 |
|------|------|
| `:quit` / `:exit` | 断开所有连接并退出 |
| `:disconnect <node>` | 断开指定节点 |
| `:help` | 显示帮助信息 |

```
beelog [web:3]> :disconnect web-3
已断开节点: web-3

beelog [web:2]> :quit
```

### 输出模式

**stream**（默认）— 实时流式，每行带节点前缀：
```
[web-1] log line 1
[web-2] log line 1
[web-1] log line 2
```

**grouped** — 按节点分组展示：
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
[2024-01-15 10:30:00] [web-1] log line 1
[2024-01-15 10:30:00] [web-2] log line 1
[2024-01-15 10:30:01] [web-1] log line 2
```

## 运行测试

```bash
go test ./...
```

## 项目结构

```
cmd/                    # CLI 入口
internal/
  config/               # 配置管理（YAML 加载、验证、默认值）
  totp/                 # TOTP 验证码生成
  ssh/                  # SSH 连接管理（JumpServer 菜单交互、重试、保活）
  executor/             # 并发执行引擎
  output/               # 输出聚合（分组、合并、流式、彩色）
  shell/                # 交互式 shell（REPL、历史、补全、会话命令）
examples/
  config.yaml           # 示例配置文件
```
