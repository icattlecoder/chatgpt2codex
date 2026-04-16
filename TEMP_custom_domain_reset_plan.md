# 全局自定义域名与 GPT 重置

## 目标
1. 支持 `chatgpt2codex domain <domain>` 设置全局自定义域名，`chatgpt2codex domain -` 清除全局配置。
2. 当设置了全局自定义域名后，工作区启动时默认主机名为 `<workspace>.<domain>`。
3. 使用自定义域名时，工作区记录需要持久化 `host`、`api_key` 和 `gpt_id`；后续启动复用该 `api_key`，不再每次随机生成。
4. 使用自定义域名时，启动默认不自动编辑 GPT；仅当用户显式传 `--reset` 时，才重新同步 GPT 名称、指令、Action。
5. 支持 `chatgpt2codex --reset`，强制刷新 GPT 元数据。
6. 如果未设置全局自定义域名，则继续走当前随机域名模式，即使用随机 `trycloudflare.com` 地址。

## 设计结论

### 1. 配置文件设计
建议把 `~/.chatgpt2codex/config.json` 拆成全局配置与工作区配置两层：

```json
{
  "domain": "chatgpt2codex.fun",
  "gpts": {
    "/abs/workspace": {
      "gpt_id": "g-123",
      "host": "my-project.chatgpt2codex.fun",
      "api_key": "ctc_xxx"
    }
  }
}
```

说明：
- `domain`：全局基础域名，由 `chatgpt2codex domain <domain>` 维护。
- `gpts.<workspace>.host`：当前工作区实际使用的 hostname，默认值为 `<workspace>.<domain>`。
- `gpts.<workspace>.api_key`：当前工作区固定 Action Bearer Key。
- `gpts.<workspace>.gpt_id`：现有 GPT 标识。

### 2. CLI 设计
- 根命令保留 `chatgpt2codex` 启动服务。
- 根命令新增 `--reset`。
- 新增子命令：`chatgpt2codex domain <domain>`。
- `chatgpt2codex domain -` 清除全局域名配置。
- `chatgpt2codex domain` 无参数时可直接打印当前全局域名，便于自检。

### 3. 域名规则
- 默认 hostname 为 `<workspace-dns-label>.<global-domain>`。
- 需要新增 `WorkspaceDNSLabel(workspace)`：
  - 小写化；
  - 非 `[a-z0-9-]` 替换为 `-`；
  - 去首尾 `-`；
  - 空值回退 `workspace`。
- 首版不做跨工作区 hostname 冲突消解，若同名目录冲突，由用户自行覆盖或后续补策略。

### 4. 启动行为
- 未设置全局自定义域名：
  - 继续走当前随机域名模式；
  - 使用 quick tunnel；
  - 公网地址保持为随机 `trycloudflare.com` 地址；
  - 每次启动随机生成 API Key；
  - 自动执行 GPT 创建或更新。
- 已设置全局自定义域名：
  - 启动时读取全局 `domain`。
  - 为当前工作区解析默认 `host`。
  - 若工作区未保存 `api_key`，首次生成一次并写回配置；后续复用。
  - 默认不执行 GPT 自动化。
  - 仅在传 `--reset` 时执行 GPT 更新。

### 5. Cloudflare 接入结论
- 现有 `internal/proxy` 仅支持 quick tunnel，得到的是随机 `trycloudflare.com` 地址。
- Quick Tunnel 只适合测试，不适合生产，也不能直接作为自定义域名方案。
- 自定义域名必须改为 named tunnel / published application 路线。
- 代码层至少需要补：
  - tunnel 标识与凭据持久化；
  - 指定 hostname 的 DNS / route 创建；
  - 运行时使用 named tunnel，而不是 quick tunnel。

### 6. GPT 同步策略
- `--reset` 的效果：
  - 强制重新同步 GPT 名称；
  - 强制重新同步指令；
  - 强制重新同步 OpenAPI Schema；
  - 强制重新同步 Action Bearer API Key。
- 设置了全局域名后：
  - 默认启动只拉起服务和 tunnel；
  - 不自动进入 Chrome 编辑 GPT；
  - 只有 `--reset` 才进入 GPT 编辑流程。

## Cloudflare 前置设置建议
- 域名 `chatgpt2codex.fun` 已在 Cloudflare 托管时，推荐走 named tunnel。
- 第一版推荐为每个工作区创建精确 hostname，如 `demo.chatgpt2codex.fun`，不要继续依赖 `trycloudflare.com`。
- 若后续需要覆盖大量工作区，可评估 `*.chatgpt2codex.fun` 通配路由，但代码里仍需明确 tunnel 与 ingress 关系。

## 影响文件
- `internal/app/app.go`
- `internal/config/config.go`
- `internal/config/config_test.go`
- `internal/gpt/manager.go`
- `internal/gpt/manager_test.go`
- `internal/proxy/proxy.go`
- `internal/proxy/proxy_test.go`
- `README.md`
- `docs/requirements/01-cli-and-service.md`
- `docs/requirements/03-context-audit-and-proxy.md`

## 任务列表
- [ ] TODO 1：补全局域名命令与配置模型
  - 新增 `domain` 子命令。
  - 把配置拆成全局 `domain` 与工作区 `host/api_key/gpt_id`。
  - 增加工作区名到 DNS label 的转换逻辑。

- [ ] TODO 2：补 named tunnel 启动链路与 `--reset` 策略
  - 已配置全局域名时，切换到 named tunnel。
  - 启动时为当前工作区生成或复用 `host` 与 `api_key`。
  - 默认跳过 GPT 自动化。
  - 传入 `--reset` 时强制同步 GPT。

- [ ] TODO 3：补测试与文档
  - 增加 `domain` 命令、配置兼容、`--reset` 行为测试。
  - 更新 README 与 requirements，替换 quick tunnel / 随机 API Key 的旧描述。
  - 增加 Cloudflare 前置配置说明。

## 验收点
- `chatgpt2codex domain chatgpt2codex.fun` 可写入全局域名。
- `chatgpt2codex domain -` 可清除全局域名。
- 设置全局域名后，工作区默认使用 `<workspace>.<domain>`。
- 设置全局域名后，多次启动复用同一工作区 `api_key`。
- 未设置全局域名时，启动仍返回随机 `trycloudflare.com` 地址。
- 设置全局域名后，不带 `--reset` 启动不会自动编辑 GPT。
- 带 `--reset` 启动会重新同步 GPT 名称、指令、Action。
- `go test ./...` 通过。

## 风险
- 这是从 quick tunnel 切到 named tunnel 的架构变更，不是简单参数透传。
- 需要决定 tunnel 生命周期：单 tunnel 多 hostname，还是每工作区一 tunnel。
- 旧配置文件必须向后兼容。