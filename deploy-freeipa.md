# Ubuntu 24.04：以非登录账号部署 FreeIPA 版 One API

更新日期：2026-10-04。安装入口为已发布的 `FreeIPA` 分支，分支名称区分大小写。

## 1. 部署方式与账号职责

本项目在原 One API 上增加 FreeIPA 账号接入。本文采用 **普通部署账号构建 → 管理员安装 → 非登录账号运行** 的流程：systemd 直接运行编译后的 Go 程序，Nginx 提供反向代理，Certbot 管理网站 HTTPS 证书。

**FreeIPA 功能目前仅通过克隆源码安装方式的验证。** 已使用的环境为 Ubuntu 24.04 x86_64、Go 1.27.1、Node.js 26.9.0、npm 11.19.1，默认主题和 SQLite。PM2 及用户级 systemd 曾用于本机运行；本文的专用非登录账号方案尚未在本机实际部署。Docker、Docker Compose、上游预编译包、其他主题及 Nginx/Certbot 组合的完整安装流程尚未验证。

### 1.1 账号职责

| 身份 | 职责 | 工具与权限 |
| --- | --- | --- |
| 普通部署账号 | 登录服务器、克隆源码、安装 nvm/Node.js、构建前端、编译 Go、准备安装文件、执行升级 | 使用自己的用户目录与构建环境；不以 root 执行 npm |
| 系统管理员 | 安装系统软件、创建服务账号、安装程序和配置、设置权限、维护 systemd/Nginx/Certbot | 可由部署账号通过 sudo 执行，也可由另一名管理员执行 |
| 非登录服务账号 `one-api` | 执行应用、读取配置和 CA、写数据库及日志 | 无交互登录、无 sudo、无登录密码；不安装 nvm、Node.js、Go 或 PM2 |
| FreeIPA 应用用户 | 登录网页、调用模型及按应用角色管理额度 | 与上述 Linux 账号职责独立 |
| LDAP bind 账号 | 在 FreeIPA 中供应用查询目录 | 不是 Linux 服务账号，也不是员工登录账号 |

nvm 安装在**普通部署账号**的环境中，通常为 `~/.nvm`；所有 npm 和构建命令由该账号执行。若 Go 安装在 `/usr/local/go`，由管理员安装，部署账号使用它编译。服务启动时无需加载 nvm 或部署账号的 Shell 配置。

前端静态资源通过 Go embed 编入可执行文件。运行时无需 Node.js、Go 编译器或 PM2，但仍需对应的系统动态库；不能将使用 CGO 编译的程序视为可在任何 Linux 上运行的完全静态文件。应用以 `one-api` 账号运行，部署账号退出登录后服务仍运行，开机启动不依赖用户会话或 linger。

### 1.2 源码目录与运行目录

源码可克隆在部署账号可读写的任意目录。编译后将运行文件安装到标准位置，不要求服务账号访问部署账号的家目录。

| 内容 | 位置 | 所有者与权限 |
| --- | --- | --- |
| 源码、nvm 与前端依赖 | 部署账号选择的目录及自己的用户目录 | 部署账号管理 |
| 程序与工作目录 | `/opt/one-api` | root:root；目录及程序 755 |
| 运行配置 | `/etc/one-api/.env` | root:one-api；目录 750，文件 640 |
| IPA CA | `/etc/one-api/cert/ca.crt` | root:one-api；目录 750，文件 640 |
| SQLite 与运行数据 | `/var/lib/one-api` | one-api:one-api；目录 750，数据库文件 600 |
| 应用文件日志 | `/var/log/one-api` | one-api:one-api；目录 750 |
| 服务文件 | `/etc/systemd/system/one-api.service` | root:root，文件 644 |

后续代码块默认在普通部署账号的 Bash 终端执行；带 sudo 的命令需要管理员权限。无需切换或登录 `one-api` 账号。

## 2. 准备构建工具

先检查是否已有可用工具：

```bash
command -v git curl gcc make go node npm nginx certbot
```

SQLite 驱动使用 CGO，需要 C 编译器。由管理员安装 Ubuntu 系统依赖：

```bash
sudo apt update
sudo apt install -y git curl ca-certificates build-essential xz-utils
```

| 工具 | 安装方式 | 执行账号 |
| --- | --- | --- |
| Go | 按 [Go 官方安装说明](https://go.dev/doc/install)安装，Linux 官方发行包通常位于 /usr/local/go；加入部署账号 PATH | 管理员安装，部署账号使用 |
| nvm、Node.js、npm | 按 [nvm 官方说明](https://github.com/nvm-sh/nvm#installing-and-updating)安装到部署账号的用户目录 | 普通部署账号，不加 sudo |
| Nginx、Certbot | 使用第 9 节的 Ubuntu 软件包与配置步骤 | 管理员 |

### 2.1 在部署账号下安装 nvm 和 Node.js

已有可用的 Node.js/npm 可复用。需要安装时，以下命令由普通部署账号执行，不使用 sudo，也不在服务账号下执行：

```bash
curl -fsSL https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.8/install.sh | bash
# 安装后新开终端，或按安装器使用的默认目录规则加载 nvm。
export NVM_DIR="$HOME/.nvm"
if [ -n "${XDG_CONFIG_HOME:-}" ]; then
  export NVM_DIR="$XDG_CONFIG_HOME/nvm"
fi
. "$NVM_DIR/nvm.sh"
nvm install 26.9.0
nvm use 26.9.0
nvm alias default 26.9.0
```

以上固定版本用于复现已使用的构建环境，不表示最新版本。已有 nvm 时以其安装配置为准；其他版本需自行确认构建兼容性。nvm 安装及目录规则见 [官方文档](https://github.com/nvm-sh/nvm#installing-and-updating)。

管理员安装 Go 后，部署账号在构建终端中设置 PATH 并检查版本：

```bash
export PATH="/usr/local/go/bin:$PATH"
go version
node --version
npm --version
```

不需要为 FreeIPA 接入安装 ldapsearch 或 Python；目录访问由 Go 程序中的 LDAP 库完成。

## 3. 克隆 FreeIPA 分支

以下由普通部署账号执行。先设置源码目录的绝对路径，路径可自行选择；包含空格时保留双引号：

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

远端检查应返回 `refs/heads/FreeIPA`，克隆后应显示 `FreeIPA`。2026-10-04 已发布该分支，无需等待合入上游 main。已有克隆目录不要重复覆盖；新开终端时重新设置 `ONE_API_DIR` 并加载构建工具环境。不要使用 sudo git clone 或 sudo npm install，以免源码和依赖归 root 所有。

## 4. 构建前端和 Go 程序

全部构建命令由普通部署账号执行。后端编译会嵌入前端资源，因此先构建默认主题：

```bash
cd "$ONE_API_DIR/web/default"
npm install
BUILD_PATH=../build/default GENERATE_SOURCEMAP=false \
  node node_modules/react-scripts/scripts/build.js
cd "$ONE_API_DIR"
go mod download
CGO_ENABLED=1 go build -trimpath -o one-api .
```

构建结果应包含 `web/build/default/index.html` 及项目根目录的 `one-api` 可执行文件。仅构建默认主题时配置 `THEME=default`。现有 ESLint 告警不一定代表失败，应检查命令退出状态和生成的文件。

若遇到 `Cannot find module 'ajv/dist/compile/codegen'`，已使用过的处理方式是修复依赖树后重新构建：

```bash
cd "$ONE_API_DIR/web/default"
npm install --no-save --no-package-lock 'ajv@^8.8.2'
BUILD_PATH=../build/default GENERATE_SOURCEMAP=false \
  node node_modules/react-scripts/scripts/build.js
cd "$ONE_API_DIR"
CGO_ENABLED=1 go build -trimpath -o one-api .
```

编译完成后，只安装可执行文件、运行配置和所需证书。前端源码更新后，需重新构建前端并编译 Go 程序。

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

以下由普通部署账号执行。源码目录中的配置是安装准备文件；第 7 节会将其安装到 `/etc/one-api/.env`，启用服务后以该运行配置为准。从 IPA 管理员取得可信的 PEM CA 文件：

```bash
cd "$ONE_API_DIR"
mkdir -p cert
cp /可信来源/ipa-ca.crt cert/ca.crt
cp .env.example .env
chmod 600 .env
```

已有 `.env` 时直接编辑，避免复制示例覆盖原配置。`cert/ca.crt` 在此处保存待安装的 CA，服务运行时使用 `/etc/one-api/cert/ca.crt`。不使用自建 CA 时跳过证书复制。

以下是新安装的 SQLite + 纯 IPA 模式配置示例。域名、DN、密码和会话密钥均需替换为自己的值。端口 3000 仅为示例，实际以 `.env` 的 `PORT` 为准，后面的访问地址和 Nginx 上游端口应同步调整。已有安装应保留原数据库与会话密钥，在原 `.env` 中增加 IPA 配置，升级前先阅读[数据库升级说明](./database-freeipa-upgrade.md)：

```dotenv
PORT=3000
DEBUG=false
THEME=default

# 不需要出站 HTTP 代理时留空。
HTTPS_PROXY=

# 固定密钥让正常重启后的会话保持可验证。
SESSION_SECRET=REPLACE_WITH_RANDOM_SESSION_SECRET

# SQLite 使用服务账号的数据目录；本示例不启用 Redis。
SQL_DSN=
SQLITE_PATH=/var/lib/one-api/one-api.db
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
IPA_CA_CERT=/etc/one-api/cert/ca.crt
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
| `IPA_BIND_PASSWORD` | 专用查询账号密码，保存在 `.env`，不要放入服务文件、命令参数或版本库 |
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

运行配置保存在 `/etc/one-api/.env`，通过 `sudoedit /etc/one-api/.env` 修改，然后执行 `sudo systemctl restart one-api`。修改源码目录中的准备文件不会自动更新已安装的配置。恢复公开注册时将 `REGISTER_ENABLED=true`，还需确保后台注册开关已开启；它不是强制打开注册的开关。

**启用 IPA 会改变管理资格。** 当前实现中，即使 `IPA_ONLY=false`，旧本地 `admin/root` 也不能凭原有角色访问管理接口。切换前应先准备符合 `IPA_USER_MATCH` 且直接属于 `IPA_ROOT_GROUP_DN` 指定组的 IPA 用户，避免切换后没有可用的应用超级管理员。账号同名不会自动合并，已有安装的身份关联与数据保留要求见[数据库升级说明](./database-freeipa-upgrade.md)。

下列原项目参数继续沿用，用于说明上述示例和迁移时的路径关系：

| 配置 | 本示例中的用法 |
| --- | --- |
| `SQL_DSN` | 留空使用 SQLite；已有 MySQL/PostgreSQL 安装保留原配置 |
| `SQLITE_PATH` | 默认 `one-api.db`；本文明确使用 `/var/lib/one-api/one-api.db`。迁移时先复制原数据库，再设置目标路径；不要指向空文件而丢失旧账号数据 |
| `SESSION_SECRET` | 保留固定密钥，避免正常重启后会话失效 |
| `THEME` | 本分支验证使用 `default` |
| `REDIS_CONN_STRING` | 本示例留空；已有 Redis 配置继续沿用 |
| `HTTPS_PROXY` | 出站 HTTP/HTTPS 代理，不用于网站 HTTPS 或 IPA 目录连接 |

其他原项目环境变量见 [README 环境变量说明](./README.md#环境变量)，无需因 FreeIPA 接入重新设置。

使用 StartTLS 时，改为 `IPA_URL=ldap://ipa.example.com:389` 和 `IPA_STARTTLS=true`。普通明文 LDAP 不被允许；内部证书通过 CA 信任配置处理。

`HTTPS_PROXY` 用于支持环境代理的出站 HTTP/HTTPS 请求，不是服务监听地址，也不会代理 LDAP。没有可用代理时应清空示例中的 `http://localhost:7890`。模型转发与用户内容获取的专项代理还可分别使用 `RELAY_PROXY`、`USER_CONTENT_REQUEST_PROXY` 配置。

程序会从**当前工作目录**自动读取 `.env`。启动前已导出的同名环境变量优先于文件；无需执行 `source .env`，它是 dotenv 配置文件，不是 shell 脚本。调整账号匹配、CA、端口等配置后需重启。

已有安装迁移时，先记录旧 `SQLITE_PATH` 对应的真实文件，第 7 节会在停止旧实例后复制到 `/var/lib/one-api/one-api.db`。不要仅修改路径就启动；指向不存在的文件会创建空数据库。服务从 `/opt/one-api` 工作目录读取配置，不能将旧的项目相对路径直接用于新服务。

## 7. 创建服务账号并安装运行文件

以下带 sudo 的操作由管理员执行；若部署账号具备 sudo 权限，可在其终端直接完成。整个流程不需要登录服务账号。

### 7.1 迁移安装：停止原实例并保存数据

新安装跳过本小节。迁移已有安装前，记录原配置、会话密钥及 SQLite 的实际路径，并阅读[数据库升级说明](./database-freeipa-upgrade.md)。

若原实例由本项目 PM2 管理，在原部署账号下执行：

```bash
cd "$ONE_API_DIR"
./scripts/pm2.sh stop one-api
./scripts/pm2.sh delete one-api
./scripts/pm2.sh save --force
```

仅在原先启用了本项目 `pm2-one-api` 系统服务时，执行 `sudo systemctl disable --now pm2-one-api`。不要关闭其他项目的 PM2 服务。

若原实例是用户级 systemd 服务，在其所属用户下执行：

```bash
systemctl --user disable --now one-api
```

若已有系统级实例，则执行 `sudo systemctl stop one-api`。确保旧实例已停止且不会自动恢复，再备份 SQLite；不要让两个应用实例同时使用同一个数据库。保留旧程序、配置及 SQLite 文件和存在的 -wal/-shm/-journal 文件，以便恢复。MySQL/PostgreSQL 使用其数据库工具备份。

### 7.2 创建非登录账号与目录

先检查同名账号：

```bash
getent passwd one-api
```

没有该账号时执行：

```bash
sudo useradd --system --user-group --home-dir /var/lib/one-api \
  --no-create-home --shell /usr/sbin/nologin one-api
sudo passwd --lock one-api
```

不为该账号设置登录密码，不加入 sudo 组。若同名账号已存在，先确认其用途、组和登录属性，不覆盖其他应用的账号。账号禁用密码登录不影响 systemd 以其身份启动进程。

创建标准目录：

```bash
sudo install -d -o root -g root -m 0755 /opt/one-api
sudo install -d -o root -g one-api -m 0750 /etc/one-api /etc/one-api/cert
sudo install -d -o one-api -g one-api -m 0750 /var/lib/one-api /var/log/one-api
```

### 7.3 安装程序、配置和 CA

首次安装时执行：

```bash
sudo install -o root -g root -m 0755 "$ONE_API_DIR/one-api" /opt/one-api/one-api
sudo install -o root -g one-api -m 0640 "$ONE_API_DIR/.env" /etc/one-api/.env
sudo ln -s /etc/one-api/.env /opt/one-api/.env
```

配置或符号链接已存在时先核对并保留，不重复覆盖。运行时从 `/opt/one-api/.env` 链接读取 `/etc/one-api/.env`。服务账号可读取配置但不能修改程序或配置。

使用内部 CA 时执行；未启用 IPA 或使用系统信任库时可跳过：

```bash
sudo install -o root -g one-api -m 0640 \
  "$ONE_API_DIR/cert/ca.crt" /etc/one-api/cert/ca.crt
```

核对运行配置：

```bash
sudoedit /etc/one-api/.env
```

保留实际 `PORT`、会话密钥、目录连接及账号模式。SQLite 的 `SQL_DSN` 留空，路径明确设置为：

```dotenv
SQL_DSN=
SQLITE_PATH=/var/lib/one-api/one-api.db
IPA_CA_CERT=/etc/one-api/cert/ca.crt
```

如果不需要自建 IPA CA，可将 `IPA_CA_CERT` 留空。MySQL/PostgreSQL 保留原 `SQL_DSN`，不复制 SQLite。`.env` 是 dotenv 文件，不执行 source，也不作为 systemd EnvironmentFile 使用。

### 7.4 复制已有 SQLite

新安装无需执行复制，程序会创建数据库。迁移时将 `ONE_API_DB` 设置为旧 `SQLITE_PATH` 对应的真实绝对路径，不能误选源码根目录的另一份数据库。示例假定旧文件在 data 下：

```bash
ONE_API_DB="$ONE_API_DIR/data/one-api.db"
sudo install -o one-api -g one-api -m 0600 \
  "$ONE_API_DB" /var/lib/one-api/one-api.db
for one_api_suffix in -wal -shm -journal; do
  if [ -f "$ONE_API_DB$one_api_suffix" ]; then
    sudo install -o one-api -g one-api -m 0600 \
      "$ONE_API_DB$one_api_suffix" "/var/lib/one-api/one-api.db$one_api_suffix"
  fi
done
```

上述命令只在原实例已停止、目标库尚不存在或已妥善备份时执行。不得覆盖运行中的数据库。目标已有旧的 SQLite 旁文件时，应在停机后与原库一起保存并清理，不能混用两份数据库的旁文件。

## 8. 启动、检查与日常管理

### 8.1 安装服务并开机启动

项目提供 [deploy/systemd/one-api.service](./deploy/systemd/one-api.service)。由管理员安装到系统服务目录：

```bash
sudo install -o root -g root -m 0644 \
  "$ONE_API_DIR/deploy/systemd/one-api.service" \
  /etc/systemd/system/one-api.service
sudo systemctl daemon-reload
sudo systemctl enable --now one-api
sudo systemctl status one-api --no-pager
```

服务文件指定 `User=one-api`、`Group=one-api`，工作目录 `/opt/one-api`，执行 `/opt/one-api/one-api --log-dir /var/log/one-api`。异常退出后等待 5 秒重启；开机由系统管理器启动，不依赖部署账号登录。服务文件不覆盖 PORT，端口由运行配置决定。

当前服务文件使用只读系统目录、禁止访问用户家目录及隔离的临时目录。可写业务目录为 `/var/lib/one-api` 和 `/var/log/one-api`。自定义数据或缓存路径应选择服务可写目录；需要其他持久写入目录时同步调整服务文件的 ReadWritePaths 与文件所有权。

### 8.2 检查部署状态与登录

以下例子使用 3000；实际 PORT 不同则同步替换检查地址和 Nginx 上游：

```bash
sudo systemctl show one-api -p User -p Group -p ActiveState -p SubState
curl --noproxy '*' -f http://127.0.0.1:3000/api/status
ss -ltnp 'sport = :3000'
```

服务应以 one-api 用户运行，状态为 active/running。纯 IPA 配置时状态接口应包含 `ipa_login=true`、`ipa_only=true`、`registration_enabled=false`。

员工使用 IPA 登录名（如 `h320001`）及密码登录，密码管理仍在 FreeIPA 完成。纯 IPA 模式不使用本地 root。管理员应确认用户列表、组角色及额度管理可用，再配置模型渠道与调用令牌。模型客户端使用 `https://实际域名/v1`；模型 API Key 是本系统令牌，不是 IPA 密码或 bind 密码。

程序监听所有网卡；启用网站入口后，防火墙应限制后端端口的直接访问，网页、`/api/` 和 `/v1/` 均通过 Nginx 访问。

### 8.3 启停、配置修改和日志

由管理员或具备相应 sudo 权限的部署账号执行：

```bash
sudo systemctl start one-api
sudo systemctl stop one-api
sudo systemctl restart one-api
sudo systemctl status one-api --no-pager
sudo systemctl is-enabled one-api

sudo journalctl -u one-api -n 100 --no-pager
sudo journalctl -u one-api -f
```

修改运行配置使用 `sudoedit /etc/one-api/.env`，然后执行 `sudo systemctl restart one-api`。修改服务文件后先执行 `sudo systemctl daemon-reload`，再重启。应用不支持配置热加载，不使用 systemctl reload。需要取消开机启动时执行 `sudo systemctl disable one-api`；同时停止可使用 disable --now。

部署账号的 nvm/Node.js 升级不影响已经运行的 Go 程序；重新构建并安装新程序后，服务才使用更新的产物。

## 9. Nginx 与网站 HTTPS

由管理员安装及配置。反向代理与 HTTPS 的用途沿用[原作者的部署介绍](https://justsong.cn/page/how-to-deploy-a-website)，应用进程由本说明的非登录账号运行。

Ubuntu 软件包安装示例：

```bash
sudo apt install -y nginx certbot python3-certbot-nginx
sudo systemctl enable --now nginx
```

上述 Certbot 的 Python 依赖由 Ubuntu 包管理器提供，不是应用的 Python 环境；one-api 服务账号不需要自行安装。Nginx 的 Ubuntu 安装步骤见 [官方说明](https://ubuntu.com/server/docs/how-to/web-services/install-nginx/)。

在 `/etc/nginx/sites-available/one-api` 配置网站，替换域名和上游端口：

```nginx
server {
    listen 80;
    server_name one-api.example.com;

    location / {
        proxy_pass http://127.0.0.1:3000;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;
        proxy_read_timeout 600s;
    }
}
```

编辑与启用：

```bash
sudoedit /etc/nginx/sites-available/one-api
sudo ln -s /etc/nginx/sites-available/one-api /etc/nginx/sites-enabled/one-api
sudo nginx -t
sudo systemctl reload nginx
```

若链接已存在，先检查，勿重复覆盖。共享主机已有站点时保留其配置。`proxy_buffering off` 用于流式模型响应，读取超时按模型耗时调整，参数说明见 [Nginx 官方文档](https://nginx.org/en/docs/http/ngx_http_proxy_module.html)。

公网域名须正确解析到服务器，证书验证所需的端口应可达。将示例域名和邮箱替换为真实值后申请证书：

```bash
sudo certbot --nginx -d one-api.example.com \
  --email admin@example.com --agree-tos --redirect
systemctl list-timers --all | grep certbot
```

确认已配置续期机制；证书管理及 Nginx 插件用法见 [Certbot 官方文档](https://eff-certbot.readthedocs.io/en/stable/using.html#nginx)。仅内网可达的服务不能直接照搬公网 HTTP 验证流程，应选用适用的 DNS 验证或企业证书。

网站 HTTPS 证书供浏览器和模型客户端验证网站；`IPA_CA_CERT` 供应用验证 FreeIPA 的 LDAPS 服务，二者不能互换。部署后访问 `https://实际域名/login`，应用后台的服务器地址也填写对外 HTTPS 地址。

## 10. 升级、迁移与常见问题

### 10.1 更新应用

由普通部署账号更新 FreeIPA 源码并按第 4 节重新构建。管理员先停止服务，备份当前运行数据库、配置和程序，再安装新程序：

```bash
sudo systemctl stop one-api
# 完成备份后安装新编译产物。
sudo install -o root -g root -m 0755 "$ONE_API_DIR/one-api" /opt/one-api/one-api
sudo systemctl start one-api
sudo systemctl status one-api --no-pager
```

升级程序不复制 .env.example 覆盖运行配置；只在 `/etc/one-api/.env` 中补充需要的参数。保留原会话密钥及数据库路径。启动时执行数据库结构补充，具体行为和恢复要求见[数据库升级说明](./database-freeipa-upgrade.md)。

### 10.2 迁移到另一台电脑

停止应用后，保存以下内容：

| 内容 | 需要保存的文件或目录 |
| --- | --- |
| 源码及项目记录 | 克隆目录及本地 docs 文档 |
| 应用配置及 IPA CA | /etc/one-api |
| SQLite 及旁文件 | /var/lib/one-api |
| 必要日志 | /var/log/one-api |
| 服务与网站配置 | /etc/systemd/system/one-api.service、对应 Nginx 站点 |
| 网站证书及续期配置 | 对应 /etc/letsencrypt 内容及所用验证方式 |

可将外部运行资料的备份集中保存到项目目录中受保护、未提交 Git 的位置，再复制整个文件夹。真实密码、私钥和业务数据不提交 Git。仅复制源码目录不会包含实际运行数据库。

新电脑由部署账号重新准备构建工具，由管理员重建非登录服务账号、安装运行文件并恢复配置和数据。复制时不盲目沿用旧电脑的数字 UID/GID，恢复后按新电脑的 one-api 账号设置所有权。重建 systemd/Nginx/续期配置，核对域名、IPA 地址及端口后启动。

### 10.3 常见问题

| 现象 | 检查方法 |
| --- | --- |
| 服务无法读取配置或 CA | 检查 /opt/one-api/.env 链接、/etc/one-api 权限和 IPA_CA_CERT；用 namei -l 检查路径 |
| 服务无法写数据库 | 确认 SQLITE_PATH 指向 /var/lib/one-api，目录及文件归 one-api 所有 |
| 找不到前端资源 | 先构建 web/build/default，再重新编译 Go 程序 |
| IPA 查询失败或等待 | 检查域名解析、网络、636/389 端口、bind 凭据和 CA；LDAP 建连有 5 秒超时 |
| 登录后没有管理权限 | 检查应用组 DN、直接组成员资格和 IPA_USER_MATCH |
| 列表出现本地 root | 检查实际运行配置是否 IPA_ONLY=true 并重启 |
| 修改配置不生效 | 编辑 /etc/one-api/.env，而非源码目录的准备文件，之后重启 |
| 重启后会话失效 | 保留固定 SESSION_SECRET |
| database is locked | 确认旧实例已停且只有一个应用进程使用该库 |
| 找不到服务 | 本文使用系统服务，命令为 sudo systemctl，而非 systemctl --user |

服务与账号参数依据 Ubuntu 24.04 自带的 `man systemd.service`、`man systemd.exec`、`man useradd`。本文命令用于管理员按步骤部署，修改文档不代表已在本机执行账号创建或服务切换。
