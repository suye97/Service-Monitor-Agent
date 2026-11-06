# Service Monitor Agent

基于 Go、Wails v2 的桌面 Linux 服务监控应用。通过 SSH 采集 CPU、内存与本地磁盘挂载点使用率，管理多台服务器、项目日志、持续告警和日志对话分析。

## 构建与运行

需要 Go 1.24+、Node.js、Wails v2，以及 Windows WebView2 与 Wails 要求的 C 编译工具链。

```powershell
.\build.ps1
```

当前环境的 Wails CLI 2.10.1 在 Go 1.27 上生成绑定时会报内部类型加载错误，`build.ps1` 因此使用 Go 直接编译 Wails 应用并嵌入前端产物。CLI 兼容的工具链中可使用 `wails dev` 开发运行。首次启动在界面创建加密凭据文件并设置至少 12 字符的管理员口令；随后添加更多服务器时需再次输入管理员口令。每次重启应用都要重新解锁，监控才开始采集。管理员口令遗失后无法恢复加密文件里的 SSH 密码，请公司单独保管备份。

也可由管理员通过命令行创建包含首台服务器的加密文件（输入密码时终端不回显）：

```powershell
go run ./cmd/vaulttool -id server-1 -name 生产机 -host 192.0.2.10 -port 22 -user monitor -out credentials.enc
```

将生成的文件放到操作系统用户配置目录下的 `ServiceMonitorAgent/credentials.enc`；应用启动时读取该位置。所有服务器 SSH 密码仅保存在该文件中，格式是 Argon2id 派生密钥与 AES-256-GCM 认证加密的 JSON 封套。项目、告警和其他设置保存在同目录的数据库或状态文件中。模型 API 密钥、飞书 AppSecret 和 QQ AppSecret 使用随机数据密钥加密写入 `private-data.enc`，数据密钥由管理员口令加密保存在 `private-key.enc`；解锁后自动恢复，无需每次重启重新输入。界面只显示是否已配置，不回显或提供复制明文的功能，也不将这些密钥写入 SQLite。请将三个加密文件与数据库一并备份；遗失管理员口令或 `private-key.enc` 后无法恢复私有数据。旧版本未落盘的密钥需在升级后重新输入一次。

## SSH 主机身份

应用严格使用当前系统用户 `~/.ssh/known_hosts` 校验 SSH 主机公钥。首次连接前，管理员应从可信渠道核对服务器公钥指纹，并将对应公钥加入该文件；未知或变化的主机密钥会导致连接失败。监控账号至少需能读取 `/proc/stat`、`/proc/meminfo`，运行 `df -Pk -l`，以及读取被配置的日志文件。项目日志路径应指向具体文件。

## 监控与告警

默认每 30 秒采样。CPU 读取间隔 1 秒的两次 `/proc/stat`，内存使用 `MemAvailable`，磁盘以 `df` 返回的最大本地挂载点占用率判定；服务器详情可展开查看各挂载点。超过 80% 发送阈值告警；相邻采样突增至少 20 个百分点且超过阈值，连续 5 分钟不低于阈值则发送持续告警。阈值、突增幅度、持续时间和采样周期可设置；采集失败在界面显示错误，不当作 0%。采集错误与通知错误分别展示，通知失败不会隐藏有效指标。告警状态存于 `alerts.json`，按通知渠道记录投递结果；部分失败时仅重试失败渠道，同一告警在单个渠道失败 3 次后停止本次投递，新告警重新计数，并避免重启后重复发送已成功送达的告警。

飞书和 QQ 可分别配置是否启用，均使用 AppID / AppSecret。飞书通过官方 WebSocket 长连接接收 `im.message.receive_v1` 事件并发现 `chat_id`，再通过官方 IM OpenAPI 向目标会话发送文本告警。需要在飞书开放平台配置事件订阅及消息权限。

QQ 按 [API v2 文档](https://bot.q.qq.com/wiki/develop/api-v2/) 获取 `QQBot` access token，通过 `/v2/groups/{group_openid}/messages` 或 `/v2/users/{openid}/messages` 发送群聊或 C2C 单聊文本。Gateway WebSocket 使用当前接口 `GET https://api.sgroup.qq.com/gateway` 获取地址，用于连接状态和发现群/C2C 目标；保存时只需机器人凭据，不强制要求目标类型或 OpenID，实际发送前才校验目标。事件权限及连接可用性以 QQ 平台为准，失败时显示真实错误。也可手动填写目标 OpenID。主动消息仍受 QQ 平台权限、频率和配额限制。

[OpenClaw FAQ](https://q.qq.com/qqbot/openclaw/faq.html) 中的“灵魂”是 OpenClaw 智能运行时，不是本程序的告警连接；若使用 OpenClaw 机器人，仍需按该指引单独安装并启动 OpenClaw Gateway。

WebSocket 不负责发送告警文本；实际发送由官方 HTTPS OpenAPI 完成。AppID、目标会话和加密的 AppSecret 均可保存；重启后解锁保险库即可恢复机器人连接。

## 日志分析

项目配置所属服务器、项目目录和日志文件绝对路径。点击分析时应用最多读取日志最后 256 KiB，并发送其中最后 32 KiB 到所配置的兼容 OpenAI Chat Completions 接口；连续对话会携带最近 6 轮问答，并限制历史内容长度。模型平台支持 DeepSeek、OpenAI 及其他 OpenAI 兼容接口。填写 BaseURL 和 API Key 后，可调用 `GET {BaseURL}/models` 获取当前密钥可用的模型并从下拉框选择；分析请求发送至 `POST {BaseURL}/chat/completions`。模型端点须为 HTTPS 或本机 HTTP。模型服务由使用单位提供；请先确定该服务允许接收日志数据。报告可导出 Markdown，并附最近 8 KiB 日志摘录；导出前会提示报告可能包含敏感数据。没有配置模型时分析功能提示先配置接口，不生成假分析结果。成功的问答、时间、来源和日志摘录加密保存在 `private-data.enc`，解锁后可按项目回看和导出；最近六轮记录可用于后续提问。最多保留最新 100 条，文件超过 8 MiB 时优先淘汰最早记录；单条记录过大则显示保存失败。旧版本仅在内存中的分析记录无法在升级后恢复。

## 当前约束

本机有系统级控制权限的人仍有机会在程序运行时提取解密后的 SSH 密码；离线管理员口令主要用于保护静态凭据文件。应用启动后需要管理员解锁，因此不适合无人值守开机自动监控。QQ 事件连接依赖平台向机器人开放的权限；OpenClaw“灵魂”在线状态需要另行配置 OpenClaw，不由本程序管理。真实服务器、飞书、QQ 和模型端到端测试需要相应的有效测试凭据与测试环境。
