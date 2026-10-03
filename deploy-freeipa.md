# Ubuntu 24.04 克隆安装说明

更新日期：2026-10-04。适用于包含 FreeIPA 更新的 One API 源码；安装时使用 `FreeIPA` 分支（区分大小写）。

## 1. 安装范围

**新增 FreeIPA 功能目前仅通过了克隆源码安装方式的验证**。已执行的环境为 Ubuntu 24.04 x86_64、Go 1.27.1、Node.js 26.9.0、npm 11.19.1，使用默认主题、SQLite 和本机 `go run .` 启动方式。Docker、Docker Compose、上游预编译包及其他主题的完整安装流程尚未验证。

本项目是在原 One API 上增加 FreeIPA 接入的扩展。常驻部署沿用作者的 **PM2 + Nginx + Certbot** 方案，通用安装、反向代理及 HTTPS 步骤参见[作者部署教程](https://justsong.cn/page/how-to-deploy-a-website)。本文补充本分支源码安装和 FreeIPA 所需的差异，不替代原项目的通用部署与参数说明。PM2 单实例管理已在本机完成部署，端口由 `.env` 的 `PORT` 决定；开机自启尚待管理员执行配置。Nginx 与 Certbot 的组合仍未完成本机部署验证，首次安装先以前台方式确认启动和登录。

当前开发分支 `feature/ipa-account-system` 的内容将合并到 `FreeIPA` 分支，以下安装命令以 `FreeIPA` 为安装入口。合入原作者的 `main` 需经原作者同意。2026-10-04 核对时，新增功能仍在本地开发工作区；实际克隆前应确认维护者已将所需更新合并并发布到目标仓库的 `FreeIPA` 分支。

## 2. 准备系统与工具

工具按 Ubuntu 或工具官方文档的常规方式安装，安装位置与项目克隆位置无关。已有可用工具时直接复用：

```bash
command -v git curl gcc make go node npm nginx pm2 certbot
```

SQLite 驱动需要 CGO 和 C 编译器。Ubuntu 基础依赖可按默认位置安装：

```bash
sudo apt update
sudo apt install -y git curl ca-certificates build-essential xz-utils
```

| 工具 | 常规安装方式与位置 |
| --- | --- |
| Go | 按 [Go 官方安装文档](https://go.dev/doc/install)安装，Linux 官方发行包通常安装到 `/usr/local/go`，并将 `/usr/local/go/bin` 加入 PATH；已有安装时按官方升级步骤处理 |
| Node.js 与 npm | 沿用作者通过 nvm 安装的方式，使用 nvm 默认目录 `~/.nvm`；版本选择参考 [Node.js 官方下载页面](https://nodejs.org/en/download)。本功能不要求定制安装目录 |
| PM2 | 在所选 Node.js 环境执行 `npm install -g pm2`，使用 npm 默认的全局安装位置；nvm 环境下通常不需要 sudo |
| Nginx | 通过 Ubuntu 软件包安装：`sudo apt install nginx`，站点配置沿用 `/etc/nginx/` |
| Certbot | 按 [Certbot 官方 Ubuntu/Nginx 安装指引](https://certbot.eff.org/instructions?ws=nginx&os=snap)安装，使用其默认位置及续期机制；网站证书通常保存在 `/etc/letsencrypt/` |

### 2.1 安装 Node.js、npm 与 PM2

已有 Node.js 和 npm 时可以保留现有环境，直接安装 PM2。以下沿用作者的 nvm 方式，安装到其默认目录，使用已用于本次构建的 Node.js 版本：

```bash
curl -fsSL https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.8/install.sh | bash
# 安装后新开终端，或在当前 Bash 终端加载 nvm。
export NVM_DIR="${XDG_CONFIG_HOME:-$HOME}/.nvm"
. "$NVM_DIR/nvm.sh"
nvm install 26.9.0
nvm alias default 26.9.0
nvm use 26.9.0
npm install -g pm2@7.0.4
command -v node npm pm2
```

以上 nvm 安装方式见 [nvm 官方说明](https://github.com/nvm-sh/nvm#installing-and-updating)。PM2 7.0.4 已用于本次服务部署；这里固定版本方便复现。nvm 安装的 npm 全局工具属于当前用户，不需要 `sudo npm install`。后续构建、PM2 启动和开机服务配置均使用同一个普通部署用户。

安装后确认构建工具可用：

```bash
go version
node --version
npm --version
```

第 1 节列出的版本是已验证环境的记录；其他版本的完整安装过程尚未验证。Go、Node.js 或 PM2 不必放进项目目录，FreeIPA 接入也不需要单独准备 Python 环境。

## 3. 克隆 FreeIPA 分支

将仓库地址替换为已发布 `FreeIPA` 分支的实际地址。当前工作区的来源仓库为 `YingzhiLin/one-api`，安装时应明确选择 `FreeIPA` 分支。

项目可以放在任意具有读写权限的目录。以下先设置项目的**绝对路径**，替换示例后再执行；路径包含空格时保留双引号。后续命令统一使用 `ONE_API_DIR`：

```bash
export ONE_API_DIR='/你选择的目录/one-api'
mkdir -p "$(dirname "$ONE_API_DIR")"
ONE_API_REPOSITORY='https://github.com/YingzhiLin/one-api.git'
git ls-remote --heads "$ONE_API_REPOSITORY" FreeIPA
git clone --branch FreeIPA --single-branch \
  "$ONE_API_REPOSITORY" "$ONE_API_DIR"
cd "$ONE_API_DIR"
git branch --show-current
```

后续命令在同一终端执行；新开终端时重新设置 `ONE_API_DIR` 为实际项目根目录。克隆目标目录应为空或尚不存在；已有安装从其项目根目录继续，不重新克隆覆盖。

`git ls-remote` 应返回 `refs/heads/FreeIPA`，克隆后 `git branch --show-current` 应输出 `FreeIPA`。若远端没有该分支，应先确认维护者已发布。还应核对所需开发改动已合并，以及源码中的 `common/ipa/`、`.env.example` 中的 `IPA_ENABLED`；分支存在不代表已经包含全部更新。合并前的开发验证可使用 `feature/ipa-account-system`，其余安装步骤相同。

## 4. 构建默认前端

后端通过 Go embed 将前端资源编入程序，必须先构建前端。以下直接输出到后端需要的 `web/build/default`：

```bash
cd "$ONE_API_DIR/web/default"
npm install
BUILD_PATH=../build/default GENERATE_SOURCEMAP=false \
  node node_modules/react-scripts/scripts/build.js
cd "$ONE_API_DIR"
```

构建结果应包含 `web/build/default/index.html`。本分支验证使用 `THEME=default`；仅构建默认主题时不要选择 `air` 或 `berry`。

若遇到 `Cannot find module 'ajv/dist/compile/codegen'`，本机已使用以下命令修复依赖树，再执行上述构建命令：

```bash
cd "$ONE_API_DIR/web/default"
npm install --no-save --no-package-lock 'ajv@^8.8.2'
BUILD_PATH=../build/default GENERATE_SOURCEMAP=false \
  node node_modules/react-scripts/scripts/build.js
cd "$ONE_API_DIR"
```

已有的 ESLint 告警不一定代表构建失败，应检查命令退出状态及生成的资源。前端源码修改后，需要重新构建前端并重新编译或启动后端。

## 5. 准备 FreeIPA

在启动前准备以下资料：

| 资料 | 用途 |
| --- | --- |
| 可解析的 IPA 域名及 LDAP 服务端口 | 连接目录，域名需与服务证书匹配 |
| Base DN 和用户目录 DN | 限定查询范围 |
| 专用 LDAP bind DN 与密码 | 查询用户属性及组成员身份 |
| 管理员、超级管理员组 DN | 授予 One API 的管理角色 |
| IPA CA 证书或 CA 证书链 | 验证内部证书 |

绑定账号需要能读取匹配用户的 `uid`、`cn`/`displayName`、`mail`、`ipaUniqueID`、`memberOf` 和 `nsAccountLock` 等属性。本系统不需要通过该账号修改 IPA 密码或账号。

应用角色组可使用 FreeIPA 非 POSIX 组，例如 `one-api-admins` 和 `one-api-roots`。代码依据用户 `memberOf` 中的组 DN 判断角色；首次安装先准备至少一个直接属于应用超级管理员组的员工账号，并确保其 uid 匹配 `IPA_USER_MATCH`。不要依赖 IPA 的 `admin` 用户名自动取得本系统管理员资格。

管理员通过 FreeIPA Web UI 维护用户、密码、组成员和 IPA 锁定状态。本系统的本地“启用/禁用”仅控制应用使用资格；IPA 与本地任一侧禁用都不可用。

## 6. 放置 CA 并配置 .env

在项目根目录创建运行目录，从 IPA 管理员取得可信的 PEM CA 文件：

```bash
cd "$ONE_API_DIR"
mkdir -p cert data logs
cp /可信来源/ipa-ca.crt cert/ca.crt
cp .env.example .env
chmod 600 .env
```

已有 `.env` 时直接编辑，避免复制示例覆盖原配置。证书示例使用相对路径，因此启动时工作目录必须是项目根目录。

以下是新安装的 SQLite + 纯 IPA 模式配置示例。域名、DN、密码和会话密钥均需替换为自己的值。端口 3000 仅为示例，实际以 `.env` 的 `PORT` 为准，后面的访问地址和 Nginx 上游端口应同步调整。已有安装应保留原数据库与会话密钥，在原 `.env` 中增加 IPA 配置，升级前先阅读[数据库升级说明](./database-freeipa-upgrade.md)：

```dotenv
PORT=3000
DEBUG=false
THEME=default

# 不需要出站 HTTP 代理时留空。
HTTPS_PROXY=

# 固定密钥让正常重启后的会话保持可验证。
SESSION_SECRET=REPLACE_WITH_RANDOM_SESSION_SECRET

# SQLite 路径相对于项目根目录；本示例不启用 Redis。
SQL_DSN=
SQLITE_PATH=./one-api.db
REDIS_CONN_STRING=

REGISTER_ENABLED=false
IPA_ENABLED=true
IPA_ONLY=true
IPA_URL=ldaps://ipa.example.com:636
IPA_STARTTLS=false
IPA_BASE_DN=dc=example,dc=com
IPA_USER_BASE_DN=cn=users,cn=accounts,dc=example,dc=com

# h* 只允许 h 开头的 IPA uid；* 匹配全部。
IPA_USER_MATCH=h*
IPA_ADMIN_GROUP_DN=cn=one-api-admins,cn=groups,cn=accounts,dc=example,dc=com
IPA_ROOT_GROUP_DN=cn=one-api-roots,cn=groups,cn=accounts,dc=example,dc=com

IPA_BIND_DN=uid=app_bind_oneapi,cn=users,cn=accounts,dc=example,dc=com
IPA_BIND_PASSWORD="REPLACE_WITH_REAL_BIND_PASSWORD"
IPA_CA_CERT=./cert/ca.crt
IPA_SYNC_FREQUENCY=300
```

可使用 `openssl rand -hex 32` 生成会话密钥，然后填入 `SESSION_SECRET`。每次启动都更换密钥会导致旧浏览器会话失效。不要把示例占位符作为真实密钥或密码使用。

| 配置 | 说明 |
| --- | --- |
| `PORT` | 网页、管理 API 和模型 API 共用的端口；服务监听所有网卡 |
| `REGISTER_ENABLED` | `false` 关闭公开注册；纯 IPA 模式还会禁止非 IPA 账号创建 |
| `IPA_ENABLED` | 默认 `false`；启用 IPA 登录。`IPA_ONLY=true` 时也会强制启用 IPA |
| `IPA_URL` | LDAP 连接地址；推荐 `ldaps://域名:636`，域名须与证书匹配 |
| `IPA_BASE_DN` | IPA 目录的 Base DN，例如 `dc=example,dc=com` |
| `IPA_USER_BASE_DN` | 用户查询范围；未设置时使用 `IPA_BASE_DN` |
| `IPA_BIND_PASSWORD` | 专用查询账号密码，保存在 `.env`，不要放入 PM2 配置或版本库 |
| `IPA_STARTTLS` | 默认 `false`；使用 `ldap://域名:389` 时设置为 `true` |
| `IPA_ONLY` | `true` 只允许 IPA 身份；列表和搜索过滤本地 root 等本地账号，空数据库也不创建本地 root |
| `IPA_USER_MATCH` | 按登录名 uid 匹配，忽略大小写；只有 `*` 为通配符，空值按 `*`；管理员也必须匹配 |
| `IPA_BIND_DN` | 专用查询账号；该账号始终从应用账号中排除，即使配置匹配 `*` |
| `IPA_ADMIN_GROUP_DN` | 应用管理员组；管理员访问时会向 IPA 核验身份和成员资格 |
| `IPA_ROOT_GROUP_DN` | 应用超级管理员组；未配置时不会通过该组授予超级管理员 |
| `IPA_CA_CERT` | PEM CA 文件路径；空值使用系统信任库，内部 CA 应明确提供 |
| `IPA_SYNC_FREQUENCY` | 目录同步间隔，单位秒；启动时也会同步，列表翻页只读取本地数据 |

### 账号模式与已有账号

| 配置 | 旧本地账号登录与使用 | 管理权限 |
| --- | --- | --- |
| `IPA_ENABLED=false`，`IPA_ONLY=false` | 保留原有本地账号机制 | 保留原本地管理员机制 |
| `IPA_ENABLED=true`，`IPA_ONLY=false` | 允许已有本地账号与 IPA 账号使用 | 管理接口要求 IPA 身份及对应应用角色组资格 |
| `IPA_ONLY=true` | 仅允许 IPA 身份，本地账号不能登录或使用 | 由 IPA 应用角色组授予，IPA 登录同时被强制启用 |

`IPA_ONLY` 默认 `false`，不设置也不会强制仅使用 IPA。已有本地账号继续使用的前提是账号未禁用且系统允许本地密码登录，原额度和令牌保持在原账号上。`REGISTER_ENABLED=false` 仅关闭新用户公开注册，不影响已有账号登录。

### 迁移期间独立禁止注册

迁移原数据库时，可以允许旧本地账号登录，同时禁止新用户自行注册，无需启用纯 IPA 模式：

```dotenv
REGISTER_ENABLED=false
IPA_ONLY=false
```

`IPA_ENABLED` 按实际需要保留：`false` 使用原本地账号机制，`true` 允许 IPA 与本地账号混合使用。`REGISTER_ENABLED=false` 独立生效，后台不能通过注册开关覆盖这一部署限制；它阻止公开密码注册，以及 GitHub、OIDC、飞书、微信等登录入口首次创建账号。已存在账号的登录不因此关闭，IPA 目录同步和 IPA 身份首次映射仍可执行，管理员手工创建本地账号也仍受原有管理权限及 `IPA_ONLY` 限制。

修改配置后重启应用：PM2 部署执行 `./scripts/pm2.sh restart one-api`，systemd 部署执行 `sudo systemctl restart one-api`。恢复公开注册时将 `REGISTER_ENABLED=true`，还需确保后台注册开关已开启；它不是强制打开注册的开关。

**启用 IPA 会改变管理资格。** 当前实现中，即使 `IPA_ONLY=false`，旧本地 `admin/root` 也不能凭原有角色访问管理接口。切换前应先准备符合 `IPA_USER_MATCH` 且直接属于 `IPA_ROOT_GROUP_DN` 指定组的 IPA 用户，避免切换后没有可用的应用超级管理员。账号同名不会自动合并，已有安装的身份关联与数据保留要求见[数据库升级说明](./database-freeipa-upgrade.md)。

下列原项目参数继续沿用，用于说明上述示例和迁移时的路径关系：

| 配置 | 本示例中的用法 |
| --- | --- |
| `SQL_DSN` | 留空使用 SQLite；已有 MySQL/PostgreSQL 安装保留原配置 |
| `SQLITE_PATH` | 默认 `one-api.db`；示例 `./one-api.db` 位于项目根目录。也可使用其他相对或绝对路径，父目录须已存在；升级时必须指向原数据库 |
| `SESSION_SECRET` | 保留固定密钥，避免正常重启后会话失效 |
| `THEME` | 本分支验证使用 `default` |
| `REDIS_CONN_STRING` | 本示例留空；已有 Redis 配置继续沿用 |
| `HTTPS_PROXY` | 出站 HTTP/HTTPS 代理，不用于网站 HTTPS 或 IPA 目录连接 |

其他原项目环境变量见 [README 环境变量说明](./README.md#环境变量)，无需因 FreeIPA 接入重新设置。

使用 StartTLS 时，改为 `IPA_URL=ldap://ipa.example.com:389` 和 `IPA_STARTTLS=true`。普通明文 LDAP 不被允许；内部证书通过 CA 信任配置处理。

`HTTPS_PROXY` 用于支持环境代理的出站 HTTP/HTTPS 请求，不是服务监听地址，也不会代理 LDAP。没有可用代理时应清空示例中的 `http://localhost:7890`。模型转发与用户内容获取的专项代理还可分别使用 `RELAY_PROXY`、`USER_CONTENT_REQUEST_PROXY` 配置。

程序会从**当前工作目录**自动读取 `.env`。启动前已导出的同名环境变量优先于文件；无需执行 `source .env`，它是 dotenv 配置文件，不是 shell 脚本。调整账号匹配、CA、端口等配置后需重启。

已有安装迁移时，保持原来的 `SQLITE_PATH`。例如已有数据在项目根目录 `one-api.db`，应设置 `SQLITE_PATH=./one-api.db`；改成新的路径会创建空数据库。

## 7. 启动与检查

先下载 Go 依赖并以前台方式启动：

```bash
cd "$ONE_API_DIR"
go mod download
CGO_ENABLED=1 go run .
```

看到数据库迁移完成、服务启动日志后，在另一个终端检查：

```bash
curl --noproxy '*' -f http://127.0.0.1:3000/api/status
curl --noproxy '*' -I http://127.0.0.1:3000/login
ss -ltnp | grep ':3000'
```

纯 IPA 配置下，状态响应应包含 `ipa_login=true`、`ipa_only=true`、`registration_enabled=false`。监听日志可能显示 localhost，但实际监听全部网卡。其他电脑使用 `http://服务器局域网地址:3000/login`；客户端网络和防火墙也须允许该端口。

使用员工的 IPA 登录名（例如 `h320001`）与其密码登录，账号密码修改仍在 FreeIPA 完成。纯 IPA 模式不使用本地 root 登录。

管理员登录后检查用户列表和组角色，配置模型渠道、分配额度，然后创建模型调用令牌。客户端配置 OpenAI 兼容 Base URL 为 `http://服务器地址:3000/v1`，API Key 填写该令牌；`/api/` 是管理接口，IPA 密码和 bind 密码均不能作为模型 API Key。

可以在项目根目录生成可执行文件，用于后续常驻运行：

```bash
CGO_ENABLED=1 go build -trimpath -o one-api .
./one-api
```

同一个数据库只运行一个该应用进程。前台 `go run` 用 Ctrl+C 停止后，再启动可执行文件。源码或前端更新后需要重新构建。

## 8. 沿用作者的 PM2 + Nginx + Certbot 方案

按[作者教程](https://justsong.cn/page/how-to-deploy-a-website)安装并配置 PM2、Nginx、Certbot。作者教程以 Ubuntu 20.04 和另一个 Go 应用为示例；本分支平台为 Ubuntu 24.04，程序名称改为 `one-api`，后端端口以本项目 `.env` 中的 `PORT` 为准。本例为 3000。工具按第 2 节的常规方式安装，配置目录使用各工具的默认位置；项目源码可位于任意目录。不直接照搬教程的旧版工具下载命令。

### 8.1 PM2：部署与启动

完成第 2.1 节的 PM2 安装、第 7 节的 Go 可执行文件构建，并停止前台实例后，以普通部署用户在项目根目录执行。升级已有服务时先按[数据库升级说明](./database-freeipa-upgrade.md)停止旧服务并备份：

```bash
cd "$ONE_API_DIR"
mkdir -p logs
./scripts/pm2.sh start ecosystem.config.js
./scripts/pm2.sh save
./scripts/pm2.sh status
```

`ecosystem.config.js` 自动以其所在目录为工作目录，启动项目中的 `one-api` 可执行文件，不设置 `PORT` 或数据库等环境变量；程序直接读取 `.env`。`scripts/pm2.sh` 将 PM2 状态保存在项目的 `.runtime/pm2` 中，与其他 PM2 服务隔离，并清除启动终端中的应用配置覆盖。后续管理也使用该脚本。

### 8.2 配置开机自启

服务正常启动后，在同一个部署用户的终端执行：

```bash
cd "$ONE_API_DIR"
./scripts/enable-pm2-startup.sh
sudo systemctl start pm2-one-api
sudo systemctl is-enabled pm2-one-api
sudo systemctl status pm2-one-api --no-pager
```

`enable-pm2-startup.sh` 保存进程列表并通过 sudo 配置 `pm2-one-api` 开机服务，需要部署用户输入 sudo 密码。Go 程序使用单实例，不使用 PM2 cluster。启动配置将记录绝对路径，移动文件夹到新电脑后需重新创建。使用 nvm 升级 Node.js 后，应在选定版本下重新安装 PM2，并更新开机服务配置，避免服务仍引用旧 Node 路径。PM2 安装、配置及启动保存的通用说明见 [PM2 官方文档](https://pm2.keymetrics.io/docs/usage/startup/)。

### 8.3 日常管理与配置修改

```bash
cd "$ONE_API_DIR"
./scripts/pm2.sh status
./scripts/pm2.sh logs one-api --lines 100
./scripts/pm2.sh restart one-api
./scripts/pm2.sh stop one-api
```

修改 IPA 配置后，使用 `./scripts/pm2.sh restart one-api`；查看运行状态和日志使用 `./scripts/pm2.sh status`、`./scripts/pm2.sh logs one-api --lines 100`，停止使用 `./scripts/pm2.sh stop one-api`。程序会重新读取工作目录中的 `.env`；本项目启动脚本不会传入端口等配置覆盖。若绕过该脚本直接启动，外部已设置的同名环境变量仍会优先。bind 密码放在受保护的 `.env` 中，CA 放在 `cert/ca.crt`，不必将密码复制到 PM2 命令或配置。

### 8.4 Nginx 与 Certbot：网站 HTTPS 保持原方案

Nginx 将网站页面、`/api/` 和 `/v1/` 转发到 `http://127.0.0.1:3000`；FreeIPA 登录仍经过同一个应用入口，无需另设 LDAP 反向代理。按作者方案用 Certbot 申请并续期网站证书，由 Nginx 提供 HTTPS。域名、证书联系邮箱、DNS 解析和 80/443 的可达性应使用实际部署资料。

对 One API 流式模型响应，Nginx 应关闭代理缓冲（`proxy_buffering off;`），并按模型耗时设置读取超时；参见 [Nginx 代理配置文档](https://nginx.org/en/docs/http/ngx_http_proxy_module.html)。本分支应用仍监听所有网卡；生产部署时由防火墙限制后端端口，仅通过 Nginx 入口访问。

部署后员工访问 `https://实际域名/login`，模型客户端使用 `https://实际域名/v1`。应用后台的服务器地址也应填写对外 HTTPS 地址。

网站 HTTPS 证书与 `IPA_CA_CERT` 是两套用途：前者供浏览器和模型客户端验证网站，后者供应用验证 FreeIPA 的 LDAPS 服务。不要用网站证书替换 IPA CA。仅内网可达的网站不能直接照搬公网 HTTP 验证流程，应根据 DNS 条件选择 Certbot DNS 验证或企业证书；参见 [Certbot 官方说明](https://certbot.eff.org/instructions?ws=nginx&os=pip)。

## 9. Ubuntu 24.04：使用 systemctl 直接管理应用

本节适用于 Ubuntu 24.04，以 systemd 直接运行编译后的 Go 程序。Node.js 用于前端构建，运行服务不依赖 PM2。9.1–9.5 使用专用系统账号部署，需管理员 sudo 权限；9.6 提供保留克隆目录的用户服务方式。2026-10-04 本机已使用 9.6 的方式从 PM2 切换到 systemd；专用系统账号方式尚未在本机实际安装。

### 9.1 服务账号与目录

源码仍可克隆在任意目录，以下继续使用 `ONE_API_DIR` 指向源码根目录。构建完成后，将运行文件安装到标准系统目录；源码目录与运行目录独立。

| 项目 | 路径 | 所有者与权限 |
| --- | --- | --- |
| 专用服务账号 | `one-api` | 系统账号，不允许交互登录，不授予 sudo |
| 程序与工作目录 | `/opt/one-api` | root:root；目录及可执行文件 755 |
| 应用配置 | `/etc/one-api/.env` | root:one-api；目录 750，文件 640 |
| IPA CA | `/etc/one-api/cert/ca.crt` | root:one-api；目录 750，文件 640 |
| SQLite 与运行数据 | `/var/lib/one-api` | one-api:one-api；目录 750，数据库文件 600 |
| 应用文件日志 | `/var/log/one-api` | one-api:one-api；目录 750 |
| systemd 服务文件 | `/etc/systemd/system/one-api.service` | root:root，文件 644 |

以下命令需要系统管理员的 sudo 权限。若账号已经存在，先检查其用途及属性，再复用；不要覆盖另一个应用使用的同名账号。

```bash
getent passwd one-api
# 没有上述账号时执行：
sudo useradd --system --user-group --home-dir /var/lib/one-api \
  --no-create-home --shell /usr/sbin/nologin one-api

sudo install -d -o root -g root -m 0755 /opt/one-api
sudo install -d -o root -g one-api -m 0750 /etc/one-api /etc/one-api/cert
sudo install -d -o one-api -g one-api -m 0750 /var/lib/one-api /var/log/one-api
```

### 9.2 停止原服务并保存数据

新安装可以直接进入下一节。已有安装先停止所有使用原数据库的应用进程，并按[数据库升级说明](./database-freeipa-upgrade.md)保存数据库、配置和旧可执行文件的备份。同一个 SQLite 数据库不能同时交给 PM2 和 systemd 两个应用进程使用。

从本项目 PM2 方式切换时，在原部署用户的终端执行：

```bash
cd "$ONE_API_DIR"
./scripts/pm2.sh stop one-api
./scripts/pm2.sh delete one-api
./scripts/pm2.sh save --force
```

若之前启用了本项目的 PM2 开机服务，再执行 `sudo systemctl disable --now pm2-one-api`，避免重启电脑后恢复旧进程。其他应用的 PM2 服务按其各自配置保留。

### 9.3 安装程序、配置和数据库

完成第 4、7 节的前端与 Go 构建。首次安装执行：

```bash
sudo install -o root -g root -m 0755 "$ONE_API_DIR/one-api" /opt/one-api/one-api
sudo install -o root -g one-api -m 0640 "$ONE_API_DIR/.env" /etc/one-api/.env
sudo ln -s /etc/one-api/.env /opt/one-api/.env
```

如果配置或符号链接已经存在，保留并编辑现有文件，不重复覆盖。已有 systemd 服务升级程序前先执行 `sudo systemctl stop one-api`。

使用自建 IPA CA 时复制可信证书；来源路径按实际情况调整。不启用 IPA，或使用系统已信任的 CA 时可跳过此项。

```bash
sudo install -o root -g one-api -m 0640 \
  "$ONE_API_DIR/cert/ca.crt" /etc/one-api/cert/ca.crt
```

由管理员编辑运行配置：

```bash
sudoedit /etc/one-api/.env
```

保留 `PORT`、会话密钥、账号模式、目录连接等原设置。使用 SQLite 时，确保 `SQL_DSN` 留空，并将数据库路径改为运行目录；启用 IPA 且使用上述 CA 时设置证书绝对路径：

```dotenv
SQL_DSN=
SQLITE_PATH=/var/lib/one-api/one-api.db
IPA_CA_CERT=/etc/one-api/cert/ca.crt
```

服务文件不设置 `PORT` 或使用 `EnvironmentFile`，应用会在 `/opt/one-api` 工作目录通过符号链接读取 dotenv 配置。这样无需将 bind 密码改写为 systemd 的环境文件格式。

**迁移已有 SQLite 时**，先将 `ONE_API_DB` 设为原 `SQLITE_PATH` 解析后的实际绝对路径。下例假定文件在源码根目录；原文件位于 `data/` 时应改为 `"$ONE_API_DIR/data/one-api.db"`。目标已有数据库时先备份，不覆盖运行中的数据库。

```bash
ONE_API_DB="$ONE_API_DIR/one-api.db"
sudo install -o one-api -g one-api -m 0600 \
  "$ONE_API_DB" /var/lib/one-api/one-api.db
for one_api_suffix in -wal -shm -journal; do
  if [ -f "$ONE_API_DB$one_api_suffix" ]; then
    sudo install -o one-api -g one-api -m 0600 \
      "$ONE_API_DB$one_api_suffix" "/var/lib/one-api/one-api.db$one_api_suffix"
  fi
done
```

新安装无需复制数据库，程序首次启动会在该目录创建文件。使用 MySQL/PostgreSQL 时保留原 `SQL_DSN`，无需复制 SQLite。

本服务将 `/opt`、`/etc` 和用户家目录设为只读或不可访问，写入路径限定为 `/var/lib/one-api`、`/var/log/one-api` 及隔离的临时目录。自定义数据、CA 或编码器缓存路径时，应放入对应可访问目录；需要额外写入目录时由管理员调整服务的 `ReadWritePaths`，并设置正确所有权。

### 9.4 创建服务文件

项目提供 [deploy/systemd/one-api.service](./deploy/systemd/one-api.service)。将它安装到系统的默认服务目录：

```bash
sudo install -o root -g root -m 0644 \
  "$ONE_API_DIR/deploy/systemd/one-api.service" \
  /etc/systemd/system/one-api.service
sudo systemctl daemon-reload
sudo systemctl enable --now one-api
sudo systemctl status one-api --no-pager
```

服务以 `one-api` 用户运行，直接执行 `/opt/one-api/one-api`，异常退出后等待 5 秒重启。端口由运行配置的 `PORT` 决定；现有 Nginx 的上游端口应与之匹配，HTTPS 和 Certbot 沿用第 8.4 节的方案。

### 9.5 启停、日志与更新

```bash
# 查看状态
sudo systemctl status one-api --no-pager

# 启动、停止、重启
sudo systemctl start one-api
sudo systemctl stop one-api
sudo systemctl restart one-api

# 查看启动与运行日志
sudo journalctl -u one-api -n 100 --no-pager
sudo journalctl -u one-api -f

# 查看是否开机启动，或取消开机启动
sudo systemctl is-enabled one-api
sudo systemctl disable one-api
```

修改 `/etc/one-api/.env` 后执行 `sudo systemctl restart one-api`。修改服务文件后先执行 `sudo systemctl daemon-reload`，再重启。该程序没有实现配置热加载，不使用 `systemctl reload` 更新应用配置。

程序升级时，先备份数据库和旧程序，停止服务，安装新编译产物，再启动：

```bash
sudo systemctl stop one-api
sudo install -o root -g root -m 0755 "$ONE_API_DIR/one-api" /opt/one-api/one-api
sudo systemctl start one-api
sudo systemctl status one-api --no-pager
```

运行目录与源码目录分开后，迁移电脑需要保存 `/etc/one-api`、`/var/lib/one-api`、必要日志与服务文件，不能仅复制克隆目录。权限问题可用 `namei -l /opt/one-api/.env` 查看目录和链接权限，并确认服务账号可读取配置及 CA、写入数据库和日志目录。

### 9.6 保留克隆目录：使用用户级 systemd 服务

需要保留源码目录中的 `.env`、CA、数据库和日志时，可使用当前部署用户运行服务。本方式没有专用账号隔离，应用具有部署用户的文件访问权限；常驻服务器可采用前述专用账号方式。两种服务方式选择一种，切换时先停止另一种实例。

先完成构建，将 `ONE_API_DIR` 设置为克隆目录的绝对路径，并按 9.2 停止 PM2、备份数据库、删除 PM2 应用记录。然后安装项目提供的 [one-api-user.service](./deploy/systemd/one-api-user.service)：

```bash
mkdir -p "$HOME/.config/systemd/user" "$HOME/.config/one-api"
chmod 700 "$HOME/.config/one-api"
# 首次安装创建链接；若已存在，先核对其指向，不覆盖其他项目。
ln -s "$ONE_API_DIR" "$HOME/.config/one-api/project"
install -m 0644 "$ONE_API_DIR/deploy/systemd/one-api-user.service" \
  "$HOME/.config/systemd/user/one-api.service"
systemctl --user daemon-reload
systemctl --user enable --now one-api
systemctl --user status one-api --no-pager
```

服务使用 `$HOME/.config/one-api/project` 链接作为工作目录，直接读取项目 `.env`。保持原 `SQLITE_PATH`、`IPA_CA_CERT` 和 `PORT` 即可，相对路径依然相对于项目根目录。启动时清除继承的环境变量，避免用户管理器中的同名变量覆盖 `.env`。项目目录及数据库、日志须可由部署用户读写，`.env` 建议权限 600。启动链接和已安装的 unit 位于项目之外，其可重建的服务源文件保存在项目中。

要在退出登录后持续运行，并在开机时启动用户管理器，检查并启用 linger：

```bash
loginctl show-user "$USER" -p Linger
# 输出 Linger=no 时，由管理员执行：
sudo loginctl enable-linger "$USER"
```

若输出已为 `Linger=yes`，无需重复启用。未启用 linger 时，用户服务随用户管理器生命周期运行，不能保证退出登录后的常驻与开机启动。安装用户服务本身不需要 sudo。

日常管理使用当前部署用户执行，不加 sudo：

```bash
systemctl --user start one-api
systemctl --user stop one-api
systemctl --user restart one-api
systemctl --user status one-api --no-pager
systemctl --user is-enabled one-api
journalctl --user -u one-api -n 100 --no-pager
journalctl --user -u one-api -f
```

修改项目 `.env` 后执行 `systemctl --user restart one-api`。更新可执行文件时先停止服务，替换项目根目录的 `one-api`，再启动。修改服务源文件后重新 install 到用户服务目录，执行 daemon-reload 后重启。移动克隆目录或换电脑时重建 project 链接、安装 unit、检查 linger，再启动；不要让 PM2 同时恢复该应用。

systemd 参数依据 Ubuntu 24.04 自带的 `man systemd.service`、`man systemd.exec`，服务账号创建参数见 `man useradd`。

## 10. 常见问题与迁移

| 现象 | 检查方法 |
| --- | --- |
| 提示 `web/build/*` 没有匹配文件 | 先完成默认前端构建，再启动 Go 程序 |
| IPA 登录一直等待或提示无法查询 | 检查 `getent hosts IPA域名`、网络路由、636/389 端口、目录服务及 bind 凭据；LDAP 建连有 5 秒超时 |
| CA 文件不存在或证书验证失败 | 核对工作目录、`IPA_CA_CERT`、PEM 内容及证书域名；移动项目后检查路径 |
| 登录后没有管理权限 | 检查应用角色组 DN、用户 `memberOf`、用户是否直接属于对应组，以及 uid 是否符合匹配条件 |
| 列表出现本地 root | 检查 `.env` 的 `IPA_ONLY=true` 是否被外部环境覆盖，并重启 |
| 修改匹配条件后人员减少 | 范围外历史 IPA 账号也会隐藏并失去使用资格，记录仍保留；管理员 uid 同样受规则约束 |
| 重启后会话失效 | 检查是否配置固定 `SESSION_SECRET`；旧会话失效后重新登录 |
| `database is locked` | 确认只有一个应用进程使用该 SQLite 文件，并使用已包含单连接池修复的本分支代码 |

迁移到另一台电脑时，先停止服务，再复制项目目录，包括 `.env`、CA 文件、SQLite 数据库、日志和本地过程文档。相对路径配置可以随文件夹迁移；使用绝对路径时要调整。新电脑按本文重新准备 Go/Node 工具、构建并启动，并按作者方案重建 PM2 启动记录、Nginx 站点配置和证书续期配置。现有站点配置和续期步骤也应在项目内保存一份，网站私钥按敏感文件保存且不提交 Git。工具按新电脑的常规安装方式重新准备，不需要沿用原电脑的工具目录。将 `ONE_API_DIR` 更新为新电脑上的项目根目录，并重新设置 PM2 工作目录；Nginx 和 Certbot 默认配置及网站证书位于项目之外，迁移前须另外保存必要的配置和证书资料。

`.env` 和 `.runtime/` 已在 Git 忽略规则中；内部 CA、业务数据库及备份也应保持本地保存。不要通过提交仓库传递真实密码。由原版升级到 FreeIPA 版本时，先阅读[数据库升级与回退说明](./database-freeipa-upgrade.md)。
