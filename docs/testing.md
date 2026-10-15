# 验证说明

## 自动化范围

- 领域规则：北京时间窗口、按账号独立随机、重启稳定性、成功后的日期边界、补签截止和非法配置。
- 站点协议：严格 JSON、成功/已签到、AgentRouter checked_in 缺失、HTML 假成功、401、429、余额与奖励分离。
- 存储：AEAD 加密、账号关联验证、版本冲突、并发入队唯一性、有限重试和重启预算、异常退出、备份事务回滚。
- HTTP：访问鉴权、来源校验、未知字段、请求大小、静态页面与 API 分离、账号读回脱敏。
- 执行器：总并发与同 origin 串行、异常退出归一化、通知唤醒。
- 真实 Chromium：密码登录、独立账号上下文、截图、操作、一个账号关闭不影响另一个、共享进程与空闲回收。
- Windows：当前用户启动注册适配器（隔离测试键）、偏好持久化/冲突/回滚。
- 网页 E2E：站点/账号新增、签到与防重复、编辑保留凭据、人工浏览器登录、加密备份恢复。
- Windows 实际程序：嵌入界面、版本、托盘状态、八项菜单、最小化、恢复、关闭留驻、暂停、开机开关联动、正常退出。

## 命令

```powershell
go test -race ./internal/...
go vet ./...
$env:CHECKIN_BROWSER_TEST='1'
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
