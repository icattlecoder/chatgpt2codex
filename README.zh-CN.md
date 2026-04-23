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

如需改用 ngrok 而不是默认的 Cloudflare 隧道，先提供认证令牌，再指定代理类型：

```bash
NGROK_AUTHTOKEN=your_token chatgpt2codex --proxy ngrok
```
