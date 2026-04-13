# chatgpt2codex

`chatgpt2codex` 是一个本地 Go CLI，用来把一组面向代码助手的工具能力暴露给外部调用方。

当前仓库已经补齐了两条发布链路：

- Go 源码可以直接本地构建运行。
- npm 包 `chatgpt2codex` 负责分发 CLI，安装时会自动下载当前平台对应的预编译二进制。

## 安装

### 通过 npm 安装

```bash
npm install -g chatgpt2codex
```

支持的平台：

- macOS `x64`
- macOS `arm64`
- Linux `x64`
- Linux `arm64`
- Windows `x64`
- Windows `arm64`

### 通过 Go 安装

```bash
go install github.com/icattlecoder/chatgpt2codex/cmd/chatgpt2codex@latest
```

## 本地开发

```bash
go test ./...
go build ./cmd/chatgpt2codex
```

如果要验证 npm 包装层：

```bash
node scripts/install.js
npm pack --dry-run
```

开发版本的 `package.json` 使用占位版本号，`postinstall` 会自动跳过二进制下载；真正发布时由 GitHub Actions 按 tag 覆盖为正式版本。

## 使用方式

```bash
chatgpt2codex serve --workspace /path/to/project
chatgpt2codex tools
chatgpt2codex prompt
```

主要命令：

- `serve`：启动本地 HTTP 服务，暴露工具 API。
- `tools`：输出 `docs/tools.api.yaml`。
- `prompt`：输出系统提示词内容。

`serve` 命令也支持 `--proxy ngrok` 或 `--proxy cloudflare`。

## GitHub Actions

仓库包含两个 workflow：

- `.github/workflows/ci.yml`
  - 在普通 `push` 和 `pull_request` 上执行
  - 运行 `go test ./...`
  - 构建 CLI
  - 校验 npm 包装脚本语法
  - 执行 `npm pack --dry-run`
- `.github/workflows/release.yml`
  - 在推送 `v*` 标签时执行
  - 交叉编译六个平台的 CLI 二进制
  - 生成 GitHub Release 并上传二进制与 `SHA256SUMS`
  - 使用 `npm publish --provenance --access public` 发布 npm 包

## 发布流程

1. 在 npm 上创建一个 Automation Token。
2. 在 GitHub 仓库里添加 Actions Secret：`NPM_TOKEN`。
3. 推送语义化版本标签，例如：

```bash
git tag v0.1.0
git push origin v0.1.0
```

发布 workflow 会自动把 tag `v0.1.0` 转成 npm 版本 `0.1.0`，上传对应平台的二进制到 GitHub Release，然后再发布 npm 包。

安装 npm 包时，`postinstall` 会从对应版本的 GitHub Release 下载二进制文件。
