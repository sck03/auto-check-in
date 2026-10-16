# 实施进度与验收记录

更新：2026-10-16。源码 1.0.3 的本地验收已完成；最新已发布版本仍为 v1.0.2。GitHub 构建结果以文末 Verify 运行记录为准，真实站点账号验收独立列出。

## 需求基线

| 编号 | 需求 | 实现位置 | 当前状态 |
| --- | --- | --- | --- |
| R01 | 多站点、多账号、简单中文界面 | domain / app / web | 已完成，网页 E2E 通过 |
| R02 | New API、AnyRouter、AgentRouter 分离适配 | provider | 已完成，协议契约与模拟站点通过 |
| R03 | 页面流程扩展与可替换执行层 | port / browser | 已完成，注册表和会话接口可独立扩展 |
| R04 | 北京时间 08:00–09:00 随机首次执行、成功一天一次、分类补签至 09:00 | domain / store / app | 已完成，重启预算和截止时间测试通过 |
| R05 | 凭据加密、账号隔离、API 鉴权、加密备份 | security / store / httpapi | 已完成，篡改/错误口令/回滚/读回脱敏通过 |
| R06 | 登录失效/验证码的人工处理入口 | browser / app / web | 已完成，桌面独立登录窗口、网页直接输入及两种 OAuth 跳转/返回通过 |
| R07 | 奖励与余额分开、历史记录 | provider / store / web | 已完成，奖励字段与余额变化独立验证 |
| R08 | Windows 程序与 Docker 共用核心 | cmd / Dockerfile / scripts | Windows 成品、Linux 构建和 GitHub 实际容器验证通过 |
| R09 | 架构、运维、扩展文档与测试证据 | docs / tests | 已完成，文档按最终方案修订 |
| R10 | 托盘、自启动、八项右键菜单 | desktop / DesktopSettings | 已完成，实际 Windows 程序验证通过 |
| R11 | 空闲资源、流畅性和稳定性 | browser/pool / app/engine / web | 已完成，共享进程隔离、取消和回收测试通过 |
| R12 | GitHub 最新稳定 Actions、Windows/Docker 产物与发布 | .github/workflows / scripts | 已配置完整 SHA 固定版本，Verify 全部通过；版本产物见 Releases |
| R13 | 精简发布包、保留完整许可、固定 Ubuntu 26.04 | build/archive / scripts / .github | v1.0.1 已发布，Ubuntu 26.04 Verify / Release 全部通过 |
| R14 | 登录卡顿、OAuth 新页及四种登录方式验证 | browser/tabs / interactive / BrowserDialog | v1.0.2 已发布，本地验收、三项 CI、发布包重新下载实测全部通过 |
| R15 | 登录状态覆盖、取消启动残留、刷新并发与输入积压 | browser/process / app/login / web | 全量竞态、7 组网页回归、可见 OAuth、Windows 成品资源释放及在线 CI 通过 |
| R16 | AnyRouter 访问签到、AgentRouter 新登录、WONG 与薄荷适配、失败详情 | provider / browser / web | 本地协议、真实浏览器与 8 组网页回归通过 |
| R17 | 默认自动操作全部限制在 08:00–09:00、完成后停止站点访问 | domain / store / app / browser | 旧配置迁移、排队过期、执行截止、关闭页面和弹窗后零新增请求通过 |
| R18 | 数据保存在程序目录、旧目录与密钥迁移、显示实际路径 | bootstrap / store / httpapi / web | 数据与 WAL、密钥和偏好迁移、完整性校验、失败保护及实际本机数据核对通过 |
| R19 | 历史增长后的查询性能、过期队列与界面刷新 | store / RunsView | 索引升级、连接重建、过期任务跳过、重试预算更新和请求合并/取消回归通过 |

## 本地验收结果

| 检查 | 结果 |
| --- | --- |
| Go 单元测试与静态检查 | 通过 |
| Go 竞态测试 | 通过，包括真实 Chromium 资源池 |
| 前端类型检查与生产构建 | 通过 |
| 前端回归测试 | 11 组通过：管理/签到及模板切换、防护/备份、人工登录、失败详情、高延迟输入、刷新与保存并发、退出后的迟到响应、完整响应体超时、筛选分页去重、慢请求刷新合并及离页取消 |
| Windows 程序 | 启动、自动连接、版本显示通过 |
| 托盘生命周期 | 最小化、恢复、关闭留驻、暂停、正常退出通过 |
| 开机运行 | 开关联动、当前用户注册适配器、偏好冲突与失败回滚通过；未执行整机重启 |
| 资源池 | 两个账号一个后台进程，Cookie 隔离、独立取消、空闲释放通过 |
| 发布包 | v1.0.3 Windows ZIP 仅 4 个文件、Linux tar 仅 3 个文件；55 个组件的完整许可合并保留，构建与 ZIP 成品验收通过 |
| Linux 文件权限 | tar 内服务程序为 0755、说明文件为 0644；归档回归测试通过 |
| 登录方式 | Cookie 恢复、密码登录、GitHub/LinuxDo 同页及新页授权回调、窗口关闭返回、会话保存和账号隔离通过 |
| 桌面人工登录 | 实际窗口打开、显示、取消通过，管理页面不再轮询截图 |
| 启动与状态恢复 | 无响应浏览器进程、等待启动的账号均可取消；诊断查询不阻塞，临时目录与会话名额释放；后续页面不再重复注入旧登录状态 |
| 人工登录参数 | Edge、Chrome 的可见窗口不启用自动化标记；本地 GitHub/LinuxDO 授权弹窗回调通过，未据此宣称 Cloudflare 线上验证通过 |
| 薄荷令牌 | 登录窗口退出会清除旧令牌，手动替换令牌会清除冲突的浏览器状态；独立令牌仍可使用 |

2026-10-16：完整构建中的 `go test ./...` 与 `go vet ./...`、开启真实 Chromium 的 `go test -race -count=1 ./internal/...`、前端生产构建及严格未使用代码检查全部通过。11 组前端回归通过。实际 Windows 验收使用从最新 1.0.3 ZIP 解压出的程序，核对版本、窗口、托盘、开机开关、独立登录窗口、取消后资源释放与正常退出；测试使用隔离数据目录。

1.0.3 Windows ZIP 为 12,074,337 字节，仅含两个可执行文件、简短 README 和合并许可声明（55 个组件）。SHA-256：`3fb5e1085d123b9120d6da94a468b7f07e2a6b2efaf3f787989757df5d2af5f9`。Linux 服务包同时构建完成。本次没有发布新的 GitHub Release。

已移走本次 27 个模拟数据、构建暂存和成品验证目录，集中在本地 `待手动清理/20261016-验证临时文件/`；当前运行环境的自动审批阻止递归删除，因此由用户手动清理。该目录已被 Git 和 Docker 忽略。最新程序、压缩包、真实数据、验证摘要及截图保留。新增的 3 组记录页面回归在隔离后台轮询时钟后另行复测通过，避免定时刷新干扰请求计数。

本机旧数据库已迁到可执行文件旁的 `data`，迁移前后逐表内容一致，全部账号凭据可解密，密钥和访问令牌一致；原始数据库和密钥已另行归档至非系统盘。验证过程中没有启动真实账号任务。

同一 Windows 主机的 20,000 条已完成历史基准：空队列查询从 12,443,977 ns/op 降至 131,923 ns/op，分配从 603 次降至 55 次；当日无尝试记录时的计数查询从 13,347,721 ns/op 降至 72,612 ns/op，分配从 604 次降至 30 次。这是本机样本，不是所有设备或站点的速度保证。可用 `go test ./internal/store -run '^$' -bench BenchmarkHistoryQueries -benchmem` 复测。

截图与机器可读记录由测试脚本保存在本地 artifacts/。一次空白实例的托盘诊断为 Go 堆约 1.45 MiB、12 个 goroutine、后台浏览器未运行；这不是总 RSS 或通用性能承诺。

2026-10-15 的高延迟回归：每次输入请求人为增加 200 ms 延迟，同样 29 个字符由修复前的 29 次请求合并为 4 次，减少约 86%。测试同时核对输入内容、跨输入框顺序及会话保存；该数字衡量请求数，不代表所有网站的整体速度提升比例。

## 在线构建与发布

**[v1.0.2 已发布](https://github.com/sck03/auto-check-in/releases/tag/v1.0.2)**：[Verify #38714753128](https://github.com/sck03/auto-check-in/actions/runs/38714753128) 的 Windows、Ubuntu 26.04 和 container 全部通过，对应代码提交 `57a80b4`。首轮自动探测浏览器时出现初始化超时，前后端改为明确指定 Chrome 后通过；启动失败与正常退出分别归类。两个系统均运行 7 组网页回归。[Release #38715409711](https://github.com/sck03/auto-check-in/actions/runs/38715409711) 的 version、windows、docker、publish 全部成功。

已核对三个程序/镜像包的 SHA-256 清单与 GitHub 资产摘要。重新下载的 Windows ZIP 为 `a01a15420ee78ee3e15d47c87fde9f11ae71d5e711ede5d032d8912ab4311590`，仅含两个程序、简短 README 和合并许可；对下载包再次执行托盘、开机开关、独立登录窗口、取消后资源释放和正常退出测试，全部通过。Linux 程序执行权限与版本由发布流程检查通过。

GHCR `1.0.2` 镜像索引包含 linux/amd64 和 linux/arm64，已验证匿名读取；索引摘要为 `sha256:8f752f2e1ebdbde153e1bcc5b43de253d504f162f3ce9f2ef724542a7b364f24`。Windows ZIP 约 12 MB，Docker 离线包约 376 MB。

**v1.0.1 已发布**：[Verify #38703736992](https://github.com/sck03/auto-check-in/actions/runs/38703736992) 和 [Release #38704107828](https://github.com/sck03/auto-check-in/actions/runs/38704107828) 全部通过；Linux job 固定为 Ubuntu 26.04，新运行没有迁移警告。程序包不再带 docs/licenses 目录，完整第三方许可保留在单个声明文件；发布前已核对归档文件清单、SHA-256 和 Linux 程序执行权限。

首轮完整远端验收：[Verify #38698044432](https://github.com/sck03/auto-check-in/actions/runs/38698044432)，Windows、Linux、container 三个 job 全部成功，对应实现提交 3a5f15d。容器测试包含真正的 Chromium 密码登录、签到、奖励判断和重启后的去重。

**v1.0.0 已发布**：[Release #38698942047](https://github.com/sck03/auto-check-in/actions/runs/38698942047) 的 version、windows、docker、publish 全部成功；对应代码提交 9e4aa8c。[该提交 Verify](https://github.com/sck03/auto-check-in/actions/runs/38698934900) 也全部成功。

已核对三个下载产物的 SHA-256 与 GitHub 资产摘要一致，确认 GHCR 可匿名拉取且包含 linux/amd64、linux/arm64。镜像索引摘要为 `sha256:21ec11d9348f4849232195866069b7228cda25049776248c1357ccb373364c79`。Windows ZIP 约 12 MB，Docker 离线包约 400 MB。

已重新下载 GitHub 发布的 Windows ZIP，校验实际下载文件的 SHA-256，并对包内程序再次运行原生窗口/托盘全流程测试，结果通过。正式 Release 仅保留三个程序/镜像包与校验和文件，构建调试记录保留在 Actions 内。

- [Verify 运行记录](https://github.com/sck03/auto-check-in/actions/workflows/ci.yml)：包含实际 Docker Chromium 登录、签到与重启验证。
- [Release 运行记录](https://github.com/sck03/auto-check-in/actions/workflows/release.yml)：Windows ZIP、Linux 服务包、GHCR 双架构镜像及 amd64 Docker 离线包。
- [GitHub Releases](https://github.com/sck03/auto-check-in/releases)：构建与容器检查通过后才发布下载文件。

## 已完成研究

- 原会话全部要求已核对，包括“前端不需要复杂”的最后更正。
- 三个开源项目已固定版本、阅读相关完整方案，并核对关键代码。
- 补充核对 New API `19d435e` 的 OAuth 和认证实现；保留不同分支的登录、会话与奖励差异。Agent Router 公开登录页实测会另开 LinuxDo 授权页，可出现站点验证；尚未使用真实账号完成登录。
- 已确定 Go + Vue + SQLite + chromedp，Windows 薄壳与 Docker 共享 API。
- 已记录来源冲突、许可处理、AgentRouter 语义与奖励核验规则；按本线程最新要求统一每日随机窗口。
- 已核对并安装最新稳定版 Go 1.27.2（官方 stable=true、SHA-256 已验证）。

## 外部验收

真实站点账号、当前线上验证策略及奖励到账尚未验证；需要使用用户自己的账号逐站点验收。本地没有 Docker 引擎，已使用 GitHub 工作流完成实际容器测试。Windows 自动启动尚未经过整机注销/重启测试。
