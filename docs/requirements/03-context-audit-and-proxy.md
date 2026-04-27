# 功能点：运行时上下文、审计、代理与 GPT 自动化

## 1. 运行时上下文 `get_runtime_context`
### 目标
为代码助手提供当前仓库的额外约束信息，包括仓库级说明和可用技能清单。

### 输入输出要求
- 请求体必须包含 `cwd`。
- `cwd` 需要基于当前服务工作区解析。
- 返回值为 `system_instruct` 字符串，格式为完整的 `SystemInstruct` 块。

### 发现规则
- 从 `cwd` 向上查找最近的 `AGENTS.md` 文件。
- 从 `cwd` 向上查找最近的 `.agents/skills` 目录。
- 若存在 `AGENTS.md`，则读取其全文并嵌入结果。
- 若存在技能目录，则递归搜集所有 `SKILL.md`。
- 每个技能需要输出名称、简要描述和文档路径。
- 技能描述优先提取第一条有效自然语言描述，忽略标题、空行和代码块。

## 2. 系统提示词
- 系统提示词需要列出当前可见工具及其用途。
- 工具描述来源于 `internal/tool/definitions.go`。
- 需要汇总并去重工具使用准则。
- 提示词中需要包含当前日期与当前工作目录。
- GPT 创建流程中的“GPT 指令”直接使用内部 `prompt.Build(...)` 结果。

## 3. 审计日志与调用打印
- 所有 HTTP 访问都应写入审计日志，包括正常请求、鉴权失败、404 与其他未命中路由。
- 会话号优先取请求头 `Openai-Conversation-Id`；未提供时自动生成随机会话号。
- 日志目录默认位于 `~/.chatgpt2codex/年/月/日/`。
- 每个会话写入单独的 `.jsonl` 文件。
- 每条日志至少包含时间戳、会话号、工具名、工作区、请求快照与响应快照。
- 请求快照必须记录 request body；响应快照必须记录状态码与响应 body。
- 请求头中的 `Authorization` 必须按明文写入访问日志，不做 `[REDACTED]` 脱敏替换。
- 写日志过程需要按文件加锁，避免并发写入冲突。
- 每次工具请求完成后，需要向运行终端打印一条调用摘要，包含工具名、HTTP 状态码、耗时、工作区与请求关键信息。
- 调用摘要需要按工具选择合适字段：`read` 输出路径和分页参数，`bash` 输出截断后的命令与超时，`edit` 输出路径和替换数量，`write` 输出路径和写入字节数，`grep` 输出匹配条件，`find` 输出匹配模式，`ls` 输出目录和数量限制，`get_runtime_context` 输出 cwd。
- 调用摘要不得打印 `write.content`、`edit.oldText`、`edit.newText` 等大块正文内容。

## 4. 代理能力
### 1. Quick Tunnel 模式
- `cloudflare.enableDomain` 为 `false` 时，程序启动必须默认启动 cloudflare Quick Tunnel。
- `cloudflare` 通过直接引用 `cloudflared` 源码，在当前 CLI 进程内启动 Quick Tunnel，不要求用户额外安装 `cloudflared`。
- 启动阶段需要先向 TryCloudflare 服务申请临时公网地址，再以内嵌 cloudflare 隧道逻辑建立连接。
- 若 20 秒内未拿到公网地址，应判定启动失败。

### 2. Named Tunnel 模式
- `cloudflare.enableDomain` 为 `true` 时，程序启动必须使用 cloudflare Named Tunnel。
- 当前工作区 hostname 使用工作区持久化的 `host`；缺失时按默认规则生成。
- Named Tunnel 持久化配置只包含 `cloudflare.domain`、`cloudflare.api_token` 与 `cloudflare.enableDomain`。
- 启动前必须通过 Cloudflare API Token 自动获取 Zone ID、Account ID、Named Tunnel ID 与当前 Tunnel Token。
- 默认使用名称为 `chatgpt2codex` 的未删除 Named Tunnel。
- 启动前必须为当前 hostname 调用 Cloudflare API 创建或校验 DNS route。
- 启动前必须为当前 tunnel 下发远端 ingress 配置，把当前 hostname 指向本地监听地址，并追加 `http_status:404` 兜底规则。

### 3. 通用代理要求
- 服务需要在启动日志中提取最近输出片段，便于排查失败原因。
- `cloudflare` 需要等待“隧道已就绪”信号后才算启动成功。
- 在输出公网地址并返回启动成功前，程序还必须确认该公网地址已不再返回 Cloudflare 5xx。
- 代理启动失败时，需要返回最近输出片段。
- 服务结束或上下文取消时，需要关闭内嵌隧道会话。

## 5. OpenAPI 架构联动
- 一旦代理成功，程序输出中应包含公网访问地址。
- 临时域名模式下，每次启动都生成新的随机 API Key，并输出到终端。
- 已启用自定义域名时，输出并复用工作区持久化的 API Key。
- GPT 创建与更新流程中的 OpenAPI 架构使用内嵌规范文本，并把 `servers.url` 替换为当前公网地址。
- 规范文本不再通过 `/api.yaml` 接口对外提供。
- 规范仍需声明 Bearer 鉴权。

## 6. GPT 自动化创建
### 目标
- 通过自动化操作 Chrome 浏览器，在 `https://chatgpt.com/gpts/editor` 页面创建 GPT。

### 浏览器要求
- 程序需要启动可见的 Chrome 浏览器实例，而不是无头模式。
- 浏览器用户数据目录应可复用，以便保留登录态。
- GPT 自动化默认将浏览器界面语言固定为英文，并按英文页面元素定义执行定位与点击。
- 若用户未登录导致跳转离开 GPT 编辑页，程序必须检测并等待用户完成登录后再继续。

### 创建与更新流程
- 打开 GPT 编辑页。
- 同步 GPT 名称、GPT 指令、推荐模型、OpenAPI 架构与 Action Bearer API Key。
- Action 身份验证必须配置为 API Key，身份验证类型为 Bearer。
- 当工作区配置不存在或 `gpt_id` 为空时，必须走创建流程。
- 仅当成功拿到有效 `gpt_id` 后，才允许更新 `~/.chatgpt2codex/config.json`。
- 浏览器自动化失败时，不得写入空或无效的 `gpt_id`。

## 7. GPT 默认同步策略
- 临时域名模式下，默认启动执行 GPT 创建或更新。
- 已启用自定义域名且当前工作区已有有效 `gpt_id` 时，默认启动跳过 GPT 自动化。
- 已启用自定义域名但当前工作区不存在配置项，或配置项中没有有效 `gpt_id` 时，默认启动执行 GPT 创建。
- 传入 `--reset` 时，必须执行 GPT 创建或更新，并刷新 Action Bearer API Key。
