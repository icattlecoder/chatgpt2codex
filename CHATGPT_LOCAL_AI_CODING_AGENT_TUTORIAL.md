# 如何把 ChatGPT 打造成一个本地 AI Coding Agent

本文基于当前仓库的实现，说明如何把 `chatgpt2codex` 接到 ChatGPT 的 GPT Actions 上，让 ChatGPT 直接调用本地文件读写、搜索、命令执行等能力，变成一个真正可操作项目代码的本地 AI Coding Agent。

> 前提假设：`chatgpt2codex` 的 npm 包已经发布完成，可以直接通过 `npm install -g chatgpt2codex` 安装。

---

## 一、这套方案在当前仓库里是怎么实现的

先看结论：这个仓库已经把“本地 Coding Agent”所需的 4 个关键部分都准备好了。

### 1. npm 包只是启动器，真正执行的是本地 CLI 二进制

相关代码：

- `package.json`
- `bin/chatgpt2codex.js`
- `scripts/install.js`

实现方式：

- `package.json` 通过 `bin` 暴露命令 `chatgpt2codex`
- `bin/chatgpt2codex.js` 会根据当前平台找到对应二进制，然后直接 `spawn(...)`
- `scripts/install.js` 会在安装时下载对应平台的 Release 二进制

这意味着用户执行：

```bash
chatgpt2codex serve
```

本质上是在运行当前平台的 Go CLI，而不是 Node.js 自己实现一套服务。

---

### 2. `serve` 命令会启动本地 HTTP 工具服务

相关代码：

- `cmd/chatgpt2codex/main.go`
- `internal/app/app.go`
- `internal/server/server.go`

在 `internal/app/app.go` 中，根命令注册了 3 个子命令：

- `serve`
- `tools`
- `prompt`

其中 `serve` 会：

1. 选择可用端口启动本地 HTTP 服务
2. 把工具能力注册为 HTTP API
3. 如果传入 `--proxy cloudflare`，再把本地地址暴露成公网地址

当前服务暴露的核心接口在 `internal/server/server.go` 中可以看到：

- `POST /tools/read`
- `POST /tools/bash`
- `POST /tools/edit`
- `POST /tools/write`
- `POST /tools/grep`
- `POST /tools/find`
- `POST /tools/ls`
- `POST /context/runtime`
- `GET /api.yaml`

也就是说，GPT Actions 连上的其实就是这组本地工具 API。

---

### 3. `prompt` 命令会输出 GPT 的系统提示词

相关代码：

- `internal/prompt/prompt.go`
- `internal/tool/definitions.go`

`chatgpt2codex prompt` 会自动生成一段系统提示词，内容包括：

- 当前可用工具列表
- 每个工具的用途
- Coding Agent 的行为约束
- 当前日期
- 当前工作目录

例如它会告诉 GPT：

- 优先用 `grep/find/ls` 做文件探索
- 用 `readFile` 看文件
- 用 `editFile` 做精确修改
- 多步骤任务使用 Todo 复选框
- 开始仓库任务前先调用 `get_runtime_context`

所以第 3.1 步里，直接把 `chatgpt2codex prompt` 的输出粘到 GPT 的 Instructions/System Prompt 中即可。

---

### 4. `GET /api.yaml` 就是给 GPT Actions 导入的 OpenAPI 描述

相关代码：

- `internal/docsasset/api/tools.api.yaml`
- `internal/server/server.go`

这里有一个很关键的点：

虽然仓库里有静态的 OpenAPI 模板，但真正给 GPT 导入时，最好使用运行中的：

```text
https://你的-public-url/api.yaml
```

原因是 `internal/server/server.go` 中的 `buildAPISpec(...)` 会把默认的：

```text
http://127.0.0.1:8080
```

替换成当前实际的公网地址。

这也是为什么在 GPT Builder 里，建议导入 `/api.yaml`，而不是只填根域名。

---

### 5. `get_runtime_context` 让 GPT 能读取仓库规则和技能

相关代码：

- `internal/runtimecontext/runtimecontext.go`

这是把它做成“代码代理”而不是“普通 API 调用器”的关键点。

当 GPT 调用 `get_runtime_context` 时，服务会在当前目录向上查找：

- `AGENTS.md`
- `.agents/skills/**/SKILL.md`

然后把这些内容拼成一个 `SystemInstruct` 返回给 GPT。

这意味着你可以在项目里继续定义：

- 仓库级约束
- 代码风格
- 发布流程
- 测试规范
- 特定子目录技能说明

这样 GPT 就不只是会“读写文件”，而是会“按你的项目约定来读写文件”。

---

## 二、最短接入步骤

下面就是你要写进教程里的最短路径。

### 步骤 1：安装 npm 包

```bash
npm install -g chatgpt2codex
```

安装完成后，`chatgpt2codex` 命令会由 npm 包包装层启动本地二进制。

---

### 步骤 2：启动本地服务并通过 Cloudflare 暴露公网地址

```bash
chatgpt2codex serve --proxy cloudflare
```

这一步对应的源码链路是：

- `internal/app/app.go`：启动 HTTP 服务
- `internal/proxy/proxy.go`：启动 Cloudflare Tunnel

`--proxy cloudflare` 的底层实现是调用：

```bash
cloudflared tunnel --protocol http2 --url http://127.0.0.1:8080
```

然后从输出中提取一个 `https://xxxxx.trycloudflare.com` 的公网地址。

正常情况下，你会看到类似输出：

```text
Listening on http://127.0.0.1:8080
Public URL: https://xxxxx.trycloudflare.com
https://xxxxx.trycloudflare.com/api.yaml
```

其中最重要的是最后这个：

```text
https://xxxxx.trycloudflare.com/api.yaml
```

它就是稍后导入 GPT Actions 的地址。

> 注意：要使用 `--proxy cloudflare`，你的机器上需要已经安装并能直接执行 `cloudflared`。

---

### 步骤 3：创建 GPT

#### 3.1 把 `prompt` 输出填入系统提示词

执行：

```bash
chatgpt2codex prompt
```

把输出内容复制到 GPT 的 Instructions / System Prompt 中。

这一步的作用是让 GPT 明白：

- 有哪些工具可用
- 应该如何探索代码仓库
- 应该优先使用哪些工具
- 修改代码时遵守什么规则

---

#### 3.2 添加 Action，并通过 URL 导入 OpenAPI

在 GPT Builder 中：

1. 打开 GPT 编辑页
2. 进入 **Actions**
3. 选择 **Create new action**
4. 选择 **Import from URL**
5. 粘贴第 2 步输出的：

```text
https://xxxxx.trycloudflare.com/api.yaml
```

不要只填根域名，最好直接填 `/api.yaml`，因为这个地址返回的是完整 OpenAPI 描述。

如果当前服务没有额外鉴权，可以先使用：

- Authentication: `None`

导入成功后，GPT 就会识别出这些动作：

- `readFile`
- `executeBash`
- `editFile`
- `writeFile`
- `grepFiles`
- `findFiles`
- `listDirectory`
- `get_runtime_context`

---

## 三、演示结果

下面给一个最贴合当前仓库的演示。

### 演示问题 1

在 GPT 里提问：

```text
请检查这个项目里，chatgpt2codex 是如何把本地 CLI 暴露给 ChatGPT 使用的，并列出关键文件。
```

GPT 的典型动作路径会是：

1. 调用 `get_runtime_context` 获取仓库上下文
2. 调用 `listDirectory` 查看根目录
3. 调用 `readFile` 读取：
   - `package.json`
   - `bin/chatgpt2codex.js`
   - `internal/app/app.go`
   - `internal/server/server.go`
   - `internal/prompt/prompt.go`

最后 GPT 会给出类似结论：

- npm 包通过 `package.json` 的 `bin` 暴露命令
- `bin/chatgpt2codex.js` 负责定位并启动平台二进制
- `serve` 启动本地工具服务
- `prompt` 生成系统提示词
- `/api.yaml` 提供给 GPT Actions 导入

---

### 演示问题 2

在 GPT 里提问：

```text
请读取当前仓库，说明为什么 GPT Builder 里应该导入 /api.yaml，而不是只填 public url 根地址。
```

GPT 通常会读取：

- `internal/server/server.go`
- `internal/docsasset/api/tools.api.yaml`

然后回答：

- `/api.yaml` 返回完整 OpenAPI schema
- schema 中定义了所有 operationId
- 服务会在返回 schema 时自动把默认 `127.0.0.1:8080` 替换成当前公网地址
- 所以 Builder 里导入 `/api.yaml` 最稳妥

---

### 演示问题 3

在 GPT 里提问：

```text
请帮我为 README 增加一节：如何把 chatgpt2codex 接到 ChatGPT GPT Actions。
```

GPT 的典型动作路径会是：

1. `readFile README.md`
2. `readFile internal/app/app.go`
3. `readFile internal/server/server.go`
4. `editFile README.md`

最后它就不只是“解释”，而是会真正修改仓库文件。

这时候，ChatGPT 就已经具备了一个本地 AI Coding Agent 的核心能力：

- 能读代码
- 能理解仓库上下文
- 能执行搜索
- 能改文件
- 能运行命令

---

## 四、建议你在教程里特别强调的 3 个点

### 1. 这是“本地工具服务 + GPT Actions”的组合

不是把代码上传到某个云端 IDE，也不是只做一个聊天机器人。

真正的链路是：

```text
ChatGPT GPT -> Actions -> public api.yaml -> local tool server -> current workspace
```

---

### 2. `prompt` 和 `get_runtime_context` 一起决定了“代理行为”

- `prompt` 解决“默认行为约束”
- `get_runtime_context` 解决“项目定制规则注入”

这两者结合后，ChatGPT 才更像一个可用的 Coding Agent，而不是只会随机调用工具。

---

### 3. 这个实现当前默认没有鉴权

从当前代码来看：

- `internal/server/server.go` 没有鉴权中间件
- OpenAPI 里也没有 `securitySchemes`

这意味着一旦你把它暴露到公网，理论上任何拿到地址的人都可能访问这些工具。

而这些工具里包含：

- 文件读取
- 文件写入
- 文本替换
- shell 命令执行

所以教程里一定要提醒：

- 只在可信环境下临时使用
- 尽量只暴露短时间会话
- 尽量把工作目录限制在单独项目中
- 使用完立即关闭 `serve`
- 不要把公网地址分享到公开场合

---

## 五、可直接复用的教程正文

下面这段可以直接用于博客、README 或文档页面。

## 用 ChatGPT + chatgpt2codex，把本地项目变成 AI Coding Agent

只需要 4 步，就可以让 ChatGPT 直接操作你本地项目代码。

### 1. 安装 CLI

```bash
npm install -g chatgpt2codex
```

### 2. 启动本地工具服务

```bash
chatgpt2codex serve --proxy cloudflare
```

命令启动后，会输出一个公网地址和一个 OpenAPI 地址，例如：

```text
Public URL: https://xxxxx.trycloudflare.com
https://xxxxx.trycloudflare.com/api.yaml
```

### 3. 创建 GPT

先执行：

```bash
chatgpt2codex prompt
```

把输出内容填入 GPT 的系统提示词。

然后在 GPT Builder 里添加 Action：

- Create new action
- Import from URL
- 粘贴 `https://xxxxx.trycloudflare.com/api.yaml`

完成后，GPT 就会获得：

- 读文件
- 搜索文件
- 列目录
- 改文件
- 写文件
- 执行命令
- 读取仓库运行时上下文

### 4. 开始演示

例如你可以直接对 GPT 说：

```text
请读取当前仓库，告诉我 npm 包装层、serve 命令、prompt 命令分别对应哪些源码文件，并帮我补一段接入说明到 README。
```

这时 GPT 会自动调用本地工具，读取代码后直接修改你的仓库文件。

这样，ChatGPT 就从“只能聊天”升级成了“可以在本地仓库里实际干活”的 AI Coding Agent。

---

## 六、补充：排障建议

### 导入 Action 失败

优先检查是不是导入了错误地址。应导入：

```text
https://xxxxx.trycloudflare.com/api.yaml
```

而不是只导入：

```text
https://xxxxx.trycloudflare.com
```

### `--proxy cloudflare` 启动失败

通常是本机没有安装 `cloudflared`，或者它不在 PATH 中。

### GPT 不理解你的仓库规范

在项目中补充：

- `AGENTS.md`
- `.agents/skills/**/SKILL.md`

然后让 GPT 在开始任务时先调用 `get_runtime_context`。

### 想追踪 GPT 实际调用了哪些工具

当前实现带有审计日志，默认会写到：

```text
~/.chatgpt2codex/
```

你可以据此回放某次会话里调用了哪些工具、传了什么参数、返回了什么结果。
