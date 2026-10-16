# HTTP API 契约

前缀 `/api/v1`。所有 API 使用 `Authorization: Bearer <管理令牌>`；令牌不接受查询参数传递。写入 JSON 使用 `Content-Type: application/json`。未知字段、额外 JSON 文本和超限请求会被拒绝。

错误格式：

```json
{"error":{"code":"conflict","message":"记录已变更或正在使用，请刷新后重试"}}
```

状态码：401 认证失败，403 来源拒绝，404 不存在，409 版本/运行冲突，400 输入或操作错误，503 浏览器不可用/繁忙。

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| GET | `/healthz`（无前缀） | 最小健康检查，无账号数据 |
| GET | `/meta`、`/templates` | 程序版本、Go 版本、浏览器状态、站点模板 |
| GET / POST | `/sites` | 列出 / 创建站点 |
| PUT / DELETE | `/sites/{id}` | 更新 / 删除站点 |
| GET / POST | `/accounts` | 列出 / 创建账号 |
| PUT / DELETE | `/accounts/{id}` | 更新 / 删除账号 |
| GET / PUT | `/settings` | 调度与保留设置 |
| POST | `/runs` | 入队；返回逐账号接受/跳过结果 |
| GET | `/runs` | 支持 account_id、status、action、limit、offset |
| DELETE | `/runs/{id}` | 取消尚未开始的任务 |
| POST | `/accounts/{id}/login` | 创建人工登录会话 |
| GET | `/sessions/{id}` | 登录会话描述与过期时间 |
| GET | `/sessions/{id}/screenshot` | 受鉴权保护的 JPEG，主要用于网页预览 |
| POST | `/sessions/{id}/input` | click / text / key / scroll / reload / focus |
| POST / DELETE | `/sessions/{id}/save` / `/sessions/{id}` | 保存 / 取消登录 |
| POST | `/backup/export` | `{"password":"..."}`，返回加密二进制文件 |
| POST | `/backup/restore` | `{"password":"...","archive":"Base64 文件内容"}` |
| GET / PUT | `/desktop` | Windows 偏好、托盘状态、菜单描述 |
| POST | `/desktop/action` | show / hide / minimize / quit / run_all / pause / open_data |
| GET | `/diagnostics` | Go 堆、goroutine 与浏览器池计数；不包含凭据 |

## 账号写入

```json
{
  "name": "主账号",
  "site_id": "anyrouter",
  "enabled": true,
  "schedule": {
    "timezone": "Asia/Shanghai",
    "window_start": "08:00",
    "window_end": "09:00",
    "retry_until": "09:00"
  },
  "version": 0,
  "credentials": {
    "method": "cookie",
    "api_user": "12345",
    "cookie_header": "session=..."
  }
}
```

`credentials` 为写入补丁。省略字段保留旧值，显式空字符串可清空；设置 `cookie_header` 会替换旧 Cookie 并清除旧浏览器存储。读取账号仅返回 `auth` 摘要，不返回密码、Cookie、Token。更新时携带读取到的 `version`；创建为 0。正在排队、执行或人工登录的账号禁止编辑和删除。

登录会话描述包含 `native_window`。为 true 时，客户端显示独立窗口的操作说明，不轮询截图；`focus` 用于显示当前登录窗口。取消会先中断正在等待的浏览器操作；关闭独立浏览器后，服务会结束会话并释放容量。

## 执行与人工重试

```json
{"account_ids":["account-id"],"retry":false}
```

空列表表示所有启用账号。再次手动执行使用 `retry:true`，且必须一次只指定一个账号；已经确认当天完成仍会拒绝。自动失败重试由后端按类型、次数和窗口决定，不由前端定时触发。

## Windows 偏好

```json
{"autostart":false,"minimize_to_tray":true,"close_to_tray":true,"version":1}
```

只有桌面进程注入 `DesktopIntegration` 时提供此接口。服务版/Docker 返回 404。后台业务不依赖 Windows 实现，前端也不直接调用系统 API。
