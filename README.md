# 签到台 · Auto Check-In

一个面向个人使用的多站点、多账号自动签到工具。Go 核心、Vue 3 界面、SQLite 存储；Windows 独立应用窗口和 Docker 网页共用一套业务逻辑。

**v1.0.0 成品：** [Windows 下载](https://github.com/sck03/auto-check-in/releases/download/v1.0.0/AutoCheckIn-1.0.0-windows-amd64.zip) · [Docker 离线镜像](https://github.com/sck03/auto-check-in/releases/download/v1.0.0/auto-check-in-1.0.0-docker-linux-amd64.tar.gz) · [全部产物与校验和](https://github.com/sck03/auto-check-in/releases/tag/v1.0.0)

镜像：`docker pull ghcr.io/sck03/auto-check-in:1.0.0`（linux/amd64、linux/arm64，已验证匿名拉取）。

## 已实现

- 站点与账号管理，内置 Any Router、Agent Router，支持 New API 兼容站点和 CSS 页面签到。
- 北京时间 **08:00–09:00** 按账号分别随机首次执行，**每天确认成功一次**。
- 明确的临时故障默认最多重试 2 次，约等待 5 / 15 分钟并随机延后 0–30 秒；补签默认截止 **09:30**。
- 验证码、登录失效与结果不明分别处理；New API 支持只读签到状态复核。
- 浏览器登录窗口，可完成人工登录、授权与验证码，再保存账号独立会话。
- AES-256-GCM 凭据存储、口令加密备份、持久任务队列、异常退出恢复和历史记录。
- Windows 托盘、最小化到托盘、关闭窗口后继续运行、登录 Windows 后自动启动及托盘右键菜单。
- 单个后台浏览器进程、最多两个隔离会话、空闲 60 秒释放；隐藏界面停止轮询。

奖励金额来自站点响应，不固定为 $25；余额差额不会被当作签到奖励。当前验证使用本地模拟站点，真实账号验收见 [进度与验收](docs/progress.md)。

## Windows 直接运行

使用发布目录中的 `AutoCheckIn.exe`。运行设备需要已安装 Edge 或 Chrome；用户不需要 Go、Node 或 Python。

1. 双击 `AutoCheckIn.exe`，打开独立管理窗口。
2. 在“站点管理”中确认站点地址，在“账号签到”中添加账号。
3. 使用“浏览器登录”绑定会话，或填写账号凭据。Agent Router 请选择密码或 OAuth 登录。
4. 手动执行一次并核对记录，再保留自动计划运行。

默认数据目录为 `%LOCALAPPDATA%\AutoCheckIn`。设置页可控制托盘和开机运行。开机运行默认关闭，启用后在**当前用户登录 Windows 后**直接驻留托盘。程序退出后不会执行计划；睡眠和关机期间也不会执行。

托盘右键提供：显示主窗口、立即签到全部、暂停/恢复自动计划、开机运行、最小化到托盘、关闭窗口后继续运行、打开数据目录、退出签到台。点击或双击托盘图标可以恢复窗口。

另有 `checkin-server.exe`，适合独立常驻服务：

```powershell
.\checkin-server.exe -open
# 使用独立数据目录
.\checkin-server.exe -listen 127.0.0.1:18990 -data E:\checkin-data
```

桌面版与服务版不能同时使用同一数据目录。再次启动桌面程序会尝试显示已有桌面实例。

## Docker

```bash
docker compose up -d --build
docker compose exec checkin sh -c 'cat /data/access.token'
```

打开 `http://127.0.0.1:18990`，输入上一步在运行设备上读取的管理访问令牌。数据保存在 `checkin-data` 命名卷。若需局域网访问，可在 `.env` 中设置 `CHECKIN_BIND_IP=0.0.0.0`；公网部署应由 HTTPS 反向代理提供入口。

容器包含 Chromium，人工登录通过管理页面完成，无需开放 CDP/VNC。已通过 GitHub 中的实际容器启动、Chromium 登录/签到和重启持久化测试，详细记录见 [进度与验收](docs/progress.md)。

## 构建与测试

本次实际使用 **Go 1.27.2**，2026-10-15 官方发布清单标记为最新稳定版。`go.mod` 固定 `toolchain go1.27.2`。前端使用 Node 24、Vue 3、TypeScript 5.9；TypeScript 版本与当前 vue-tsc 配套锁定。

```powershell
# Windows：完整构建、测试、打包
.\scripts\build.ps1

# 单独开发
cd web
npm ci
npm run build
cd ..
go test ./...
go run ./cmd/server -open
```

Windows 资源使用已固定的 GoVersionInfo 1.7.0 生成，不需要另装 MinGW。已经生成的 `build/windows/app.ico` 可直接使用，只有重新生成图标才需要 Pillow。

```powershell
# 浏览器隔离与资源池的真实 Chromium 测试
$env:CHECKIN_BROWSER_TEST = '1'
go test -race ./internal/...
go vet ./...

# 网页端到端测试：先构建 dist/checkin-server.exe
cd web
npm run test:e2e
cd ..

# 实际 Windows 窗口、托盘和开机开关测试
node scripts/native-smoke.mjs
```

`native-smoke` 使用隔离的启动配置文件；真实注册表适配器单元测试使用专用测试键，不修改实际开机运行项。

## 文档入口

| 文档 | 内容 |
| --- | --- |
| [架构设计](docs/architecture.md) | 模块边界、依赖、数据流、状态机、资源与桌面设计 |
| [进度与验收](docs/progress.md) | 已完成能力、证据、未验证的外部条件 |
| [开源参考核对](docs/research.md) | 三个开源项目的固定版本、方案冲突与采用决策 |
| [运行与维护](docs/operations.md) | 配置、备份、托盘、自启动、故障处理 |
| [适配器扩展](docs/extending.md) | 新站点配置、新流程代码与契约测试 |
| [API 契约](docs/api.md) | 路由、凭据写入、版本冲突和结果语义 |
| [验证说明](docs/testing.md) | 测试范围、运行命令与限制 |
| [GitHub 构建与发布](docs/github-release.md) | 最新稳定 Actions 固定版本、Releases、GHCR 与离线镜像 |
| [第三方声明](THIRD_PARTY_NOTICES.md) | 参考项目与分发依赖的许可声明 |
