# 签到台 · Auto Check-In

一个面向个人使用的多站点、多账号自动签到工具。Go 核心、Vue 3 界面、SQLite 存储；Windows 独立应用窗口和 Docker 网页共用一套业务逻辑。

**v1.0.2 成品：** [Windows 下载](https://github.com/sck03/auto-check-in/releases/download/v1.0.2/AutoCheckIn-1.0.2-windows-amd64.zip) · [Docker 离线镜像](https://github.com/sck03/auto-check-in/releases/download/v1.0.2/auto-check-in-1.0.2-docker-linux-amd64.tar.gz) · [全部产物与校验和](https://github.com/sck03/auto-check-in/releases/tag/v1.0.2)

镜像：`docker pull ghcr.io/sck03/auto-check-in:1.0.2`（linux/amd64、linux/arm64）。

## 已实现

- 站点与账号管理，四种平台模板按专用站点和通用模板分组，支持接口与网页按钮签到。
- 北京时间 **08:00–09:00** 按账号分别随机首次执行，**每天确认成功一次**。
- 明确的临时故障默认最多重试 2 次，约等待 5 / 15 分钟并随机延后 0–30 秒；补签默认截止 **09:00**。
- 验证码、登录失效与结果不明分别处理；New API 支持只读签到状态复核。
- Windows 独立浏览器登录窗口，可直接输入、授权和处理验证码；网页预览支持直接输入与 OAuth 新页面跟随，再保存账号独立会话。
- AES-256-GCM 凭据存储、口令加密备份、持久任务队列、异常退出恢复和历史记录。
- Windows 托盘、最小化到托盘、关闭窗口后继续运行、登录 Windows 后自动启动及托盘右键菜单。
- 单个后台浏览器进程、最多两个隔离会话、空闲 60 秒释放；隐藏界面停止轮询。

当前源码为 **1.0.3**，包含以下兼容性修复（尚未发布到 Releases）：

| 站点 | 每日操作 |
| --- | --- |
| AnyRouter | 北京时间 08:00 后访问已登录页面并确认身份，不调用签到接口 |
| AgentRouter | 北京时间 08:00 后重新登录，不调用退出接口，不点击签到 |
| 薄荷 API | 在 `https://up.x666.me` 保存 LinuxDO 登录会话；查询今日状态后执行一次免费抽奖，奖励直接到账 |
| New API（通用） | WONG 等兼容站点默认使用接口签到；需要网页按钮签到时，切换签到方式并配置页面及 CSS 选择器 |

前三项归为“专用站点”，New API（通用）合并原 New API、WONG 和自定义网页签到入口。接口签到调用 API 并检查今日状态；网页按钮签到打开页面并检查配置的结果标识。默认 HTTP 执行占用较低；OAuth 登录或站点验证可选择浏览器执行，网页按钮签到必须使用浏览器。没有账号引用的站点可切换平台模板和签到方式。

密码登录一般不需要先手动登录：有效会话会被复用，失效时才提交密码。验证码、二次验证需通过“浏览器登录”处理；OAuth 注册账号可能尚未设置本站密码。薄荷主站与签到页面的凭据独立，不能用主站 Cookie 替代签到页面的 `userToken`。

执行详情现在包含失败阶段、HTTP 状态、站点原因和处理建议。Windows 人工登录使用独立目录和普通可见浏览器参数；不连接日常浏览器配置，不发送会使导入会话失效的退出请求。Cloudflare 放行仍取决于真实站点、浏览器和网络环境。08:00 前的特殊站点测试不会占用当天正常计划。

默认自动任务和补签均在 09:00 截止，正在执行的自动任务也受此截止时间约束。保存登录、取消登录或签到结束后会关闭账号页面和授权弹窗，空闲时不访问站点；空的后台浏览器进程最多保留 60 秒供临近任务复用。

管理界面空闲时每 30 秒刷新，任务进行中每 5 秒刷新，手动操作和重新显示窗口会立即刷新；隐藏界面停止轮询。

奖励金额来自站点响应，不固定为 $25；余额差额不会被当作签到奖励。当前验证使用本地模拟站点，真实账号验收见 [进度与验收](docs/progress.md)。

## Windows 直接运行

使用发布目录中的 `AutoCheckIn.exe`。运行设备需要已安装 Edge 或 Chrome；用户不需要 Go、Node 或 Python。

从 v1.0.1 起，程序压缩包只包含可执行文件、简短 `README.md` 和单个 `THIRD_PARTY_NOTICES.md`。完整文档保留在仓库，依赖版本及完整许可文本合并到声明文件中，不再附带 `docs/` 或 `licenses/` 文件夹。

1. 双击 `AutoCheckIn.exe`，打开独立管理窗口。
2. 在“站点管理”中确认站点地址，在“账号签到”中添加账号。
3. 使用“浏览器登录”绑定会话，或填写账号凭据。AgentRouter 请选择密码或 OAuth 登录。
4. 手动执行一次并核对记录，再保留自动计划运行。

默认数据目录为 **`AutoCheckIn.exe` 所在目录下的 `data`**，数据库是 `data/checkin.db`；密钥、托盘设置和浏览器临时目录也保存在这里。设置页“运行环境”显示实际数据目录。程序放在 D/E 盘时，应用数据也保存在相应磁盘；该目录必须可写，不会因写入失败自动回退到系统盘。

本次整理按全新安装处理，使用新的空 `data` 目录重新添加站点和账号，不兼容旧数据库或旧模板配置，也不再从系统缓存目录自动导入。可使用 `-data` 或 `CHECKIN_DATA_DIR` 指定新目录；同版本数据可重开并使用加密备份恢复。

设置页可控制托盘和开机运行。开机运行默认关闭，启用后在**当前用户登录 Windows 后**直接驻留托盘。程序退出后不会执行计划；睡眠和关机期间也不会执行。

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

GitHub Actions 的 Linux 运行器明确固定为 **`ubuntu-26.04`**，避免 `ubuntu-latest` 迁移时自动切换环境。Docker 运行时使用独立固定的 Debian 基础镜像。

```powershell
# Windows：完整构建、测试、打包
.\scripts\build.ps1

# 单独开发
cd web
npm ci
npm run build
cd ..
go test ./...
go run ./cmd/server -data ./data -open
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
