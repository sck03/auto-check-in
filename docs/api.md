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
| GET | `/meta`、`/templates` | 程序版本、Go 版本、浏览器状态、实际 `data_dir` 绝对路径、站点模板 |
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
| GET | `/diagnostics` | Go 堆、goroutine、浏览器池、profile 回收计数及结果写回状态；不包含凭据 |

## 站点模板

`GET /templates` 返回四种模板，显示名称统一读取 `site.name`：`anyrouter`（AnyRouter）、`agentrouter`（AgentRouter）、`x666`（薄荷 API）、`newapi`（New API（通用））。前三种为专用站点，后一种为通用模板。

`template_id` 必填并保存所选模板，`adapter` 决定签到方式，`transport` 决定 HTTP 或浏览器执行。通用模板允许 `adapter=newapi`（接口签到，含 WONG）和 `adapter=browser`（网页按钮签到，必须 `transport=browser`）；专用模板只允许对应适配器。网页签到的 `selectors.checkin` 留空时自动识别常见按钮；`selectors.success` / `selectors.already` 均留空时识别明确签到提示，填写其中任一项后仅使用配置的结果标识。多个按钮候选或无效 CSS 返回需要处理；点击后缺少结果返回 `unverified`。不匹配、空模板、已移除的 `wong` / `browser` 模板均拒绝保存。不再接受无用的 `logout_path` 字段。

数据库版本为 5，仅支持新建或重开同版本数据库，不进行旧版本迁移。站点名称、接口路径和模板选择随新版备份保存。

站点地址保存时去掉 HTTP 80 / HTTPS 443 默认端口。重开 v5 数据时会同步归一化等价地址，读取凭据时兼容原 localStorage origin 键；不会更换实际目标站点。

有账号引用时不能更换地址、适配器或平台模板；存在 OAuth 账号时不能切换到 HTTP。账号保存会在事务中再次检查当前站点配置，避免并发修改导致 OAuth 账号失去浏览器执行方式。

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

界面的“清除旧会话”同时清空 Cookie、Token 和旧用户 ID，保留用户名与密码供重新登录；同次填写的新凭据优先保留。仅有 localStorage 的页面登录也计为已保存会话。

登录会话描述包含 `native_window`。为 true 时，客户端显示独立窗口的操作说明，不轮询截图；`focus` 用于显示当前登录窗口。取消会先中断正在等待的浏览器操作；关闭独立浏览器后，服务会结束会话并释放容量。

已绑定 `api_user` 的账号在运行和保存登录期间不能改变用户身份；身份不符返回错误并保留原凭据。更换身份需明确编辑账号、清除旧会话。保存登录可在两秒内对 401 做只读复核，总验证超时仍为十二秒，不会重复提交登录。

`GET /diagnostics` 增加 `engine.pending_results` / `engine.save_failures`，分别为待恢复的本地结束记录数和本次进程结果写入失败次数。`browser_pool.profile_cleanup` 包含启动回收的 `removed` / `failed` 计数。结果保存故障后的恢复只写本地记录；`persistence_error` 表示需核对站点的未知状态。

`profile_cleanup.runtime_failed` 是运行期目录回收失败累计数，`pending` 是尚未回收的目录数，有限重试成功后 pending 归零。账号 Cookie 与 localStorage 的文本数据合计上限为 2 MiB，localStorage 最多 2048 项；超限拒绝新快照及备份恢复，已有凭据仍可读取。`invalid_status` 表示状态接口缺少可信的今日结果，未提交签到；对应阶段是 `status_check`。HTTP 与浏览器请求都支持数字及 HTTP 日期形式的 Retry-After。

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
