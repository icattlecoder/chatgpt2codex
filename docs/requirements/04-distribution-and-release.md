# 功能点：构建、分发与发布

## 1. 本地构建与校验
- 需要提供 `make` 命令用于 `fmt`、`test`、`build`、`run`、`install`、`release`、`clean`、`verify`。
- `verify` 需要串联常用校验：格式化、测试与本地构建。
- Go 构建默认关闭 CGO，输出本地 CLI 二进制。

## 2. GitHub Release 产物
- 需要支持 6 个目标平台：
  - macOS amd64
  - macOS arm64
  - Linux amd64
  - Linux arm64
  - Windows amd64
  - Windows arm64
- 发布脚本需要批量生成对应平台归档，并输出 `chatgpt2codex_checksums.txt`。
- macOS / Linux 目标产物使用 `.tar.gz`，Windows 目标产物使用 `.zip`。
- Windows 归档内的可执行文件需要保留 `.exe` 扩展名。

## 3. 一键安装脚本
- 仓库根目录需要提供 `install.sh` 与 `install.ps1`。
- 安装脚本需要根据当前平台和架构下载对应的 GitHub Release 归档。
- 安装脚本需要校验 `chatgpt2codex_checksums.txt` 后再落盘可执行文件。
- 安装脚本默认安装最新版本，同时支持指定安装目录与版本。
- 不支持的平台必须明确报错，并给出失败原因。

## 4. 发布流程约束
- 发布流程由 GitHub Actions 在推送 `v*` 标签时触发。
- 发布前必须执行 `go test ./...`。
- 发布流程需要创建 GitHub Release，并上传全部平台归档和校验文件。

## 5. 对外文档要求
- `README.md` 需要说明一键安装、Go 安装、本地开发、默认启动方式与发布流程。
- OpenAPI 规范源文件放在 `internal/docsasset/api/tools.api.yaml`，供程序在启动时内嵌并生成 GPT Action 架构文本。
