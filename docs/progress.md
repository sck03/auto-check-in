# 实施进度与验收记录

更新：2026-10-15。实现与本地验收已完成，GitHub 构建/发布结果以本文链接中的实际运行记录为准。真实站点账号验收独立列出。

## 需求基线

| 编号 | 需求 | 实现位置 | 当前状态 |
| --- | --- | --- | --- |
| R01 | 多站点、多账号、简单中文界面 | domain / app / web | 已完成，网页 E2E 通过 |
| R02 | New API、AnyRouter、AgentRouter 分离适配 | provider | 已完成，协议契约与模拟站点通过 |
| R03 | 页面流程扩展与可替换执行层 | port / browser | 已完成，注册表和会话接口可独立扩展 |
| R04 | 北京时间 08:00–09:00 随机首次执行、成功一天一次、分类补签至 09:30 | domain / store / app | 已完成，重启预算和截止时间测试通过 |
| R05 | 凭据加密、账号隔离、API 鉴权、加密备份 | security / store / httpapi | 已完成，篡改/错误口令/回滚/读回脱敏通过 |
| R06 | 登录失效/验证码的人工处理入口 | browser / app / web | 已完成，真实 Chromium 登录和保存会话通过 |
| R07 | 奖励与余额分开、历史记录 | provider / store / web | 已完成，奖励字段与余额变化独立验证 |
| R08 | Windows 程序与 Docker 共用核心 | cmd / Dockerfile / scripts | Windows 成品、Linux 构建和 GitHub 实际容器验证通过 |
| R09 | 架构、运维、扩展文档与测试证据 | docs / tests | 已完成，文档按最终方案修订 |
| R10 | 托盘、自启动、八项右键菜单 | desktop / DesktopSettings | 已完成，实际 Windows 程序验证通过 |
| R11 | 空闲资源、流畅性和稳定性 | browser/pool / app/engine / web | 已完成，共享进程隔离、取消和回收测试通过 |
| R12 | GitHub 最新稳定 Actions、Windows/Docker 产物与发布 | .github/workflows / scripts | 已配置完整 SHA 固定版本，Verify 全部通过；版本产物见 Releases |
| R13 | 精简发布包、保留完整许可、固定 Ubuntu 26.04 | build/archive / scripts / .github | v1.0.1 本地打包与静态检查通过，在线构建待验证 |

## 本地验收结果

| 检查 | 结果 |
| --- | --- |
| Go 单元测试与静态检查 | 通过 |
| Go 竞态测试 | 通过，包括真实 Chromium 资源池 |
| 前端类型检查与生产构建 | 通过 |
| 网页端到端测试 | 3 组通过：管理/签到、防护/备份、人工登录 |
| Windows 程序 | 启动、自动连接、版本显示通过 |
| 托盘生命周期 | 最小化、恢复、关闭留驻、暂停、正常退出通过 |
| 开机运行 | 开关联动、当前用户注册适配器、偏好冲突与失败回滚通过；未执行整机重启 |
| 资源池 | 两个账号一个后台进程，Cookie 隔离、独立取消、空闲释放通过 |
| 发布包 | v1.0.1 Windows ZIP 仅 4 个文件、Linux tar 仅 3 个文件；54 个依赖和 Go 的完整许可合并保留，重复构建通过 |
| Linux 文件权限 | tar 内服务程序为 0755、说明文件为 0644；归档回归测试通过 |

截图与机器可读记录由测试脚本保存在本地 artifacts/。一次空白实例的托盘诊断为 Go 堆约 1.45 MiB、12 个 goroutine、后台浏览器未运行；这不是总 RSS 或通用性能承诺。

## 在线构建与发布

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
- 已确定 Go + Vue + SQLite + chromedp，Windows 薄壳与 Docker 共享 API。
- 已记录来源冲突、许可处理、AgentRouter 语义与奖励核验规则；按本线程最新要求统一每日随机窗口。
- 已核对并安装最新稳定版 Go 1.27.2（官方 stable=true、SHA-256 已验证）。

## 外部验收

真实站点账号、当前线上验证策略及奖励到账尚未验证；需要使用用户自己的账号逐站点验收。本地没有 Docker 引擎，已使用 GitHub 工作流完成实际容器测试。Windows 自动启动尚未经过整机注销/重启测试。
