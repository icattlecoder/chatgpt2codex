# chatgpt2codex

## 项目定位
- 本项目提供本地 Go CLI 与 HTTP 服务，把文件、搜索、命令执行与仓库上下文能力暴露给代码助手。
- CLI 入口在 `cmd/chatgpt2codex/main.go`，命令编排在 `internal/app/`，HTTP 服务在 `internal/server/`。

## 关键目录
- `internal/tool/`：工具实现、输入输出结构、路径处理、截断与 `.gitignore` 规则。
- `internal/runtimecontext/`：向上查找 `AGENTS.md` 与 `.agents/skills`，生成 `SystemInstruct`。
- `internal/prompt/`：拼装给代码助手使用的系统提示词。
- `internal/audit/`：按会话写入 JSONL 审计日志。
- `internal/proxy/`：封装 `ngrok` / `cloudflared` 代理启动。
- `internal/docsasset/`：内嵌 API 规范资源。
- `docs/requirements/`：按功能点整理的逆向需求文档。

## 修改约定
- 新增或调整工具时，同时检查 `internal/tool/definitions.go`、`internal/server/server.go`、`internal/docsasset/api/tools.api.yaml` 与相关测试。
- 调整提示词、仓库说明或技能发现逻辑时，同时检查 `internal/prompt/` 与 `internal/runtimecontext/`。
- 提交前至少运行 `go test ./...`；涉及发版链路时，再执行 `make release` 检查 GitHub Release 产物。

## 文档索引
- `README.md`：安装、使用方式、发布流程。
- `docs/requirements/README.md`：需求文档总览。
- `docs/requirements/01-cli-and-service.md`：CLI 命令、HTTP 服务与 API 暴露。
- `docs/requirements/02-tool-capabilities.md`：文件、搜索、命令执行类工具能力。
- `docs/requirements/03-context-audit-and-proxy.md`：运行时上下文、审计日志、代理能力。
- `docs/requirements/04-distribution-and-release.md`：构建、GitHub Release 分发与发布要求。
- `internal/docsasset/api/tools.api.yaml`：内嵌 OpenAPI 规范源文件。

## 工作规范
- 文档先行，再落代码，文档简洁，不罗嗦，无旁边描述，不写设计的调整过程，遇到调整设计的，删除原相关，直接重写最新设计。