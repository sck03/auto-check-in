# 签到台 · Auto Check-In @VERSION@

## Windows

解压后双击 `AutoCheckIn.exe`。需要已安装 Edge 或 Chrome，无需安装 Go 或 Node。

在“站点管理”确认地址，在“账号签到”添加账号。“浏览器登录”会打开独立浏览器窗口，完成登录/授权后回到程序保存会话；登录方式应与实际使用的密码、GitHub 或 LinuxDo 一致。手动签到一次核对结果后保留自动计划。设置页和托盘右键菜单可控制最小化到托盘、关闭窗口后继续运行及开机运行。

默认数据目录为程序旁的 `data`，数据库是 `data/checkin.db`；设置页显示实际路径。请将程序解压到有写权限的新目录，重新添加站点和账号。

本版按全新安装处理，不兼容旧数据库和旧模板配置，不再自动导入旧目录。`-data` 或 `CHECKIN_DATA_DIR` 可指定空数据目录。同版本数据支持重开及加密备份恢复。

平台模板为 AnyRouter、AgentRouter、薄荷 API 和 New API（通用）。WONG 选择通用模板的接口签到；网页按钮签到也在通用模板内配置。

默认自动签到与重试均在北京时间 08:00–09:00 内进行，成功后当天停止。保存会话或结束签到后，账号浏览器窗口会关闭。

`checkin-server.exe` 是可选的常驻服务程序，使用 `.\checkin-server.exe -open` 启动。桌面程序与服务程序不能同时使用同一数据目录。

## Linux 服务

安装 Chromium 或 Chrome 后运行 `./checkin-server -data ./data`，访问 `http://127.0.0.1:18990`。其他配置见下方运行说明；容器用户可从发布页面下载已包含 Chromium 的 Docker 镜像。

## 在线文档

- [运行、配置、备份与故障处理](https://github.com/sck03/auto-check-in/blob/main/docs/operations.md)
- [架构与模块设计](https://github.com/sck03/auto-check-in/blob/main/docs/architecture.md)
- [完成进度与验收范围](https://github.com/sck03/auto-check-in/blob/main/docs/progress.md)
- [已发布版本与校验和](https://github.com/sck03/auto-check-in/releases)

本包中的 `THIRD_PARTY_NOTICES.md` 合并保留参考项目、Go 和应用依赖的版本及完整许可文本。
