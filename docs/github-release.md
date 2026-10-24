# GitHub 构建与发布

目标仓库：[sck03/auto-check-in](https://github.com/sck03/auto-check-in)。

## 工作流

**Verify**：main 推送、Pull Request 或手动触发。验证 Windows / Linux 的 Go 与前端构建，Linux 运行竞态测试；另外构建实际容器，以非 root、只读根文件系统运行 Chromium 登录/签到/重启持久化测试。

**Release**：推送 `v*` 标签或在 Actions 手动输入版本标签。版本格式如 `v1.0.2`。

唯一版本源为 `internal/buildinfo/VERSION`。普通 Go 构建直接嵌入该文件；Windows 脚本默认读取它；Docker 未传 VERSION 时也使用嵌入值，Compose 本地镜像命名为 `auto-check-in:local`。前端是私有包，界面版本只使用后端 `/meta`，不再维护重复版本号。

Release 首先检出指定标签，核对标签与该标签源码中的 VERSION 一致；Windows 和 Docker 均构建这个标签。手动触发须指定已经存在的标签，不会把当前 main 内容误标为旧版本。更新版本文件、完成 Verify 后再建立发布标签；推送 main 本身不会创建 Release。

产物：

- `AutoCheckIn-版本-windows-amd64.zip`：`AutoCheckIn.exe`、`checkin-server.exe`、简短 `README.md`、合并后的 `THIRD_PARTY_NOTICES.md`。
- `auto-check-in-版本-linux-amd64.tar.gz`：`checkin-server` 及同样的两个说明文件。
- `ghcr.io/sck03/auto-check-in:版本`：linux/amd64 与 linux/arm64 镜像，稳定版同时更新 `latest`。
- `auto-check-in-版本-docker-linux-amd64.tar.gz`：可用 `docker load` 导入的 Docker 离线成品。
- `ALL-SHA256SUMS.txt`：以上下载文件的校验和。

程序压缩包使用明确的文件清单生成，旧构建目录中的 `docs/`、`licenses/` 或其他文件不会进入成品。完整架构、进度和运行文档留在仓库，包内 README 链接到对应版本的在线文档。合并后的第三方声明保留每个依赖的版本及完整许可文本。

Linux tar 包通过 Go 标准库生成，服务程序固定为 `0755`，说明文件为 `0644`，不继承 Windows 构建机器的文件权限。

Docker 内的应用声明也合并为 `/usr/share/doc/auto-check-in/THIRD_PARTY_NOTICES.md`；系统软件包自带的版权和许可文件继续保留。

只有 Windows 构建和容器验证都通过后才创建 GitHub Release。镜像发布使用工作流自己的 GITHUB_TOKEN，权限仅在发布 job 开放 contents:write / packages:write，不需要在代码中保存令牌。GHCR 首次创建包的可见性受 GitHub 包设置控制；离线镜像附件也可直接下载。

## 触发

```bash
VERSION="$(tr -d '\r\n' < internal/buildinfo/VERSION)"
git tag "v$VERSION"
git push origin "v$VERSION"
```

也可在 [Release 工作流](https://github.com/sck03/auto-check-in/actions/workflows/release.yml) 选择 Run workflow，输入标签。正式更新使用新的版本号；不要覆盖已发布标签。

## 使用成品

```bash
docker pull ghcr.io/sck03/auto-check-in:latest
docker run -d --name auto-check-in --restart unless-stopped \
  -p 127.0.0.1:18990:18990 -v checkin-data:/data \
  ghcr.io/sck03/auto-check-in:latest

# 离线导入：从 Release 下载对应文件后替换文件名
docker load -i auto-check-in-版本-docker-linux-amd64.tar.gz
```

Windows 解压 ZIP 后运行 AutoCheckIn.exe。发布包未进行代码签名。

## Actions 版本基线

所有 Linux job 固定使用 **`ubuntu-26.04`**。GitHub 已宣布 Ubuntu 26.04 正式可用；`ubuntu-latest` 从 2026-10-19 起逐步迁移，见[官方公告](https://github.com/actions/runner-images/issues/14748)。显式标签避免迁移提示及系统大版本自动漂移；历史运行中的提示不会被追溯清除。Docker 的 Debian 基础镜像版本与 Actions 宿主机分别管理。

当前稳定版 actionlint 1.7.12 的内置运行器清单尚未收录该标签，`.github/actionlint.yaml` 仅补充这个已正式提供的标签；实际工作流仍使用 GitHub 托管运行器。

2026-10-15 逐个查询 GitHub `releases/latest`，确认不是 draft/prerelease，再解析标签对应提交。工作流使用完整提交 SHA，注释保留当前最新稳定版本。锁定结果保存在 `.github/action-versions.json`。

| Action | 核对时最新稳定版 |
| --- | --- |
| actions/checkout | v7.0.1 |
| actions/setup-go | v7.0.0 |
| actions/setup-node | v7.1.0 |
| actions/upload-artifact | v7.0.2 |
| actions/download-artifact | v8.0.2 |
| docker/setup-qemu-action | v4.4.0 |
| docker/setup-buildx-action | v4.4.1 |
| docker/login-action | v4.6.0 |
| docker/metadata-action | v6.2.0 |
| docker/build-push-action | v7.4.0 |
| softprops/action-gh-release | v3.0.3 |

构建工具链为 Go 1.27.2、Node 24.18.0 LTS、GoVersionInfo 1.7.0。依赖与 Actions 更新应重新核对稳定发布、更新固定版本/提交并运行验证，不使用漂移的 `@main` 或未经确认的预发布版本。
