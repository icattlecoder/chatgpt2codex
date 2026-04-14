# 功能点：构建、分发与发布

## 1. 本地构建与校验
- 需要提供 `make` 命令用于 `fmt`、`test`、`build`、`run`、`install`、`release`、`package`、`prepublish`、`publish-dry-run`、`publish`、`clean`、`verify`。
- `verify` 需要串联常用校验：格式化、测试、构建与 npm 包检查。
- Go 构建默认关闭 CGO，输出本地 CLI 二进制。

## 2. 发布二进制
- 需要支持 6 个目标平台：
  - macOS x64
  - macOS arm64
  - Linux x64
  - Linux arm64
  - Windows x64
  - Windows arm64
- 发布脚本需要批量生成对应平台二进制，并输出 `SHA256SUMS`。
- Windows 目标产物需要保留 `.exe` 扩展名。

## 3. npm 包分发
- npm 包本身作为 Go CLI 的启动器，不直接内置全部平台二进制。
- 安装 npm 包时，`postinstall` 根据当前平台和架构下载对应的 GitHub Release 二进制到 `native/` 目录。
- 支持通过 `CHATGPT2CODEX_RELEASE_BASE_URL` 覆盖下载地址。
- 开发版本或设置 `CHATGPT2CODEX_SKIP_DOWNLOAD=1` 时跳过下载。
- 不支持的平台必须明确报错，并给出支持列表。

## 4. 发布约束
- `package.json` 的开发版本号禁止直接发布。
- 发布前校验脚本需要阻止带 `-development` 后缀的版本进入 npm。
- 正式版本由 release 流程基于 Git tag 生成。

## 5. 对外文档要求
- `README.md` 需要说明 Go 安装、npm 安装、本地开发、主要命令与发布流程。
- API 规范源文件放在 `internal/docsasset/api/tools.api.yaml`，通过 CLI 与 HTTP 服务对外输出。
