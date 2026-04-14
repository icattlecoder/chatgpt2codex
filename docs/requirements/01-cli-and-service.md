# 功能点：CLI 与服务接口

## 目标
为代码助手提供一个可本地运行的统一入口，启动 HTTP 服务、建立 cloudflare 公网访问地址，并按工作区自动创建或维护 chatgpt.com 上的 GPT。

## CLI 要求
### 1. 根命令
- 可执行文件名为 `chatgpt2codex`。
- 无子命令时输出帮助信息，而不是直接报错。
- 默认工作目录取当前进程所在目录。

### 2. `serve` 命令
- 启动本地 HTTP 服务，默认工作区为当前目录。
- 支持 `--workspace` 指定工作区。
- 兼容隐藏的历史参数 `--worksapce`，用于平滑迁移旧调用方。
- 支持 `--model` 指定 GPT 推荐模型；默认值为 `GPT-5.4 Thinking`。
- `serve` 启动时必须默认建立 cloudflare Quick Tunnel，不再要求调用方提供 `--proxy`。
- 启动后输出本地监听地址、公网地址与 `/api.yaml` 地址。
- 在服务与公网地址准备完成后，读取 `~/.chatgpt2codex/config.json` 并按工作区执行 GPT 创建或更新流程。
- 收到 `SIGINT` / `SIGTERM` 时应优雅关闭服务与代理。

### 3. `tools` 命令
- 输出内嵌的 OpenAPI 规范文本。
- 输出内容应与服务端 `/api.yaml` 的基础规范一致。

### 4. `prompt` 命令
- 根据工作区生成系统提示词。
- 支持 `--workspace` 与隐藏兼容参数 `--worksapce`。

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
- `GET /api.yaml`

### 3. 工作区处理
- 服务启动时存在默认工作区。
- 单次请求可通过 `X-Workspace` 头覆盖默认工作区。
- 服务端必须校验工作区存在且为目录。
- GPT 映射使用服务启动时解析出的绝对工作区路径。
- GPT 名称中的 `<workspace>` 使用绝对工作区路径的目录名。

### 4. 错误模型
- 参数错误返回 `400`。
- 执行错误返回 `500`。
- 返回体统一使用工具响应结构，至少包含 `content` 字段。

## API 规范输出要求
- `/api.yaml` 返回 YAML，`Content-Type` 为 `application/yaml; charset=utf-8`。
- 代理成功后，规范中的 `servers.url` 必须替换为 cloudflare 公网地址。
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
- 若当前工作区已有 GPT 记录，则进入 GPT 更新分支。
- GPT 更新分支本期不实现，但需要保留明确的代码入口。
