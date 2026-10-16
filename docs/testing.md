# 验证说明

## 自动化范围

- 领域规则：北京时间窗口、按账号独立随机、重启稳定性、成功后的日期边界、补签截止和非法配置。
- 站点协议：严格 JSON、成功/已签到、AnyRouter 只访问、AgentRouter 新登录与不注销导入会话、New API 状态复核、薄荷每日抽奖与令牌失效、HTML 假成功、401、429、余额与奖励分离。
- 存储：AEAD 加密、账号关联验证、版本冲突、并发入队唯一性、有限重试和重启预算、异常退出、备份事务回滚。
- HTTP：访问鉴权、来源校验、未知字段、请求大小、静态页面与 API 分离、账号读回脱敏。
- 执行器：总并发与同 origin 串行、异常退出归一化、通知唤醒。
- 真实 Chromium：Cookie 恢复、密码登录、GitHub/LinuxDo 同页跳转和新页授权、授权返回及会话保存、多账号页面与 Cookie 隔离、共享进程与空闲回收。
- Windows：当前用户启动注册适配器（隔离测试键）、偏好持久化/冲突/回滚。
- 网页回归：站点/账号新增、签到与防重复、编辑保留凭据、人工浏览器登录、高延迟连续输入、保存与刷新并发、退出后的迟到响应、完整响应体超时、加密备份恢复。
- 浏览器生命周期：不响应 CDP 的子进程、等待启动的账号取消、诊断查询不阻塞、临时目录与会话名额释放；恢复的 localStorage 不会覆盖后续登录或退出状态。
- 浏览器空闲：关闭账号页面及 OAuth 弹窗后，模拟站点的轮询请求停止；覆盖后台池保留与可见进程关闭两种情况。
- 登录回归：延迟渲染且只有 id 的输入框、提交后立即跳转导致响应体丢失、有效会话不重复登录、薄荷 userToken 保存和恢复、可见人工窗口不启用自动化标记。
- 执行记录：手动密码失败显示原始业务原因、登录阶段、HTTP 状态与建议；密码脱敏；08:00 前的跳过不消耗正常计划。
- 调度截止：旧默认配置一次性迁移至 09:00、自定义窗口保留；首次与补签队列到点取消，运行中任务及失败后的复核共用截止时间。
- Windows 实际程序：界面、版本、托盘状态、八项菜单、最小化、恢复、关闭留驻、暂停、开机开关联动、独立登录窗口显示/取消及零截图轮询、正常退出。

## 命令

```powershell
go test -race ./internal/...
go vet ./...
$env:CHECKIN_BROWSER_TEST='1'
$env:CHECKIN_NATIVE_BROWSER_TEST='1' # 仅本地 Windows 桌面测试，打开隔离的测试窗口
go test -race -count=1 ./internal/browser
cd web
npm ci
npm run build
npm run test:e2e
cd ..
node scripts/native-smoke.mjs
```

端到端测试需要先生成 `dist/checkin-server.exe`（Linux 为 `dist/checkin-server`）。Windows 程序测试还需要 `dist/AutoCheckIn.exe`。测试目录在 `artifacts/`，与实际用户数据目录分开。

## 证据

测试脚本生成 `artifacts/go-tests.jsonl`、`web/test-results/results.json`、`artifacts/native-smoke.json` 和界面 PNG。最新逐项状态见 [进度与验收](progress.md)。

一次实测的托盘空闲状态：后台浏览器未运行，Go 堆约 1.45 MiB、12 个 goroutine。这是该次空白测试实例的**Go 已分配堆**，不是进程 RSS、总内存或所有电脑的保证。浏览器启动/页面复杂度会显著影响总内存。

## 边界

模拟站点通过不等于真实网站通过；开机开关测试通过不等于执行了整机重启。本地没有 Docker 引擎，已在 GitHub 完成实际容器启动、Chromium 登录/签到和重启测试。需后续完成真实账号协议/风控验收以及 Windows 实际登录自启动验收。

GitHub 的 Windows / Linux 验证开启无头浏览器集成测试和 7 组网页回归；失败时保留 Playwright 跟踪记录。本地额外验证 Windows 可见窗口。Agent Router 的公开登录页已实测能打开 LinuxDo 新授权页；该页可出现站点验证，不代表已完成真实账号授权。
