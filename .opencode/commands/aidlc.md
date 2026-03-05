---
description: AIDLC 全流程开发 - AI驱动的开发生命周期（需求分析 → 架构设计 → 代码实现 → 构建测试）
allowed-tools: Bash, Read, Write, Edit, MultiEdit, Glob, Grep
argument-hint: <功能需求描述>
---

# AIDLC - AI-Driven Development Life Cycle

<background_information>
AIDLC 是一个结构化的自适应软件开发工作流，包含三个阶段：
- **INCEPTION**（规划）：确定做什么、为什么做（需求分析、架构设计、任务拆解）
- **CONSTRUCTION**（实施）：确定怎么做（详细设计、代码生成、构建测试）
- **OPERATIONS**（运维）：未来扩展占位

**与 cc-sdd 的关系**：
- cc-sdd（`/kiro:spec-*` 命令）：轻量级规范驱动开发，手动逐步执行，适合中小需求
- AIDLC（`/aidlc` 命令）：完整的自动流转生命周期，自适应深度，适合中大型需求
- 两者独立共存，根据需求复杂度选用

**当前项目上下文**：
- 项目名：beelog
- 类型：brownfield（已有代码库）
- 语言：Go
- 构建命令：
  - go build ./...
  - go test ./...
  - go vet ./...
</background_information>

<instructions>

## 启动流程

1. **加载核心工作流**：读取 `.aidlc/core-workflow.md`，它是整个流程的编排指南。严格按照其定义的阶段顺序和门禁规则执行。

2. **检查会话状态**：
   - 检查 `aidlc-docs/aidlc-state.md` 是否存在
   - **如果存在**：按 `.aidlc/rules/common/session-continuity.md` 规则恢复，加载之前的产物上下文，展示进度摘要并询问用户下一步
   - **如果不存在**：开始新流程，显示欢迎信息（加载 `.aidlc/rules/common/welcome-message.md`）

3. **获取需求描述**：
   - 如果 `$ARGUMENTS` 非空，使用它作为初始需求描述
   - 如果为空，询问用户描述需要开发的功能

4. **遵循核心工作流**：严格按照 `core-workflow.md` 的阶段和门禁执行，加载各阶段规则文件

## 关键适配规则

### 对话式问答
- 所有问题**直接在对话中提问**，使用编号多选格式
- 用户直接在对话中回答（如"1:A, 2:C"或自然语言）
- 每个问题包含有意义的选项 + "其他"作为末尾选项
- 保留矛盾/歧义检测逻辑，通过对话追问解决
- 所有问答记录在 `aidlc-docs/audit.md` 中

### 审批门禁
- 每个阶段完成后展示摘要，询问用户"继续"或"需要修改"
- 用户在对话中确认后才进入下一阶段
- **不得跳过审批门禁**

### beelog 项目上下文
- 始终识别为 brownfield 项目（已有代码库）
- Infrastructure Design 阶段通常跳过
- 构建命令：`go build ./...`、`go test ./...`、`go vet ./...`
- 代码遵循项目编码规范（参考 `coding-rules` skill）
- 单元测试遵循 `unit-test` skill

### 文件操作
- 规则文件位于 `.aidlc/rules/` 目录
- 工作产物输出到 `aidlc-docs/` 目录（按阶段组织子目录）
- 代码修改在项目根目录（绝不放入 aidlc-docs/）
- 使用 `Read` 工具加载规则文件
- 使用 `Write`/`Edit` 工具创建/更新产物文件
- 时间戳获取：使用 `Bash` 执行 `date -u +%Y-%m-%dT%H:%M:%SZ`

### 产物输出
- AIDLC 流程完成后，在 `docs_proj/` 目录下输出技术文档总结（按模块/功能子目录组织）
- `docs/` 仅放全局/跨项目公共文档
- 包含实现架构分析、技术方案、修改内容等

</instructions>
