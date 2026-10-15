# 签到台 · Auto Check-In @VERSION@

## Windows

解压后双击 `AutoCheckIn.exe`。需要已安装 Edge 或 Chrome，无需安装 Go 或 Node。

在“站点管理”确认地址，在“账号签到”添加账号。“浏览器登录”会打开独立浏览器窗口，完成登录/授权后回到程序保存会话；登录方式应与实际使用的密码、GitHub 或 LinuxDo 一致。手动签到一次核对结果后保留自动计划。设置页和托盘右键菜单可控制最小化到托盘、关闭窗口后继续运行及开机运行。

默认数据目录：`%LOCALAPPDATA%\AutoCheckIn`。升级前退出旧程序并保留数据目录。

`checkin-server.exe` 是可选的常驻服务程序，使用 `.\checkin-server.exe -open` 启动。桌面程序与服务程序不能同时使用同一数据目录。

## Linux 服务

安装 Chromium 或 Chrome 后运行 `./checkin-server -data ./data`，访问 `http://127.0.0.1:18990`。其他配置见下方运行说明；容器用户可从发布页面下载已包含 Chromium 的 Docker 镜像。

## 在线文档

- [运行、配置、备份与故障处理](https://github.com/sck03/auto-check-in/blob/v@VERSION@/docs/operations.md)
- [架构与模块设计](https://github.com/sck03/auto-check-in/blob/v@VERSION@/docs/architecture.md)
- [完成进度与验收范围](https://github.com/sck03/auto-check-in/blob/v@VERSION@/docs/progress.md)
- [发布页面与校验和](https://github.com/sck03/auto-check-in/releases/tag/v@VERSION@)

本包中的 `THIRD_PARTY_NOTICES.md` 合并保留参考项目、Go 和应用依赖的版本及完整许可文本。
