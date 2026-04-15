# chatgpt2codex

`chatgpt2codex` 是一个本地 Go CLI，用来把一组面向代码助手的工具能力暴露给外部调用方。

当前仓库通过 GitHub Release 直接分发预编译 CLI，不再发布 npm 包。

## 安装

### macOS / Linux 一键安装

默认安装到 `~/.local/bin`：

```bash
curl -fsSL https://raw.githubusercontent.com/icattlecoder/chatgpt2codex/main/install.sh | sh
```

安装到自定义目录：

```bash
curl -fsSL https://raw.githubusercontent.com/icattlecoder/chatgpt2codex/main/install.sh | sh -s -- -b /usr/local/bin
```

安装指定版本：

```bash
curl -fsSL https://raw.githubusercontent.com/icattlecoder/chatgpt2codex/main/install.sh | sh -s -- -v v0.1.0
```

### Windows PowerShell 一键安装

默认安装到 `$env:LOCALAPPDATA\Programs\chatgpt2codex\bin`：

```powershell
irm https://raw.githubusercontent.com/icattlecoder/chatgpt2codex/main/install.ps1 | iex
```

安装指定版本或目录：

```powershell
$env:CHATGPT2CODEX_VERSION = 'v0.1.0'
$env:CHATGPT2CODEX_INSTALL_DIR = 'C:\Tools\chatgpt2codex\bin'
irm https://raw.githubusercontent.com/icattlecoder/chatgpt2codex/main/install.ps1 | iex
```

### 通过 Go 安装

```bash
go install github.com/icattlecoder/chatgpt2codex/cmd/chatgpt2codex@latest
```

### 手动下载安装

GitHub Release 提供以下平台的归档包与校验文件：

- macOS `amd64`
- macOS `arm64`
- Linux `amd64`
- Linux `arm64`
- Windows `amd64`
- Windows `arm64`

发布产物包含：

- 平台归档包（macOS / Linux 为 `.tar.gz`，Windows 为 `.zip`）
- `chatgpt2codex_checksums.txt`

## 本地开发

```bash
make build
make run
make test
make release
make install
```

如果你更喜欢直接调用底层命令：

```bash
go test ./...
go build ./cmd/chatgpt2codex
```

查看全部 Makefile 命令：

```bash
make help
```

## 使用方式

```bash
cd /path/to/project
chatgpt2codex
chatgpt2codex --model "GPT-5.4 Thinking"
```

程序启动时会直接以当前工作目录作为唯一工作区，启动本地 HTTP 服务，默认建立 cloudflare 公网地址，为本次进程随机生成 API Key，并按当前工作区自动创建或更新 chatgpt.com 上的 GPT。

程序默认启用内嵌的 cloudflare Quick Tunnel，用户不需要额外安装 `cloudflared`，也不再需要手工传 `--proxy` 或 `--workspace`。

执行 `chatgpt2codex` 时，程序会读取 `~/.chatgpt2codex/config.json`。如果当前工作区没有 GPT 记录，则会启动可见的 Chrome 浏览器，打开 `https://chatgpt.com/gpts/editor`，自动填写 GPT 名称、提示词、推荐模型、Action 的 OpenAPI 架构，并把 Action 身份验证设置为 Bearer API Key；如果已有 GPT 记录，则会自动更新这些配置。成功后会把 `gpt_id` 写回或复用配置文件中的记录。

## GitHub Actions

仓库包含两个 workflow：

- `.github/workflows/ci.yml`
  - 在普通 `push` 和 `pull_request` 上执行
  - 运行 `go test ./...`
  - 构建 CLI
  - 校验安装脚本语法
  - 构建 GitHub Release 归档并检查产物是否齐全
- `.github/workflows/release.yml`
  - 在推送 `v*` 标签时执行
  - 运行 `go test ./...`
  - 交叉编译六个平台的 CLI 二进制并打包归档
  - 生成 GitHub Release 并上传归档与 `chatgpt2codex_checksums.txt`

## 发布流程

推送语义化版本标签，例如：

```bash
git tag v0.1.0
git push origin v0.1.0
```

发布 workflow 会自动执行测试、构建以下 Release 产物并创建 GitHub Release：

- `chatgpt2codex_darwin_amd64.tar.gz`
- `chatgpt2codex_darwin_arm64.tar.gz`
- `chatgpt2codex_linux_amd64.tar.gz`
- `chatgpt2codex_linux_arm64.tar.gz`
- `chatgpt2codex_windows_amd64.zip`
- `chatgpt2codex_windows_arm64.zip`
- `chatgpt2codex_checksums.txt`

最终用户可以直接通过安装脚本一键安装最新版本，或通过 `-v vX.Y.Z` / `CHATGPT2CODEX_VERSION` 安装指定版本。
