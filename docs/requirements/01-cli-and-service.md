# 功能点：CLI 与服务接口

## 目标
为代码助手提供一个可本地运行的统一入口，启动 HTTP 服务，按配置选择 cloudflare Quick Tunnel 或 Named Tunnel，并按工作区决定是否同步 chatgpt.com 上的 GPT。

## CLI 要求
### 1. 根命令
- 可执行文件名为 `chatgpt2codex`。
- 直接执行 `chatgpt2codex` 即启动服务。
- 工作区固定为当前进程工作目录，并在启动时解析为绝对路径。
- 支持 `--model` 指定 GPT 推荐模型；默认值为 `GPT-5.5 Thinking`。
- 支持 `--reset`，强制执行当前工作区 GPT 元数据同步。
- 收到 `SIGINT` / `SIGTERM` 时应优雅关闭服务与代理。

### 2. `domain` 子命令
- `chatgpt2codex domain` 必须进入交互式配置流程。
- `chatgpt2codex domain <domain>` 可将 `<domain>` 作为默认值带入交互式配置流程。
- `chatgpt2codex domain -` 关闭自定义域名开关，保留已保存的 Cloudflare API Token。
- 交互式流程至少录入以下字段：
  - Cloudflare 基础域名 `domain`
  - Cloudflare API Token
- 交互式流程需要允许复用已保存值；当用户直接回车时，保留现有值。
- 配置写入 `~/.chatgpt2codex/config.json`。

### 3. 启动模式
- `cloudflare.enableDomain` 为 `false` 时，启动必须默认建立 cloudflare Quick Tunnel。
- `cloudflare.enableDomain` 为 `true` 时，启动必须使用该工作区的 hostname 走 cloudflare Named Tunnel。
- 启动后输出本地监听地址、公网地址与当前工作区实际使用的 API Key。
- 同一工作区同一时刻只允许存在一个活跃服务实例；重复启动必须直接报错，而不是并发连接同一个 tunnel。
- 已启用自定义域名时，同一时刻只允许存在一个活跃 custom-domain 实例。
- 临时域名模式下，API Key 每次进程启动都重新生成，只在当前进程生命周期内有效。
- 已启用自定义域名时，优先复用工作区已持久化的 API Key；首次缺失时生成并写回配置。
- 已启用自定义域名时，Named Tunnel 所需的 Zone ID、Account ID 与 Tunnel Token 必须通过 Cloudflare API Token 自动获取，不持久化到配置文件。

## HTTP 服务要求
### 1. 监听策略
- 从 `127.0.0.1:8080` 开始监听。
- 若端口被占用，应顺序向后扫描，最多尝试 100 个端口。

### 2. 路由
- `POST /tools/read`
- `POST /tools/bash`
- `POST /tools/edit`
- `POST /tools/write`
- `POST /tools/grep`
- `POST /tools/find`
- `POST /tools/ls`
- `POST /context/runtime`

### 3. 工作区处理
- 服务启动时解析当前进程工作目录作为唯一工作区。
- 单次请求不得再通过额外参数或请求头覆盖工作区。
- 服务端必须校验该工作区存在且为目录。
- 工作区配置键使用启动时解析出的绝对工作区路径。
- GPT 名称中的 `<workspace>` 使用绝对工作区路径的目录名。
- 工作区 hostname 默认值为 `<workspace-dns-label>.<domain>`。
- `workspace-dns-label` 生成规则为：小写化、非 `[a-z0-9-]` 替换为 `-`、去除首尾 `-`、空值回退 `workspace`。
- 当全局基础域名被修改时，已持久化工作区的 `host` 默认值需要重新按新域名推导。

### 4. 错误模型
- 未提供或提供了无效 Bearer API Key 时返回 `401`。
- 参数错误返回 `400`。
- 执行错误返回 `500`。
- 返回体统一使用工具响应结构，至少包含 `content` 字段。

## OpenAPI 架构要求
- OpenAPI 规范源文件位于 `internal/docsasset/api/tools.api.yaml`。
- 该规范不再通过 CLI 子命令或 HTTP 路由直接对外暴露。
- 代理成功后，规范中的 `servers.url` 必须替换为当前公网地址，再提供给 GPT Action 配置流程。
- 规范中必须声明 Bearer 鉴权。
- 当无法推断外部地址时，保留默认本地地址。

## GPT 生命周期要求
### 1. 配置文件
- 配置文件路径为 `~/.chatgpt2codex/config.json`。
- 文件结构为：

```json
{
  "cloudflare": {
    "enableDomain": true,
    "domain": "chatgpt2codex.fun",
    "api_token": "xxx"
  },
  "gpts": {
    "<workspace>": {
      "gpt_id": "",
      "host": "",
      "api_key": ""
    }
  }
}
```

- `<workspace>` 键为绝对工作区路径。
- 旧版仅包含 `gpt_id` 的配置文件必须保持兼容。
- 旧版仅包含 `domain` 的配置文件必须保持兼容。
- 旧版 `cloudflare.zone_id` 与 `cloudflare.tunnel_token` 配置必须兼容读取，但保存时不再写回。

### 2. GPT 同步处理
- 临时域名模式下，启动后必须按当前工作区执行 GPT 创建或更新流程。
- 已启用自定义域名时，若当前工作区不存在配置项，或配置项中没有有效 `gpt_id`，启动后也必须执行 GPT 创建流程。
- 已启用自定义域名且当前工作区已有有效 `gpt_id` 时，默认启动只拉起服务和 tunnel，不自动进入 GPT 编辑流程。
- 已启用自定义域名且传入 `--reset` 时，必须执行 GPT 创建或更新流程。
- GPT 创建与更新时，需要同步刷新名称、指令、模型、OpenAPI 架构与 Action Bearer API Key。
- 当工作区配置不存在或 `gpt_id` 为空时，必须走 GPT 创建逻辑，不得尝试更新。
- GPT 保存成功后，只持久化 `g-<gpt_id>` 作为工作区 GPT 标识。
