# 开源方案核对与采用决策

核对日期：2026-10-17。需求来源：前序方案讨论与本次用户补充要求。界面按**简单小工具**设计，并补充每日成功一次、分类重试、资源优化、Windows 托盘、自启动和 GitHub 发布。

本线程后续明确：目标是所有账号**每天签到成功一次**，首次在北京时间 08:00–09:00 内分别随机安排，失败应分类处理。最终默认策略是明确的临时故障最多重试 2 次，间隔约 5 / 15 分钟，补签截止 09:00；登录失效和结果不明不盲目重复。参考项目中的 24 小时冷却模型不作为本项目默认调度规则。

## 核对范围与版本

| 参考项目 | 固定提交 | 已阅读的方案与实现 | 定位 |
| --- | --- | --- | --- |
| [Fl API Hub](https://github.com/RebornQ/fl_api_hub/tree/94ef25b0381d54ab11d1631d569a5cca4451eb84) | `94ef25b` | 完整 README、1486 行总 PRD、1662 行 AccountDialog 规格、New API 签到响应说明；站点接口、AnyRouter 适配器、调度实现 | 账号管理、适配层、界面与维护性参考 |
| [anyrouter-autolog](https://github.com/suainam/anyrouter-autolog/tree/687aa92944d19e83bc9c1cc25e0b544b4fa19525) | `687aa92` | 完整 README、配置模型、OAuth 实现、依赖和许可证；签到、结果判断与登录调用链 | AnyRouter 与 AgentRouter 协议参考 |
| [anyrouter-check-in](https://github.com/millylee/anyrouter-check-in/tree/3b16662142c68703ea3e79c77c06a8cf6bba7990) | `3b16662` | 完整 README、Provider/Account 配置、许可证、HTTP 签到流程 | 同类站点的声明式配置参考 |
| [New API](https://github.com/QuantumNous/new-api/tree/19d435e7fdfdaa2b9a734a76393f112cf980b815) | `19d435e` | OAuth 入口/回调、LinuxDo 服务端协议、登录状态与认证请求管理 | 衍生站的协议来源与版本差异核对 |
| [newapi-ai-check-in](https://github.com/aceHubert/newapi-ai-check-in/tree/a01c232672686ce66ac06194a7f13b04ca734a28) | `a01c232` | Provider 配置、WONG 签到路径、薄荷独立签到域名及 userToken、状态与抽奖响应、许可证 | WONG 模板及薄荷专用适配器的协议参考 |
| [Newapi-checkin](https://github.com/Jasonliu-0/Newapi-checkin/tree/49089a5bbe4c063fdb814e77d27168e55bed1700) | `49089a5` | 会话复用、密码续期、签到状态、错误分类、奖励字段和许可证 | 普通 New API 兼容性与故障说明参考 |

历史研究副本不进入构建、发布包和版本控制。本轮通过 GitHub API 和固定 SHA 的原始文件核对，未保留新的仓库副本。原项目的 PRD 包含浏览器扩展迁移规划，不能当成 Flutter 已实现功能的证明。

2026-10-17 再次查询 Fl API Hub、newapi-ai-check-in、Newapi-checkin 的默认分支，HEAD 分别仍为上表的 `94ef25b`、`a01c232`、`49089a5`。重点复核 [WONG 适配器](https://github.com/RebornQ/fl_api_hub/blob/94ef25b0381d54ab11d1631d569a5cca4451eb84/lib/core/network/adapters/wong_api_adapter.dart)、[前台调度器](https://github.com/RebornQ/fl_api_hub/blob/94ef25b0381d54ab11d1631d569a5cca4451eb84/lib/core/scheduler/foreground_scheduler.dart) 和 [Provider 配置](https://github.com/aceHubert/newapi-ai-check-in/blob/a01c232672686ce66ac06194a7f13b04ca734a28/PROVIDERS.json)。补充 WONG 的 `data.checked_in` 及不缓存状态查询；保留声明式已知接口。本项目独立实现按最近任务时间等待和保守的页面按钮识别，未照搬周期轮询，也未引入猜测接口或自动重放请求的逻辑。

## 必须修正的认识

1. **AgentRouter 查询余额不等于签到。** 按用户 2026-10-16 实测规则，08:00 后完成新的登录即签到。保留 `checked_in` 明确响应的解析；字段缺失时，以新会话完成登录且取得用户身份为规则依据，奖励金额仍未知。只在程序内丢弃旧站点认证，保留 WAF 和 OAuth 会话，不调用退出接口撤销日常浏览器使用的 Cookie。
2. **AnyRouter 不再套用普通签到接口。** 参考仓库仍配置 `/api/user/sign_in`，但本项目按用户确认的实际规则，在 08:00 后访问已登录页面并核验身份。普通 New API / WONG 仍调用 `/api/user/checkin`；不能因普通站点登录成功就宣告签到成功。
3. **HTTP 200 和正文包含 success 均不是成功证明。** Python 参考实现存在非 JSON 正文包含 success 就判成功的分支。本项目只解析结构化响应及严格的字段/已签到语义；登录页、HTML 错误页、验证码都不能算成功。
4. **$25 不是固定业务规则。** 奖励仅来自响应中的奖励字段；缺失显示“未提供”。余额快照单独记录，余额差额不计为签到奖励。额度换算由站点配置，不能混合不同单位求和。
5. **参考限制与产品计划分开。** 参考 README 记载 AnyRouter 可能按 24 小时间隔；用户要求每天成功一次。本项目统一按北京时间的自然日和随机窗口调度，站点若仍处于冷却期则如实记录，不将冷却拒绝视为可重试的网络故障。
6. **进程内定时器不等于可靠任务系统。** Fl 的重试计数与上次尝试时间保存在内存，而且到达上限时会删除计数；不能直接照用。我们的排队、运行状态、重试次数和下次执行时间写入 SQLite。
7. **浏览器不天然解决验证挑战。** 采用标准 Chromium + chromedp，正常页面操作和账号独立会话；验证码、二步验证、首次授权由用户通过人工登录界面处理。不会把未处理的挑战伪装成成功。
8. **OAuth 不能假设一直停留在原标签页。** 此版本 New API 登录使用同页跳转，绑定/验证有新窗口；实际 AgentRouter 的 LinuxDo 登录会新开页面。本程序同时支持同页、新页和关闭后的返回，并单独验证账号隔离。桌面人工登录直接使用可见浏览器，网页预览负责即时交互反馈。
9. **衍生站不能仅按项目来源认定完全兼容。** New API 当前主干还存在访问令牌及刷新会话模型，较早分支常用 Cookie / localStorage user。登录路径、认证形态、签到接口及奖励字段按实际站点核对，不把主干行为强加给已部署的旧分支。

## 采用与边界

| 设计 | 采用方式 | 本次范围 |
| --- | --- | --- |
| Provider 配置 | 分离“平台模板、具体站点、账号” | 同类站点无需改代码 |
| 站点适配器 | 小接口：执行签到、返回统一结果 | New API、AnyRouter、AgentRouter、CSS 页面流程 |
| 账号隔离 | 每次 HTTP Cookie jar / Chromium 会话独立 | 不读取用户日常浏览器配置 |
| 简单管理界面 | 账号、站点、记录、设置四个视图 | 不扩展为 API 密钥和渠道管理平台 |
| 调度 | 持久队列、每日一次、同源串行、窗口内恢复 | 一个数据目录只允许一个进程 |
| 双端运行 | Windows 薄壳与 Docker 服务使用同一 Go 应用 | 前端只依赖统一 HTTP API |
| 可维护性 | 领域规则、应用编排、基础设施、界面分层 | 适配器不直接访问数据库或界面 |

## 许可策略

Fl API Hub 与 New API 为 AGPL-3.0，本项目核对功能、架构和互操作协议，不复制其实现、界面资产或设计稿。两个 Python 项目为 BSD-2-Clause；协议兼容参考与完整 BSD 声明记入 `THIRD_PARTY_NOTICES.md`。本项目独立实现 Go/Vue 代码，第三方依赖按各自许可证分发；本项目自身尚未指定开源许可证。

新增的 aceHubert 项目为 BSD-2-Clause，Jasonliu-0 项目为 MIT。采用其接口协议和流程经验，以现有 Go 端口独立实现；未引入 Python、Camoufox、验证码代解服务或抽奖循环。薄荷使用 `up.x666.me` 独立会话，读取 `/api/checkin/status` 后至多提交一次 `/api/checkin/spin`，响应不明时只读复核。`can_spin=false` 且没有今日记录不会当成成功；额度按该项目公开示例的 500 额度/次显示，不冒充美元奖励。

## 验证口径

开源源码证明参考协议，不证明当前线上账号能够通过风控。自动化测试使用本地模拟站点和真实 Chromium；Windows 构建、Docker 构建、线上账号验收分别记录，互不替代。实际状态见 [进度与验收](progress.md)。
