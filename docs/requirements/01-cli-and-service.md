# 功能点：CLI 与服务接口

## 目标
为代码助手提供一个可本地运行的统一入口，启动 HTTP 服务、建立公网访问地址，并按当前运行目录自动创建或维护 chatgpt.com 上的 GPT。

## CLI 要求
### 1. 根命令
- 可执行文件名为 `chatgpt2codex`。
- 直接执行 `chatgpt2codex` 即启动服务，不再保留 `serve`、`api`、`prompt` 等子命令。
- 工作区固定为当前进程工作目录，并在启动时解析为绝对路径。
- 支持 `--model` 指定 GPT 推荐模型；默认值为 `GPT-5.4 Thinking`。
- 支持 `--proxy` 指定公网代理，允许值为 `cloudflare` 与 `ngrok`。
- `--proxy` 默认值为 `cloudflare`。
- 当 `--proxy=ngrok` 时，程序必须使用内嵌 ngrok Go SDK 建立公网地址，不依赖外部 `ngrok` 可执行文件。
- 当 `--proxy=ngrok` 时，调用方必须通过环境变量 `NGROK_AUTHTOKEN` 提供 ngrok 认证令牌。
- 启动后输出本地监听地址、公网地址与本次启动随机生成的 API Key。
- API Key 每次进程启动时重新生成，只在当前进程生命周期内有效。
- 在服务与公网地址准备完成后，读取 `~/.chatgpt2codex/config.json` 并按当前工作区执行 GPT 创建或更新流程，同时把本次启动的 API Key 同步到 GPT Action 的身份验证配置中。
- 收到 `SIGINT` / `SIGTERM` 时应优雅关闭服务与代理。

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
- GPT 映射使用启动时解析出的绝对工作区路径。
- GPT 名称中的 `<workspace>` 使用绝对工作区路径的目录名。

### 4. 错误模型
- 未提供或提供了无效 Bearer API Key 时返回 `401`。
- 参数错误返回 `400`。
- 执行错误返回 `500`。
- 返回体统一使用工具响应结构，至少包含 `content` 字段。

## OpenAPI 架构要求
- OpenAPI 规范源文件位于 `internal/docsasset/api/tools.api.yaml`。
- 该规范不再通过 CLI 子命令或 HTTP 路由直接对外暴露。
- 代理成功后，规范中的 `servers.url` 必须替换为当前代理返回的公网地址，再提供给 GPT Action 配置流程。
- 规范中必须声明 Bearer 鉴权，供 GPT Action 导入后与运行时接口要求保持一致。
- 当无法推断外部地址时，保留默认本地地址。

## GPT 生命周期要求
### 1. 配置文件
- 配置文件路径为 `~/.chatgpt2codex/config.json`。
- 文件结构为：

```json
{
  "gpts": {
    "<workspace>": {
      "gpt_id": ""
    }
  }
}
```

- `<workspace>` 键为绝对工作区路径。
- 当配置文件不存在时，程序应按空配置处理，并在首次成功创建 GPT 后落盘。

### 2. 工作区映射处理
- 若当前工作区没有 GPT 记录，则执行 GPT 创建流程。
- GPT 创建流程进入编辑器后，先切换到“配置”页，再填写名称、指令、模型、Action 架构与 Action 身份验证。
- Action 身份验证必须配置为 API 密钥，并把身份验证类型设置为 Bearer，密钥值使用当前进程启动时随机生成的 API Key。
- GPT 保存成功后，应优先从成功提示中展示的 GPT 链接元素提取地址；该地址可能包含 `https://chatgpt.com/g/g-<gpt_id>-<slug>`，程序只保留其中的 `g-<gpt_id>` 作为持久化标识。
- GPT 创建成功后，使用系统浏览器打开 `https://chatgpt.com/g/<gpt_id>`，不通过内置自动化上下文再次跳转。
- 浏览器自动化过程中已知的 CDP 事件反序列化噪音日志不应输出到用户终端。
- 若当前工作区已有 GPT 记录，则执行 GPT 更新流程。
- GPT 更新流程需要同步刷新名称、指令、模型、Action 架构与 Action Bearer API Key，确保其与当前启动实例保持一致。
