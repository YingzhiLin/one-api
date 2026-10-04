# Ubuntu 24.04：以非登录账号部署 LDAP / FreeIPA 版 One API

更新日期：2026-10-04。安装入口为已发布的 `FreeIPA` 分支，分支名称区分大小写。

## 1. 部署方式与账号职责

本项目在原 One API 上增加 LDAP 目录账号接入，包括目录认证、用户同步和组角色管理。FreeIPA 是已验证的目录服务实例；其他 LDAP 目录须符合第 5 节的对象及属性约定。本文采用 **普通部署账号构建 → 管理员安装 → 非登录账号运行** 的流程：systemd 直接运行编译后的 Go 程序，Nginx 提供反向代理，Certbot 管理网站 HTTPS 证书。

为保持配置和数据兼容，安装分支仍名为 `FreeIPA`，环境变量保留 `IPA_` 前缀，文档文件名仍为 `deploy-freeipa.md`。这些名称不改变实际使用的 LDAP 协议，也不表示其他目录无需适配就能直接使用。

**FreeIPA 功能目前仅通过克隆源码安装方式的验证。** 已使用的环境为 Ubuntu 24.04 x86_64、Go 1.27.1、Node.js 26.9.0、npm 11.19.1，默认主题和 SQLite。PM2 及用户级 systemd 曾用于本机运行；本文的专用非登录账号方案尚未在本机实际部署。Docker、Docker Compose、上游预编译包、其他主题及 Nginx/Certbot 组合的完整安装流程尚未验证。

### 1.1 账号职责

| 身份 | 职责 | 工具与权限 |
| --- | --- | --- |
| 普通部署账号 | 登录服务器、克隆源码、安装 nvm/Node.js、构建前端、编译 Go、准备安装文件、执行升级 | 使用自己的用户目录与构建环境；不以 root 执行 npm |
| 系统管理员 | 安装系统软件、创建服务账号、安装程序和配置、设置权限、维护 systemd/Nginx/Certbot | 可由部署账号通过 sudo 执行，也可由另一名管理员执行 |
| 非登录服务账号 `one-api` | 执行应用、读取配置和 CA、写数据库及日志 | 无交互登录、无 sudo、无登录密码；不安装 nvm、Node.js、Go 或 PM2 |
| LDAP 目录应用用户 | 登录网页、调用模型及按应用角色管理额度 | 与上述 Linux 账号职责独立；FreeIPA 员工账号属于此类 |
| LDAP bind 账号 | 供应用查询 LDAP 目录中的用户与组属性 | 不是 Linux 服务账号，也不是员工登录账号 |

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

不需要为 LDAP / FreeIPA 接入安装 ldapsearch 或 Python；目录查询和用户密码认证由 Go 程序中的 LDAP 库完成。

## 3. 克隆 FreeIPA 分支

以下由普通部署账号执行。先设置源码目录的绝对路径，路径可自行选择，必须使用绝对路径，下例中使用 `/home/lynnet/download/one-api`；包含空格时保留双引号：

```bash
export ONE_API_DIR='/home/lynnet/download/one-api/one-api'
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

默认前端的 `react-scripts@5.0.1` 与 i18next 24 的可选 TypeScript 依赖范围冲突。项目在 `web/default/.npmrc` 设置 `legacy-peer-deps=true`，让该目录的 npm 安装绕过 peerDependencies 检查；这是当前 JavaScript 前端的安装兼容处理，并未消除上游依赖冲突。行为见 [npm 官方说明](https://docs.npmjs.com/cli/v11/using-npm/config/#legacy-peer-deps)。旧克隆尚未包含该文件时，可使用 `npm install --legacy-peer-deps`，无需设置用户全局选项或使用 `--force`。


如果你需要使用go和npm的代理，推荐如下：

```bash
# Use the aliyun proxy if you need.
export GOPROXY=https://mirrors.aliyun.com/goproxy/

# use taobao proxy if you need.
# If you haven't nrm, please install it with sudo
# sudo npm install -g nrm
nrm use taobao
```
Next.

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

若出现 `Unknown user config "home"`，这是当前用户 npm 配置中的无效选项，与 ERESOLVE 依赖冲突分别处理。在执行构建的普通部署账号下移除该选项：

```bash
npm config delete home --location=user
```

日志若位于 `/root/.npm/`，应核对是否在 root 终端执行构建。按本文切换到普通部署账号并加载其 nvm 环境；不要通过 sudo npm 安装依赖。nvm 环境中的可选 npm 全局工具也由该账号安装，不使用 sudo。已失败的安装可以在 `web/default` 中重试，无需先删除整个源码目录或修改运行配置。

## 5. 准备 LDAP 目录服务（FreeIPA 示例）

### 5.1 LDAP 接入方式与兼容要求

应用先用专用 bind 账号查询用户 DN，再以该用户 DN 和登录密码执行 LDAP bind 认证。连接须使用 LDAPS，或在 LDAP 连接上启用 StartTLS；不接受明文 LDAP 认证。账号和密码管理仍由目录服务负责。

当前 LDAP 属性约定如下，配置只能调整连接地址、查询范围、uid 匹配条件和角色组 DN，尚不能自定义对象类或属性映射：

| 目录对象或属性 | 当前代码的要求 |
| --- | --- |
| `objectClass=posixAccount` | 用户搜索使用固定对象类过滤条件 |
| `uid` | 用户登录名；单用户查询结果须唯一 |
| `ipaUniqueID` | 必须存在且非空，用作稳定身份标识；不会自动改用 entryUUID 或其他属性 |
| `memberOf` | 用户条目中的组 DN 列表，与管理员及超级管理员组 DN 比较 |
| `nsAccountLock` | 值为 true 时判定目录账号锁定；不自动映射其他目录的禁用字段 |
| `displayName`、`cn`、`mail` | 显示名和邮件信息；显示名依次回退至 cn、uid |

其他 LDAP 目录可在满足这些要求时按相同流程配置。缺少稳定 ID、使用不同用户对象类、组成员模型或锁定字段时，需要先做兼容适配，不能仅替换 URL 就视为完成接入。当前完整验证的目录服务仍为 FreeIPA。

### 5.2 目录连接资料与角色准备

在启动前准备以下资料：

| 资料 | 用途 |
| --- | --- |
| 可解析的 LDAP 服务域名及端口 | 连接目录，域名需与服务证书匹配 |
| Base DN 和用户目录 DN | 限定查询范围 |
| 专用 LDAP bind DN 与密码 | 查询用户属性及组成员身份 |
| 管理员、超级管理员组 DN | 授予 One API 的管理角色 |
| LDAP 服务的 CA 证书或 CA 证书链 | 验证目录服务的内部证书；FreeIPA 示例使用 IPA CA |

绑定账号需要能读取匹配用户的 `uid`、`cn`/`displayName`、`mail`、`ipaUniqueID`、`memberOf` 和 `nsAccountLock` 等属性。本系统不需要通过该账号修改 IPA 密码或账号。

应用角色组可使用 FreeIPA 非 POSIX 组，例如 `one-api-admins` 和 `one-api-roots`。代码依据用户 `memberOf` 中的组 DN 判断角色；首次安装先准备至少一个直接属于应用超级管理员组的员工账号，并确保其 uid 匹配 `IPA_USER_MATCH`。不要依赖 IPA 的 `admin` 用户名自动取得本系统管理员资格。

管理员通过所用 LDAP 目录的管理工具维护用户、密码、组成员和锁定状态；FreeIPA 示例使用 FreeIPA Web UI。本系统的本地“启用/禁用”仅控制应用使用资格；目录锁定状态必须满足上述属性约定，目录与本地任一侧禁用都不可用。

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
# 基础运行
# 网页、管理 API 和模型 API 共用的监听端口；服务监听所有网卡。
PORT=3000
# 是否启用调试模式；默认 false。
DEBUG=false
# 前端主题；默认 default，仅构建默认主题时使用 default。
THEME=default

# 代理与会话
# 支持环境代理的出站 HTTP/HTTPS 请求使用的代理；不用于监听、网站 HTTPS 或 LDAP，不需要时留空。
HTTPS_PROXY=
# 固定会话签名密钥；保留原值可避免正常重启后会话失效，不使用示例占位符。
SESSION_SECRET=REPLACE_WITH_RANDOM_SESSION_SECRET

# 数据库与缓存
# 数据库连接字符串；留空使用 SQLite，已有 MySQL/PostgreSQL 安装保留原配置。
SQL_DSN=
# SQLite 文件路径；相对路径按应用工作目录解析，父目录须存在，迁移时确保继续使用原业务数据。
SQLITE_PATH=/var/lib/one-api/one-api.db
# Redis 连接字符串；不使用 Redis 时留空，已有安装保留原配置。
REDIS_CONN_STRING=

# 账号模式与注册
# false 禁止公开密码注册和首次 OAuth 账号创建；不影响已有账号登录或 IPA 同步，IPA_ONLY=true 时也禁止公开注册。
REGISTER_ENABLED=false
# 是否启用 IPA 登录；默认 false，IPA_ONLY=true 时强制启用；启用后管理接口要求 IPA 身份及应用角色组资格。
IPA_ENABLED=true
# true 仅允许 IPA 身份登录和使用，排除本地账号并禁止非 IPA 账号创建；空数据库不创建本地 root。
IPA_ONLY=true

# LDAP 连接与查询
# LDAP 服务地址；推荐 ldaps://域名:636，域名须与证书匹配，ldap://域名:389 须同时启用 StartTLS。
IPA_URL=ldaps://ipa.example.com:636
# 是否在 LDAP 连接上启用 StartTLS；默认 false，使用 ldap:// 地址时设为 true。
IPA_STARTTLS=false
# IPA 目录的 Base DN，用于限定查询范围。
IPA_BASE_DN=dc=example,dc=com
# 用户查询范围；未设置或留空时使用 IPA_BASE_DN。
IPA_USER_BASE_DN=cn=users,cn=accounts,dc=example,dc=com
# 按 IPA 登录名 uid 匹配，忽略大小写；仅 * 是通配符，h* 匹配 h 开头，空值按 *；管理员也须匹配，绑定服务账号始终排除。
IPA_USER_MATCH=h*

# LDAP 查询凭据
# 专用只读目录查询账号的 DN；应有读取用户与组属性的权限，不能作为应用用户登录。
IPA_BIND_DN=uid=app_bind_oneapi,cn=users,cn=accounts,dc=example,dc=com
# 目录查询账号的密码；保存在受保护的 .env 中，不放入服务文件、命令参数或版本库。
IPA_BIND_PASSWORD="REPLACE_WITH_REAL_BIND_PASSWORD"

# 应用管理角色
# 应用管理员组的 DN；按 IPA 组成员资格授予管理权限，管理员访问时会核验 IPA 身份及成员资格。
IPA_ADMIN_GROUP_DN=cn=one-api-admins,cn=groups,cn=accounts,dc=example,dc=com
# 应用超级管理员组的 DN；未设置或留空时不通过该组授予超级管理员。
IPA_ROOT_GROUP_DN=cn=one-api-roots,cn=groups,cn=accounts,dc=example,dc=com

# CA 信任与挂载
# 用于验证 LDAP TLS 证书的 PEM CA 文件或证书链路径；相对路径按应用工作目录解析，空值使用系统信任库。
IPA_CA_CERT=/etc/one-api/cert/ca.crt

# 目录同步
# IPA 目录同步间隔，单位秒，默认 300；启动时也同步，用户列表翻页只读取本地数据。
IPA_SYNC_FREQUENCY=300
```

可使用 `openssl rand -hex 32` 生成会话密钥，然后填入 `SESSION_SECRET`。每次启动都更换密钥会导致旧浏览器会话失效。不要把示例占位符作为真实密钥或密码使用。

| 配置 | 说明 |
| --- | --- |
| `PORT` | 网页、管理 API 和模型 API 共用的监听端口；服务监听所有网卡。 |
| `DEBUG` | 是否启用调试模式；默认 false。 |
| `THEME` | 前端主题；默认 default，仅构建默认主题时使用 default。 |
| `HTTPS_PROXY` | 支持环境代理的出站 HTTP/HTTPS 请求使用的代理；不用于监听、网站 HTTPS 或 LDAP，不需要时留空。 |
| `SESSION_SECRET` | 固定会话签名密钥；保留原值可避免正常重启后会话失效，不使用示例占位符。 |
| `SQL_DSN` | 数据库连接字符串；留空使用 SQLite，已有 MySQL/PostgreSQL 安装保留原配置。 |
| `SQLITE_PATH` | SQLite 文件路径；相对路径按应用工作目录解析，父目录须存在，迁移时确保继续使用原业务数据。 |
| `REDIS_CONN_STRING` | Redis 连接字符串；不使用 Redis 时留空，已有安装保留原配置。 |
| `REGISTER_ENABLED` | false 禁止公开密码注册和首次 OAuth 账号创建；不影响已有账号登录或 IPA 同步，IPA_ONLY=true 时也禁止公开注册。 |
| `IPA_ENABLED` | 是否启用 IPA 登录；默认 false，IPA_ONLY=true 时强制启用；启用后管理接口要求 IPA 身份及应用角色组资格。 |
| `IPA_ONLY` | true 仅允许 IPA 身份登录和使用，排除本地账号并禁止非 IPA 账号创建；空数据库不创建本地 root。 |
| `IPA_URL` | LDAP 服务地址；推荐 ldaps://域名:636，域名须与证书匹配，ldap://域名:389 须同时启用 StartTLS。 |
| `IPA_STARTTLS` | 是否在 LDAP 连接上启用 StartTLS；默认 false，使用 ldap:// 地址时设为 true。 |
| `IPA_BASE_DN` | IPA 目录的 Base DN，用于限定查询范围。 |
| `IPA_USER_BASE_DN` | 用户查询范围；未设置或留空时使用 IPA_BASE_DN。 |
| `IPA_USER_MATCH` | 按 IPA 登录名 uid 匹配，忽略大小写；仅 * 是通配符，h* 匹配 h 开头，空值按 *；管理员也须匹配，绑定服务账号始终排除。 |
| `IPA_BIND_DN` | 专用只读目录查询账号的 DN；应有读取用户与组属性的权限，不能作为应用用户登录。 |
| `IPA_BIND_PASSWORD` | 目录查询账号的密码；保存在受保护的 .env 中，不放入服务文件、命令参数或版本库。 |
| `IPA_ADMIN_GROUP_DN` | 应用管理员组的 DN；按 IPA 组成员资格授予管理权限，管理员访问时会核验 IPA 身份及成员资格。 |
| `IPA_ROOT_GROUP_DN` | 应用超级管理员组的 DN；未设置或留空时不通过该组授予超级管理员。 |
| `IPA_CA_CERT` | 用于验证 LDAP TLS 证书的 PEM CA 文件或证书链路径；相对路径按应用工作目录解析，空值使用系统信任库。 |
| `IPA_CA_CERT_HOST_DIR` | 仅供 Docker Compose 使用的宿主机 CA 目录，挂载到容器 /run/one-api/certs；IPA_CA_CERT 须指定容器内证书路径，源码部署无需此项。 |
| `IPA_SYNC_FREQUENCY` | IPA 目录同步间隔，单位秒，默认 300；启动时也同步，用户列表翻页只读取本地数据。 |

三处配置按相同字段顺序和解释整理，但保留各自已有字段与值。上表也列出模板或示例中的可选字段；未出现在某份配置中的字段没有自动补入。Docker Compose 专用的 IPA_CA_CERT_HOST_DIR 不用于本文的源码部署。

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
# 账号模式与注册
# false 禁止公开密码注册和首次 OAuth 账号创建；不影响已有账号登录或 IPA 同步，IPA_ONLY=true 时也禁止公开注册。
REGISTER_ENABLED=false
# true 仅允许 IPA 身份登录和使用，排除本地账号并禁止非 IPA 账号创建；空数据库不创建本地 root。
IPA_ONLY=false
```

`IPA_ENABLED` 按实际需要保留：`false` 使用原本地账号机制，`true` 允许 IPA 与本地账号混合使用。`REGISTER_ENABLED=false` 独立生效，后台不能通过注册开关覆盖这一部署限制；它阻止公开密码注册，以及 GitHub、OIDC、飞书、微信等登录入口首次创建账号。已存在账号的登录不因此关闭，IPA 目录同步和 IPA 身份首次映射仍可执行，管理员手工创建本地账号也仍受原有管理权限及 `IPA_ONLY` 限制。

运行配置保存在 `/etc/one-api/.env`，通过 `sudoedit /etc/one-api/.env` 修改，然后执行 `sudo systemctl restart one-api`。修改源码目录中的准备文件不会自动更新已安装的配置。恢复公开注册时将 `REGISTER_ENABLED=true`，还需确保后台注册开关已开启；它不是强制打开注册的开关。

**启用 IPA 会改变管理资格。** 当前实现中，即使 `IPA_ONLY=false`，旧本地 `admin/root` 也不能凭原有角色访问管理接口。切换前应先准备符合 `IPA_USER_MATCH` 且直接属于 `IPA_ROOT_GROUP_DN` 指定组的 IPA 用户，避免切换后没有可用的应用超级管理员。账号同名不会自动合并，已有安装的身份关联与数据保留要求见[数据库升级说明](./database-freeipa-upgrade.md)。

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
# 数据库与缓存
# 数据库连接字符串；留空使用 SQLite，已有 MySQL/PostgreSQL 安装保留原配置。
SQL_DSN=
# SQLite 文件路径；相对路径按应用工作目录解析，父目录须存在，迁移时确保继续使用原业务数据。
SQLITE_PATH=/var/lib/one-api/one-api.db

# CA 信任与挂载
# 用于验证 LDAP TLS 证书的 PEM CA 文件或证书链路径；相对路径按应用工作目录解析，空值使用系统信任库。
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

### 10.2 备份运行中的应用

采用 SQLite 时，推荐短暂停止应用后备份配置、数据库、程序和服务文件，再立即启动。确认其他进程也不写入该数据库，不能在持续写入期间直接复制 `.db` 文件并视为完整备份。

以下适用于本文的系统级服务与标准目录，由普通部署账号在 Bash 中执行；`ONE_API_DIR` 指向源码目录。命令会短暂停机，备份保存在项目 `.runtime/backups/` 中，便于随项目复制。先确认磁盘空间，并选择适当的维护时段。

```bash
(
  set -e
  umask 077
  sudo -v
  mkdir -p "$ONE_API_DIR/.runtime/backups"
  ONE_API_BACKUP_DIR="$(mktemp -d "$ONE_API_DIR/.runtime/backups/$(date +%F_%H%M%S)-XXXXXX")"
  sudo systemctl is-active --quiet one-api
  sudo systemctl stop one-api
  # 备份命令失败时也尝试恢复原来运行的服务。
  trap 'sudo systemctl start one-api' EXIT
  sudo tar -czf "$ONE_API_BACKUP_DIR/runtime.tar.gz" -C / \
    opt/one-api etc/one-api var/lib/one-api var/log/one-api \
    etc/systemd/system/one-api.service
  sudo chown "$(id -u):$(id -g)" "$ONE_API_BACKUP_DIR/runtime.tar.gz"
  chmod 600 "$ONE_API_BACKUP_DIR/runtime.tar.gz"
  sudo systemctl start one-api
  trap - EXIT
  # 源码、Git 历史、未提交配置与本地讨论记录另存；排除备份本身与构建依赖。
  tar --exclude='./.runtime' --exclude='./logs' \
    --exclude='./web/default/node_modules' --exclude='./web/air/node_modules' \
    --exclude='./web/berry/node_modules' \
    -czf "$ONE_API_BACKUP_DIR/source.tar.gz" -C "$ONE_API_DIR" .
  (cd "$ONE_API_BACKUP_DIR" && sha256sum runtime.tar.gz source.tar.gz > SHA256SUMS)
  printf '备份目录：%s\n' "$ONE_API_BACKUP_DIR"
)
```

备份完成后用 `sudo systemctl status one-api --no-pager` 确认已恢复运行；失败时检查错误，不能把未完整生成的压缩包当作成功备份。保存打印的目录和 `SHA256SUMS`，复制到另一台设备后可在该目录执行 `sha256sum -c SHA256SUMS` 检查传输完整性；校验和不等同于恢复验证。

若配置了其他数据路径，应调整归档路径。运行包含真实密码、会话密钥和业务数据，权限保持 600，存放目录保持 700，并另存到受保护的其他设备；只保存在同一磁盘不能防止磁盘故障。Nginx 站点和 `/etc/letsencrypt/` 中使用的证书、续期配置需另行备份；它们可能被其他网站共享，不盲目覆盖恢复。

恢复时先停止应用，将运行文件恢复到对应目录，再按第 7 节设置程序、配置、CA、数据库与日志权限，执行 daemon-reload 并启动；换电脑时先创建服务账号，不能盲目保留原电脑数字 UID/GID。应保留与数据库备份对应的程序版本，避免直接降级程序后读取升级过的数据库。

**若使用此前的用户级服务**，停止和启动使用 `systemctl --user stop/start one-api`，数据库位置取实际项目 `.env` 中的 `SQLITE_PATH`，同时备份项目目录、用户服务 unit 及项目链接说明，不能照搬上述 `/var/lib/one-api` 路径。

MySQL/PostgreSQL 应使用其数据库备份工具；文件归档不能备份远端数据库。必须不停机时，SQLite 可使用在线 Backup API 或相应的 `.backup` 工具形成一致快照，再归档快照与配置，不直接复制正在写入的文件。方法见 [SQLite 官方备份说明](https://www.sqlite.org/backup.html)。LDAP 目录本身也需由目录管理员另外备份，本应用备份不能替代目录备份。

### 10.3 迁移到另一台电脑

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

### 10.4 常见问题

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

### 10.5 使用第三方工具查看和编辑 SQLite

SQLite 是文件数据库，可以通过第三方工具打开、查询和修改。优先推荐以下桌面工具，不需要在应用服务器安装图形界面：

| 工具 | 适合场景 |
| --- | --- |
| [DB Browser for SQLite](https://sqlitebrowser.org/) | 首选，界面直观，支持浏览、编辑表数据和执行 SQL |
| [Letos，原 SQLiteStudio](https://letos.org/) | 专注 SQLite，支持跨平台及便携运行 |
| [DBeaver Community](https://dbeaver.io/about/) | 同时管理 SQLite、MySQL 等多种数据库；编辑功能见[官方说明](https://dbeaver.com/docs/dbeaver/Data-Viewing-and-Editing/) |

查看数据时，先按 10.2 生成一致备份，再用工具打开副本。修改实际数据时遵循以下步骤：

1. 确认实际运行配置中的 `SQLITE_PATH`，不要误选源码目录中的旧数据库。
2. 停止应用，并保存可恢复的数据库及相关旁文件备份。
3. 打开数据库或其工作副本，编辑后执行工具的保存或提交操作。
4. 关闭工具及数据库连接。若编辑后复制回服务器，保持应用停止，不能覆盖此期间已产生新业务数据的数据库；目标旧旁文件须与原库一起妥善保存，不能与新库混用。
5. 按第 7 节恢复服务账号的文件所有权和访问权限，再启动应用。

远端 SQLite 不能像 MySQL 一样通过数据库端口连接；可在受控复制的副本上操作。不要把业务库上传到公共在线编辑网站。系统级服务与用户级服务的配置位置及启停命令不同，以实际部署方式为准。

直接编辑会绕过应用业务逻辑，需理解对应字段的含义：

- LDAP 身份标识、登录名、角色和目录锁定字段受同步控制，手工修改可能破坏账号映射或被后续同步覆盖；目录用户与组角色在 LDAP 管理端维护。
- 本地密码字段保存哈希，不填写明文；LDAP 密码仍由目录服务管理。
- 额度字段使用系统内部计量单位，不直接等同于金额；充值、额度和本地启用/禁用优先在应用后台执行。
- 启用 Redis 时，直接 SQL 修改不会触发应用缓存失效；重启应用也不保证清除 Redis 中的旧数据，应结合对应字段处理缓存。

工具适合查询、分析和维护；修改结构前另行评估自动迁移及数据兼容性。本文仅推荐工具，未安装软件或执行数据库编辑。

服务与账号参数依据 Ubuntu 24.04 自带的 `man systemd.service`、`man systemd.exec`、`man useradd`。本文命令用于管理员按步骤部署，修改文档不代表已在本机执行账号创建或服务切换。
