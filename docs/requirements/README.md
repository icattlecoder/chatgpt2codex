# chatgpt2codex 需求文档总览

本文档基于当前代码实现逆向整理，按功能点拆分，便于维护 CLI、HTTP 服务、工具能力与发布链路。

## 功能分组
- `01-cli-and-service.md`：描述命令行入口、HTTP 服务、接口路由与 API 规范输出。
- `02-tool-capabilities.md`：描述读写文件、精确编辑、命令执行、搜索与目录浏览能力。
- `03-context-audit-and-proxy.md`：描述运行时上下文拼装、审计日志、代理与公网访问能力。
- `04-distribution-and-release.md`：描述构建、GitHub Release 二进制分发与发布约束。

## 适用范围
- 面向本地代码助手集成场景。
- 面向 Go CLI 与 GitHub Release 二进制分发场景。
- 面向仓库级指令、技能发现与审计留痕场景。
