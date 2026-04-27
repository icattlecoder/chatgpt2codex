# chatgpt2codex

[English](README.md)

`chatgpt2codex` 将您的 ChatGPT 转化为本地 Coding Agent，甚至作为通用 Agent 使用，尤其适用于 Codex 配额耗尽后的过渡期。它将 ChatGPT 接入本地项目环境，使开发者在该阶段仍可基于仓库上下文、文件能力与命令执行能力继续完成开发任务。

考虑到 ChatGPT 侧的上下文容量会直接影响长程任务连续性，项目更建议具备 128k 上下文窗口的 ChatGPT Pro 5x 或 20x 用户使用，以获得更稳定的协作体验。

## 安装

### macOS / Linux

默认安装到 `~/.local/bin`：

```bash
curl -fsSL https://raw.githubusercontent.com/icattlecoder/chatgpt2codex/main/install.sh | sh
```

安装到指定目录：

```bash
curl -fsSL https://raw.githubusercontent.com/icattlecoder/chatgpt2codex/main/install.sh | sh -s -- -b /usr/local/bin
```

## 快速开始

1. 在项目目录中启动 CLI：

```bash
cd /path/to/project
chatgpt2codex
```

2. 首次启动时，按提示登录 ChatGPT 账号。
3. 程序会自动创建可用的 GPT。
4. 在 ChatGPT 中打开该 GPT 并直接发起任务。

## 使用自定义域名

你可也可使用自定义域名，通过 Claudeflare 的 Tunnel 接入 ChatGPT.
相比随机域名，自定义域名可以快速启动 GPT.


### 2. 前置条件

1. 域名已接入 Cloudflare。
2. 已创建名为 `chatgpt2codex` 的 Cloudflare Named Tunnel。
3. 已拿到可读取 Zone、读取/更新 Tunnel、创建或更新 DNS 路由的 Cloudflare API Token。

### 3. 操作步骤

1. 在项目目录执行交互式配置：

```bash
chatgpt2codex domain
```

2. 按提示输入或确认以下配置：
- 基础域名
- Cloudflare API Token

3. 配置保存后，在项目根目录直接启动：

```bash
chatgpt2codex
```

如需重新同步 GPT 名称、指令、Action 和 Bearer Key，执行：

```bash
chatgpt2codex --reset
```

### 4. 移除自定义域名

执行：

```bash
chatgpt2codex domain -
```

执行后会将 `cloudflare.enableDomain` 设置为 `false`，保留已保存的 Cloudflare API Token，并改用随机域名。
