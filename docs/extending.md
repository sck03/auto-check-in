# 站点与适配器扩展

## 新增同类站点

界面选择“New API（通用）”，填写站点名称和 origin。WONG 等兼容站点也使用此模板，按实际协议调整：

```json
{
  "template_id": "newapi",
  "adapter": "newapi",
  "transport": "http",
  "base_url": "https://relay.example.com",
  "login_path": "/login",
  "login_api_path": "/api/user/login",
  "checkin_path": "/api/user/checkin",
  "user_info_path": "/api/user/self",
  "user_header": "New-Api-User",
  "quota_per_unit": 500000,
  "unit": "USD"
}
```

这个片段说明关键字段；完整创建请求应以 `GET /api/v1/templates` 返回的 `site` 为基底。不需要改动调度和数据库。不同衍生站可能使用不同协议：AnyRouter 通过访问已登录页面签到，AgentRouter 通过重新登录签到，两者均不调用签到接口。

HTTP 模式使用独立 Cookie jar，浏览器模式在当前账号的隔离上下文里执行同源请求。如果目标站点需要正常浏览器会话，可以选择浏览器模式；这不保证验证码或风控自动通过。

## 页面流程

选择“New API（通用）”中的“网页按钮签到”（`template_id=newapi`、`adapter=browser`、`transport=browser`），指定签到页面、签到按钮、成功结果和可选“已签到” CSS 选择器。成功选择器必须只在完成状态可见，不应选中永久显示的标题或普通容器。首次接入应使用一个账号验证“未签到、成功、已签到、失败、登录失效”几种状态。

页面流程可通过人工登录保存 Cookie/localStorage，不要求继承 New API 登录协议。程序等待按钮启用后点击；提交后响应丢失或结果缺失时标记为待核对，不自动重复点击。只有页面明确信号可以确认完成，未提供结构化奖励金额时保持未知。

## 新增特殊协议

1. 在 `internal/provider` 增加适配函数，通过 `port.Session` 访问站点，返回 `domain.Result`。
2. 在 `DefaultRegistry` 注册 `Adapter`，必要时设置 `RequiresFreshLogin`。注册表在启动后只读。
3. 在站点类型校验和前端适配器类型中加入该类型，在 `internal/domain/templates.go` 登记模板 ID、名称、说明与默认配置。前端直接使用此目录显示名称，无需维护第二套标签；同一适配器可提供多个模板。涉及新的配置字段时同步更新 API 契约与备份版本策略。
4. 添加协议契约测试及本地 fixture，不向真实账号发送测试请求。

适配器不能直接读写 SQLite、操作托盘、发送界面事件或记录原始凭据。登录、会话、执行结果和进度写回由应用层统一编排。

## 结果契约

| 状态 | 使用条件 |
| --- | --- |
| `succeeded` | 业务字段或可靠页面结果明确确认签到 |
| `already_done` | 站点明确表示已签到，或只读状态复核确认 |
| `needs_attention` | 凭据错误、验证挑战、配置缺失 |
| `unverified` | 请求可能生效，但缺少可信结果 |
| `failed` + `retryable=true` | 明确的暂时性故障，允许受预算与窗口约束的重试 |
| `failed` + `retryable=false` | 明确业务拒绝，不重试当前周期 |

奖励使用整数原始额度和可选指针，缺失与 0 必须区分。余额前后快照分开记录。不要通过“HTTP 200”“HTML 包含 success”“余额上升”推断奖励。

## 替换浏览器驱动

实现 `port.SessionFactory`、`port.Session`，需要人工操作时再实现 `port.BrowserFactory` / `port.InteractiveSession`。浏览器驱动负责进程、上下文、Cookie 域和生命周期，平台适配器只看到协议接口。

当前 chromedp 0.21 使用泛型动作与默认管道传输。项目在 `internal/browser/cdp.go` 集中处理动作调用、JSON 解码与操作取消。显式调试端口必须配套 `remote.WebSocket`，不要把测试连接配置复制为生产开放端口。
