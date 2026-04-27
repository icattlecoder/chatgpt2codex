# chatgpt2codex

[中文](README.zh-CN.md)

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

1. 在你的项目空间中执行：
```bash
cd /path/to/project
chatgpt2codex
```

2. On first launch, sign in to your ChatGPT account when prompted.
3. The program automatically creates an available GPT.
4. Open that GPT in ChatGPT and start assigning tasks directly.

## Custom Domain

You can also expose ChatGPT through a custom domain backed by a Cloudflare Tunnel.
Compared with a random tunnel hostname, a custom domain helps GPT start faster.

`chatgpt2codex domain` starts an interactive setup and stores the global custom-domain configuration in `~/.chatgpt2codex/config.json`.

`chatgpt2codex domain <domain>` uses `<domain>` as the default value in the interactive setup.

`chatgpt2codex domain -` disables the custom-domain mode while keeping the saved Cloudflare API token.

When `cloudflare.enableDomain` is `false`, startup keeps using Cloudflare Quick Tunnel and generates a fresh API key for each process.

When `cloudflare.enableDomain` is `true`:

1. The workspace hostname defaults to `<workspace>.<domain>`.
2. The workspace record persists `host`, `api_key`, and `gpt_id`.
3. Startup reuses the persisted API key.
4. GPT automation is skipped by default.
5. `chatgpt2codex --reset` forces GPT metadata and Action auth to be synced again.
6. Zone ID, account ID, tunnel ID, and tunnel token are resolved through the Cloudflare API token at startup.

## Custom Domain Setup

1. Run the interactive setup in your project directory:

```bash
chatgpt2codex domain
```

2. Enter or confirm the prompted values.
3. Start `chatgpt2codex` from the project root after the configuration is saved.
4. Run `chatgpt2codex --reset` if you need to resync the GPT name, instructions, Action settings, and bearer key.

## Interactive Setup Fields

The interactive setup asks for:

- Base domain
- Cloudflare API token

These values are persisted in `config.json`. The tunnel token is not stored; it is fetched from Cloudflare at startup.

## Cloudflare Named Tunnel Prerequisites

Before running `chatgpt2codex domain`, you still need:

1. A domain already managed by Cloudflare.
2. An existing Cloudflare Named Tunnel named `chatgpt2codex`.
3. A Cloudflare API token that can read the zone, read/update the tunnel, and create or update DNS routes.

## Remove Custom Domain

```bash
chatgpt2codex domain -
```

This sets `cloudflare.enableDomain` to `false` and switches startup back to a random tunnel hostname without deleting the saved API token.
