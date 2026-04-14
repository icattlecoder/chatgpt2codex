# 功能点：CLI 与服务接口

## 目标
为代码助手提供一个可本地运行的统一入口，既能以 CLI 方式输出提示词和 API 规范，也能启动 HTTP 服务供外部调用。

## CLI 要求
### 1. 根命令
- 可执行文件名为 `chatgpt2codex`。
- 无子命令时输出帮助信息，而不是直接报错。
- 默认工作目录取当前进程所在目录。

### 2. `serve` 命令
- 启动本地 HTTP 服务，默认工作区为当前目录。
- 支持 `--workspace` 指定工作区。
- 兼容隐藏的历史参数 `--worksapce`，用于平滑迁移旧调用方。
- 支持 `--proxy cloudflare` 暴露公网地址。
- cloudflare 代理能力直接内嵌到 CLI 中，调用方不需要额外安装 `cloudflared`。
- 启动后输出本地监听地址；启用代理时额外输出公网地址和 `/api.yaml` 地址。
- 收到 `SIGINT` / `SIGTERM` 时应优雅关闭服务。

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

### 4. 错误模型
- 参数错误返回 `400`。
- 执行错误返回 `500`。
- 返回体统一使用工具响应结构，至少包含 `content` 字段。

## API 规范输出要求
- `/api.yaml` 返回 YAML，`Content-Type` 为 `application/yaml; charset=utf-8`。
- 若配置了公网地址，则将规范中的默认本地地址替换为公网地址。
- 若未显式配置公网地址，则优先从 `Forwarded`、`X-Forwarded-Proto`、`X-Forwarded-Host` 推断外部访问地址。
- 当无法推断外部地址时，保留默认本地地址。
