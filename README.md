# chatgpt2codex

[中文说明](README.zh-CN.md)

`chatgpt2codex` turns ChatGPT into a local coding agent, and can also be used as a general-purpose agent. It is especially useful as a bridge when Codex quota is exhausted. The project connects ChatGPT to your local project environment so development can continue with repository context, file access, and command execution.

Because ChatGPT-side context capacity directly affects continuity on long-running tasks, the project is better suited to ChatGPT Pro 5x or 20x users with a 128k context window for a more stable collaboration experience.

## Installation

### macOS / Linux

Installs to `~/.local/bin` by default:

```bash
curl -fsSL https://raw.githubusercontent.com/icattlecoder/chatgpt2codex/main/install.sh | sh
```

Install to a specific directory:

```bash
curl -fsSL https://raw.githubusercontent.com/icattlecoder/chatgpt2codex/main/install.sh | sh -s -- -b /usr/local/bin
```

## Quick Start

1. Start the CLI inside your project directory:

```bash
cd /path/to/project
chatgpt2codex
```

2. On first launch, sign in to your ChatGPT account when prompted.
3. The program automatically creates an available GPT.
4. Open that GPT in ChatGPT and start assigning tasks directly.

To use ngrok instead of the default Cloudflare tunnel, provide an auth token and select the proxy:

```bash
NGROK_AUTHTOKEN=your_token chatgpt2codex --proxy ngrok
```
