---
layout: default
lang: zh
ref: marketplace
traduit_le: 2026-11-24
---
# pi-appliance 的 marketplace

marketplace 汇集了可以在 pi-appliance 控制台中安装和管理的应用。目录目前仍内置在程序中：提交的应用要等贡献通过审核、并发布新版本的控制台之后才能使用。

> **实验版本。** 镜像已在 Raspberry Pi 3 B+ 和 Raspberry Pi 4 上验证；每个应用仍需在真实的 Pi 上逐一测试。目前还不能从互联网访问服务。

## pi-appliance 底座

**pi-appliance 底座**（Docker、Traefik、系统服务和工具、控制台以及 `pia` 命令）随镜像提供：它不在目录中，也不能卸载。只有管理员（使用首次启动时创建的账户或其他管理员账户）才能在控制台中安装和管理下面的应用。

## 目录中已有的应用

<div id="marketplace-catalog" data-catalog-index="{{ '/static/pi-appliance-catalog.json' | relative_url }}">
<p id="marketplace-catalog-status" role="status">正在加载目录…</p>
<p id="marketplace-tags-help" hidden>点按一个或多个标签，只显示至少带有其中一个标签的应用；再点按一次即可取消。</p>
<div id="marketplace-tags" class="todo-filters" role="group" aria-label="按标签筛选" hidden></div>
<p id="marketplace-tags-empty" hidden>没有应用带有这些标签。</p>
<table id="marketplace-catalog-table" hidden>
<thead><tr><th>应用</th><th>说明</th><th>类别</th><th>标签</th><th>内存</th><th>磁盘</th></tr></thead>
<tbody></tbody>
</table>
</div>

应用的名称和说明来自各自的清单文件，目前只有法文。

此列表从目录 JSON 文件加载，该文件由 [`marketplace/`](https://github.com/gmoulard/pi-appliance/tree/master/marketplace) 中的清单生成。控制台读取 GitHub 仓库中的同一个文件：被接受的应用无需发布 pi-appliance 新版本即可出现在 Pi 上。每次修改清单或 `marketplace/planned.yaml` 后，请在仓库根目录运行 `go run ./cmd/marketplace-json` 重新生成该文件；否则测试（`go test ./...`）会失败。如果忘了，GitHub 的一个自动操作会在合并后于 `master` 上重新生成。文件名包含其内容的 MD5；索引 `docs/static/pi-appliance-catalog.json` 指明当前版本。

控制台可以管理多个实例、它们的启动和停止、可回退的更新，以及卸载时保留或删除数据。

## 计划中或研究中的应用

这些应用目前还不能从 marketplace 安装。列表来自同一个目录 JSON（`marketplace/planned.yaml`）：

<ul id="marketplace-planned" hidden></ul>

**内置于控制台、不在目录中**：[Cockpit](https://cockpit-project.org/)，用于管理 Pi 的系统（日志、程序、网络、磁盘以及浏览器中的终端，适合有经验的用户）。它不是容器，而是系统软件：控制台的 Marketplace 只在家中向管理员提供它，使用 pi-appliance 账户即可进入，无需另外的密码（见[规格说明]({{ '/specification.html' | relative_url }})（法文）第 6.4 节）。

数据库：已提供 PostgreSQL，并带有 Adminer 网页界面。MongoDB（仅 Pi 5）、Redis、MSSQL（没有适用于 Pi 的镜像）和 SQLite（一个简单的文件）不单独提供：见[规格说明]({{ '/specification.html' | relative_url }})（法文）第 6.3 节。

Etherpad 的公开分享链接也是计划中的改进。请查看[路线图]({{ '/TODO.html' | relative_url }})（法文）了解进展。

## 添加应用：人人都可以贡献

欢迎贡献。您可以推荐一款在家中有用的自由应用（目前唯一的例外：n8n，采用 *fair-code* 许可“Sustainable Use License”，由项目特意选用并在其介绍中注明），改进现有清单，或报告问题。提交通过 pull request 进行；合并前会经过审阅和测试。

### 1. 创建 Docker Compose 清单

fork [pi-appliance 仓库](https://github.com/gmoulard/pi-appliance)，然后创建 `marketplace/<标识>/compose.yaml`。标识必须为小写，以字母开头，只能包含字母、数字和连字符。

该文件是普通的 Docker Compose 清单，带有一个描述应用的 `x-pi-appliance` 部分。结构示例（请把其中的值和镜像换成您的应用的）：

```yaml
x-pi-appliance:
  name: 我的应用
  description: 一段面向用户的清晰说明。
  icon: M4 5h16v14H4z
  category: Outils
  tags: [administration]
  license: AGPL-3.0
  subdomain: outil
  service: app
  port: 8080
  memory_mb: 256
  disk_mb: 512
  login: application
  access: users
  parameters:
    - name: ADMIN_PASSWORD
      label: 管理员密码
      generate: true

services:
  app:
    image: editeur/image:1.2.3
    restart: unless-stopped
    environment:
      ADMIN_PASSWORD: ${ADMIN_PASSWORD}
    volumes:
      - data:/var/lib/application

volumes:
  data:
```

### 2. 遵守目录规则

- 选择带有**确切版本**（不要用 `latest` 或 `stable`）且兼容 `linux/arm64`（目标 Raspberry Pi 的架构）的 Docker 镜像。
- 在 `service` 中声明对外发布的服务，在 `port` 中声明其内部端口，并在 `memory_mb` 和 `disk_mb` 中给出以 MB 为单位的合理估计。
- `icon` 字段是 24 × 24 网格上的 SVG 路径（`d`）。`login` 字段接受 `application` 或 `console`；`access` 接受 `public`、`users`、`home` 或 `admins`（管理员、仅在家中：保留给可以控制整台 Pi 的应用，例如 Portainer 或 Claude Code）。
- 在 `parameters` 中用大写声明参数。对于机密信息，请使用 `generate: true`，不要把密码写进清单或镜像。
- 把需要长期保存的数据放在命名的 Docker 卷中。不要把机密、个人数据或某个用户专用的配置放进仓库。绝不能备份的卷（某个账户的令牌）要加上标签 `pi-appliance.backup: "false"`（卷的 `labels`，参照 Claude Code）。
- 注明应用的许可证，并确认其再分发与本项目兼容。
- 在 `x-pi-appliance` 中只使用有文档说明的键：未知的键会导致清单被拒绝。
- 避免会获得整台 Pi 控制权的选项（特权容器、访问 Docker、Pi 上的文件夹、主机网络、额外权限）：带有这些选项的新应用无法从在线目录安装，只能在 pi-appliance 发布新版本之后安装。可以访问 Docker 或作为特权容器的应用还必须声明 `access: admins`，控制台会要求确认后才安装。
- 已计划但尚未就绪的应用放在 `marketplace/planned.yaml` 中（名称、说明、图标、类别以及路线图中的条目）：网站会将其显示为“即将推出”。

字段的完整定义和校验规则见[规格说明第 6.5 节]({{ '/specification.html' | relative_url }})（法文）。

### 3. 检查并提交

在仓库根目录运行：

```shell
go run ./cmd/marketplace-json
go test ./...
```

测试主要检查清单是否有效、镜像是否有固定版本以及 Compose 变量是否已声明。如有可能，也请在测试机器上用 Docker 试一试应用的安装、停止、重启和卸载。

然后向 `master` 提交 pull request，附上应用说明、所需资源、测试步骤以及已知的限制。pull request 在 `master` 上被接受后，各台设备会在 6 小时内读取新的在线目录，无需 pi-appliance 新版本。

## 实用链接

- [marketplace 的源代码](https://github.com/gmoulard/pi-appliance/tree/master/marketplace)
- [清单的规格说明]({{ '/specification.html' | relative_url }})（法文）
- [路线图]({{ '/TODO.html' | relative_url }})（法文）
- [pi-appliance 仓库](https://github.com/gmoulard/pi-appliance)：从您的 fork 提交 pull request 来提出您的贡献。
