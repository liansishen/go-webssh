# Go WebSSH

轻量 WebSSH 服务：用浏览器通过 **Go + xterm.js** 连接多台 SSH 主机。单个 Go 进程同时提供 Web UI 与全部 SSH 会话，无需为每台目标单独启动 ttyd。

## 安全声明（必读）

本项目是**服务端代理式 WebSSH**：

```text
浏览器 / go-webssh-cli -> (HTTP 代理 CONNECT) -> Go WebSSH 服务端 -> 目标 SSH 服务器
```

- **私钥会发送到 WebSSH 服务端内存**，用于 SSH 握手与认证。
- 未明确保存时，私钥**不会**写入磁盘、数据库、日志或浏览器 `localStorage` / `sessionStorage`。
- 若启用加密凭据库并主动保存连接，私钥会以 AES-256-GCM 加密后存入本地数据库（见下文）。
- 请只在你信任的部署上使用私钥；生产环境务必 HTTPS。
- 建议使用专用 SSH key，并在目标机 `authorized_keys` 中限制来源 IP。

## 功能

### 连接与终端

- 单用户 Web 登录（bcrypt `password_hash`，开发可用明文 `password`）
- 可选保持登录 30 天（加密 Cookie，服务重启后仍有效）
- 私钥登录（ed25519 / RSA / ECDSA，支持 passphrase）
- WebSocket 交互式终端（xterm.js + fit）
- 多连接标签；每个标签最多 4 个 SSH 窗格，支持向右/向下分屏与拖动分隔线
- 断线自动重连最多 5 次；可选 Herdr 恢复远端会话
- 右键菜单：复制、粘贴、全选、清屏、分屏、关闭窗格（含快捷键提示）
- 安全粘贴确认（多行 / 控制字符）；`Ctrl+Shift+C/V`（macOS：`Cmd+C/V`）
- 可选 OSC 52 远程剪贴板写入，支持 Herdr 等终端程序通过 SSH 将复制内容写入浏览器所在设备
- 本机终端 CLI（`go-webssh-cli`）：经 HTTP 代理登录 WebSSH，在本地终端接入同一条 PTY 会话

### 界面与主题

- 中英文切换、日间/夜间模式
- 终端主题、字体、字号、行高等设置（localStorage）
- 主题可从本地目录加载；设置页提供色块预览与可读名称
- 监控侧栏可折叠为窄轨；折叠时暂停刷新监控数据
- 只读快捷键面板（顶栏键盘图标）

### 安全与运维

- Host Key：`known-hosts`（默认）或 `insecure-ignore`
- Network Policy：默认拒绝 loopback、link-local、metadata、私网（可配置）
- 可选 AES-256-GCM 加密凭据库（独立随机主密钥）
- 前端资源嵌入单二进制
- Linux 目标机 CPU / 内存 / 根分区磁盘 / 网络 / 负载 / 运行时间与 WebSocket 延迟监控

## 快速启动

### Linux 一键安装

```bash
curl -fsSL https://raw.githubusercontent.com/liansishen/go-webssh/main/install.sh | sudo bash
```

脚本支持 Linux amd64/arm64，校验 Release 的 SHA256，安装二进制、生成配置并启用 systemd。重复执行会升级二进制并保留已有配置。

可选环境变量：

```bash
GOWEBSSH_VERSION=v0.5.17 \
GOWEBSSH_LISTEN=127.0.0.1:8080 \
GOWEBSSH_USERNAME=admin \
GOWEBSSH_ALLOW_PRIVATE_RANGES=false \
sudo -E bash install.sh
```

### 从源码启动

```bash
cd go-webssh
go test ./...
go build -o go-webssh ./cmd/webssh

export GOWEBSSH_USERNAME=admin
export GOWEBSSH_PASSWORD='strong-password'
export GOWEBSSH_SESSION_SECRET='change-me-random-32bytes'
# 开发可跳过 host key；生产请使用 known-hosts
export GOWEBSSH_HOST_KEY_POLICY=insecure-ignore

./go-webssh --listen 127.0.0.1:8080
```

浏览器访问：`http://127.0.0.1:8080/`

```bash
curl -s http://127.0.0.1:8080/api/healthz
# {"ok":true}
```

## 终端 CLI

当本机出站 SSH 被局域网拦截，但浏览器能打开 WebSSH 时，可用 `go-webssh-cli` 在本地终端里连同一条通道。CLI 走 HTTPS/WebSocket，并且会用 `HTTP_PROXY` / `HTTPS_PROXY` 向局域网 HTTP 代理发 `CONNECT`。

```text
本机终端  --PTY I/O-->  go-webssh-cli
                              |
                              |  HTTP CONNECT webssh:443
                              v
                        局域网 HTTP 代理
                              |
                              v
                        go-webssh  /api/login + /api/ws/ssh
                              |
                              |  服务端 ssh.Dial + PTY
                              v
                        目标 SSH 服务器
```

这是浏览器 xterm.js 的本地替代，不是 TCP 隧道。**不能**用它跑 `scp`、`sftp`、`git+ssh`、VS Code Remote 或 SSH 端口转发。私钥仍会发到 WebSSH 服务端内存，与浏览器连接相同。

```bash
export https_proxy=http://lan-proxy:8080
export GOWEBSSH_URL=https://webssh.example.com
export GOWEBSSH_USERNAME=admin
# 密码也可不写进环境，CLI 会提示
export GOWEBSSH_PASSWORD='your-web-password'

go-webssh-cli -i ~/.ssh/id_ed25519 user@target.example.com
```

Windows（PowerShell / Windows Terminal / cmd）下载 `go-webssh-cli_*_windows_amd64.zip`：

```powershell
$env:HTTPS_PROXY = "http://lan-proxy:8080"
$env:GOWEBSSH_URL = "https://webssh.example.com"
$env:GOWEBSSH_USERNAME = "admin"
$env:GOWEBSSH_PASSWORD = "your-web-password"
.\go-webssh-cli.exe -i $env:USERPROFILE\.ssh\id_ed25519 user@target.example.com
```

请使用 Windows Terminal、PowerShell 或 cmd。Git Bash（mintty）不是 Windows 控制台，raw 模式可能不可用。

使用浏览器里已保存的连接：

```bash
go-webssh-cli --list
go-webssh-cli --saved prod
```

常用参数：

| 参数 / 环境变量 | 说明 |
|---|---|
| `--url` / `GOWEBSSH_URL` | WebSSH 基址，例如 `https://webssh.example.com` |
| `--web-user` / `GOWEBSSH_USERNAME` | Web 登录用户名 |
| `--web-password` / `GOWEBSSH_PASSWORD` | Web 登录密码；建议用环境变量而不是命令行 |
| `--proxy` / `GOWEBSSH_PROXY` | HTTP 代理 URL；默认使用 `HTTP_PROXY`/`HTTPS_PROXY` |
| `--no-proxy` | 不走代理 |
| `--insecure` | 跳过 WebSSH 服务端的 TLS 证书校验 |
| `-i` | 本机 SSH 私钥 |
| `--saved` | 用已保存凭据的 id 或名称 |
| `--herdr` / `--tmux` | 请求 Herdr 会话恢复 |

发布压缩包含 Linux 的 `go-webssh` / `go-webssh-cli`，以及 Windows 的 `go-webssh-cli.exe`。从源码构建：

```bash
go build -o go-webssh-cli ./cmd/webssh-cli
GOOS=windows GOARCH=amd64 go build -o go-webssh-cli.exe ./cmd/webssh-cli
```

## 配置

示例见 [config.example.yaml](./config.example.yaml)。

```yaml
server:
  listen: "127.0.0.1:8080"
  session_secret: "change-me-random-32bytes-min-16"
  secure_cookie: false

auth:
  username: "admin"
  password_hash: "$2a$12$..."
  session_ttl: "12h"

ssh:
  connect_timeout: "15s"
  idle_timeout: "30m"
  max_sessions: 5
  host_key_policy: "known-hosts"   # or insecure-ignore
  known_hosts_file: "./known_hosts"

network_policy:
  allow_private_ranges: false
  deny:
    - "127.0.0.0/8"
    - "169.254.0.0/16"
    - "::1/128"
    - "0.0.0.0/8"

logging:
  level: "info"

credentials:
  enabled: true
  db_file: "./credentials.db"
  key_file: "./credentials.db.key"

ui:
  themes_dir: "./themes"
```

```bash
./go-webssh --config config.yaml --listen 127.0.0.1:8080
```

### 配置优先级

```text
命令行参数 > 环境变量 > 配置文件 > 默认值
```

### 常用环境变量

| 变量 | 说明 |
|---|---|
| `GOWEBSSH_LISTEN` | 监听地址 |
| `GOWEBSSH_USERNAME` | Web 登录用户名 |
| `GOWEBSSH_PASSWORD` | 明文密码（仅建议开发） |
| `GOWEBSSH_PASSWORD_HASH` | bcrypt 密码哈希 |
| `GOWEBSSH_SESSION_SECRET` | Session 密钥（至少 16 字符） |
| `GOWEBSSH_HOST_KEY_POLICY` | `known-hosts` / `insecure-ignore` |
| `GOWEBSSH_KNOWN_HOSTS_FILE` | known_hosts 路径 |
| `GOWEBSSH_ALLOW_PRIVATE_RANGES` | `true` 允许 RFC1918 等私网 |
| `GOWEBSSH_SECURE_COOKIE` | `true` 设置 Secure cookie |
| `GOWEBSSH_LOG_LEVEL` | `debug` / `info` / `warn` / `error` |
| `GOWEBSSH_CREDENTIALS_KEY_FILE` | 凭据主密钥文件路径（默认 `<db_file>.key`） |
| `GOWEBSSH_CREDENTIALS_KEY` | 64 位十六进制凭据主密钥，优先于密钥文件 |
| `GOWEBSSH_THEMES_DIR` | 终端主题 JSON 目录（默认 `./themes`） |

## 生成 bcrypt password_hash

```bash
cat > /tmp/gowebssh-hash.go <<'EOF'
package main

import (
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	h, err := bcrypt.GenerateFromPassword([]byte(os.Args[1]), 12)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(h))
}
EOF

go run /tmp/gowebssh-hash.go 'your-password'
```

将输出写入 `auth.password_hash`，并删除明文 `password`。

## Host Key 策略

| 策略 | 行为 |
|---|---|
| `known-hosts`（默认） | 使用 `known_hosts_file` 校验；未知或变更的 host key 会拒绝连接 |
| `insecure-ignore` | 跳过校验；启动日志与 UI 会显示警告，存在中间人风险 |

生产建议：

```bash
ssh-keyscan -H example.com >> known_hosts
```

```yaml
ssh:
  host_key_policy: known-hosts
  known_hosts_file: ./known_hosts
```

## Network Policy

默认避免把 WebSSH 变成内网扫描或 SSRF 跳板：

- 拒绝 `localhost` / `127.0.0.0/8` / `::1`
- 拒绝 `0.0.0.0/8`
- 拒绝 `169.254.0.0/16`（含云 metadata `169.254.169.254`）
- 默认拒绝私网（`10/8`、`172.16/12`、`192.168/16`、`fc00::/7`、`fe80::/10`）
- 域名会解析；**任一**解析结果命中拒绝列表即拒绝

连接内网目标：

```yaml
network_policy:
  allow_private_ranges: true
```

或：`GOWEBSSH_ALLOW_PRIVATE_RANGES=true`。

即使允许私网，loopback 与 link-local/metadata 仍默认拒绝。确有需要时，用 `network_policy.allow` 显式放行。

## 私钥与加密凭据

### 默认行为

- 连接时私钥经 WebSocket 进入服务端内存
- 不写盘、不入库、不记日志、不进浏览器存储
- 连接页有明确安全提示

### 可选加密保存

启用 `credentials.enabled` 后，可在连接页**主动保存**主机、私钥与 passphrase：

- 服务首次启动时生成独立的 256 位随机主密钥，默认保存到 `<db_file>.key`
- 凭据使用 AES-256-GCM 加密后写入 bbolt；数据库记录带版本标记
- 登录密码只用于 Web 身份认证，修改登录密码不会影响已保存凭据
- 主密钥文件和数据库权限均为 `0600`，必须成对备份；丢失主密钥后无法恢复凭据
- 升级前由登录密码派生密钥加密的记录会被保留，不再自动删除。明文 `auth.password` 部署会在启动时自动迁移；仅配置 bcrypt 哈希时，前端会提示输入此前的登录密码完成迁移

## 多连接、重连与监控

- 标签栏 `＋` 打开连接设置；主页可管理已保存凭据
- 每个标签对应一台服务器，最多 4 个窗格；同标签共享左侧监控数据
- WebSocket 断开后指数退避重连（约 1、2、4、8、15 秒），最多 5 次
- 启用 Herdr 恢复时使用 `herdr session attach <name>` 创建或恢复命名会话（目标机需安装 Herdr）
- Linux 目标约每 3 秒采集 `/proc` 指标；非 Linux 或不支持时监控显示不可用，不影响终端

## 键盘快捷键

终端页可用（焦点在终端内同样生效）。界面中以独立键帽展示。

| 动作 | Windows / Linux | macOS |
|---|---|---|
| 折叠/展开侧栏 | `Ctrl` `Shift` `\` | `Ctrl` `Shift` `\` |
| 向右分屏 | `Ctrl` `Shift` `→` | `Ctrl` `Shift` `→` |
| 向下分屏 | `Ctrl` `Shift` `↓` | `Ctrl` `Shift` `↓` |
| 关闭窗格 | `Ctrl` `Shift` `X` | `Ctrl` `Shift` `X` |
| 下一/上一窗格 | `Ctrl` `Shift` `]` / `[` | `Ctrl` `Shift` `]` / `[` |
| 复制 / 粘贴 | `Ctrl` `Shift` `C` / `V` | `⌘` `C` / `V` |

- 有意避开会关闭浏览器窗口/标签的组合（如 `Ctrl+Shift+W`、`Ctrl+PageUp/Down`）
- 顶栏快捷键面板只读，当前版本不可自定义

## 远程程序剪贴板（OSC 52）

Herdr 在 SSH 会话中复制文本时会输出 OSC 52 序列，请求最外层终端写入系统剪贴板。浏览器默认不会处理该序列，因此需要在 **设置** 中开启“允许远程程序写入浏览器剪贴板（OSC 52）”。

- 该能力默认关闭，只应对可信远程主机开启；远程程序能够覆盖浏览器所在设备的文本剪贴板。
- WebSSH 只处理远程写入请求，不响应远程读取剪贴板的请求。
- 浏览器剪贴板 API 通常要求 HTTPS 或 `localhost`。如果后台写入被浏览器拦截，页面会显示内容预览，并要求点击“复制到剪贴板”完成复制。
- 当前限制为每次最多 1 MiB 的解码后文本；无效或超限的 OSC 52 内容会被忽略。

Herdr 中选中文本并执行复制后，内容会沿以下路径传递：

```text
Herdr -> OSC 52 -> SSH PTY -> Go WebSSH -> xterm.js -> 浏览器剪贴板
```

## 自定义终端主题

使用 xterm.js `ITheme` JSON（颜色表），与页面日夜间外观无关。

- 内置主题已嵌入二进制
- 仓库 [themes/](./themes) 提供同款模板
- `ui.themes_dir`（默认 `./themes`，环境变量 `GOWEBSSH_THEMES_DIR`）下的 `*.json` 会合并进下拉列表
- 同名文件覆盖内置主题；新文件追加
- 扫描在请求 `GET /themes.js` 与 `GET /api/config/ui` 时进行，通常无需重启；浏览器需刷新
- 文件名即主题 id；UI 会美化显示（如 `catppuccin-mocha` → `Catppuccin Mocha`）并展示色块

内置：`github-light`、`solarized-light`、`catppuccin-latte`、`catppuccin-mocha`、`dracula`、`tokyo-night`、`one-dark`、`nord`

```bash
cp themes/catppuccin-mocha.json ./themes/my-theme.json
# 编辑颜色后刷新浏览器即可
```

最小字段：

```json
{
  "background": "#1e1e2e",
  "foreground": "#cdd6f4"
}
```

完整字段见 [themes/README.md](./themes/README.md)。

## HTTPS 反代示例

后端监听本机，由反代终止 TLS。

### Caddy

```caddy
ssh.example.com {
  reverse_proxy 127.0.0.1:8080
}
```

### nginx

```nginx
server {
  listen 443 ssl http2;
  server_name ssh.example.com;

  ssl_certificate     /etc/ssl/certs/fullchain.pem;
  ssl_certificate_key /etc/ssl/private/privkey.pem;

  location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 3600s;
    proxy_send_timeout 3600s;
  }
}
```

生产建议：

```yaml
server:
  secure_cookie: true
```

## API 摘要

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/` | 前端 |
| GET | `/api/healthz` | 健康检查 |
| POST | `/api/login` | `{username,password,remember}` |
| POST | `/api/logout` | 退出 |
| GET | `/api/session` | 当前会话 |
| GET | `/api/config/ui` | UI 配置（含 `themes`、`themesDir`） |
| GET | `/themes.js` | 动态主题表 `window.GOWEBSSH_THEMES` |
| GET | `/api/ws/ssh` | WebSocket（需登录；首条 `connect`） |
| GET/POST/DELETE | `/api/credentials`… | 加密凭据 CRUD |

## 构建与发布

### 本地构建检查

```bash
go test ./...
go vet ./...
go build -o go-webssh ./cmd/webssh
go build -o go-webssh-cli ./cmd/webssh-cli
```

### 正式发布流程

项目没有 `release` 分支。正式发布通过向 `main` 推送版本提交，再推送以 `v` 开头的版本 Tag 完成。

发布前需要同步以下版本号：

- `cmd/webssh/main.go` 与 `cmd/webssh-cli/main.go` 中的默认 `version`；
- 本文档安装示例中的 `GOWEBSSH_VERSION`；
- `web/index.html` 中有改动的前端静态资源查询版本，用于刷新浏览器缓存。

以发布 `v0.5.10` 为例：

```bash
# 1. 确认工作区和分支
git status --short --branch

# 2. 运行发布前检查
gofmt -w $(find cmd internal themes web -name '*.go')
go test ./...
go vet ./...
go build ./...
node --check web/app.js
git diff --check

# 3. 提交并推送 main
git add -A
git commit -m "Release v0.5.10"
git push origin main

# 4. 在已推送的发布提交上创建并推送标签
git tag -a v0.5.10 -m "Go WebSSH v0.5.10"
git push origin v0.5.10
```

`.github/workflows/release.yml` 会在版本 Tag 推送后自动执行：

1. 运行 `go test ./...` 和 `go vet ./...`；
2. 交叉编译 Linux amd64/arm64；
3. 生成发布压缩包、`install.sh` 和 `SHA256SUMS`；
4. 创建或更新对应的 GitHub Release，并上传构建产物；
5. 通过构建参数将 Tag 名写入发布二进制的 `go-webssh --version`。

推送后可使用以下命令确认工作流和 Release 状态：

```bash
gh run list --workflow release.yml --limit 5
gh release view v0.5.10
```

如果工作流失败，应先修复问题并重新运行工作流；不要复用同一个版本号发布不同源码。如发布 Tag 尚未被其他人使用且确实必须更正，应明确检查远端状态后再处理。

可选：本机已用 systemd 安装时，可用 [scripts/deploy-test.sh](./scripts/deploy-test.sh) 从源码构建并重启服务（保留配置）：

```bash
sudo ./scripts/deploy-test.sh
```

## 常见问题

**连接失败提示 host key？**  
默认 `known-hosts`。用 `ssh-keyscan` 写入 known_hosts，或在明确风险下使用 `insecure-ignore`。

**连不上内网 IP？**  
默认 `allow_private_ranges=false`，需要时显式打开。

**连不上 127.0.0.1？**  
默认禁止 loopback 以防 SSRF。确有需要时用 `network_policy.allow` 显式放行。

**私钥安全吗？**  
会经过服务端内存。请使用 HTTPS、强登录密码、受信服务器与专用 key。需要持久化时再用加密凭据库，并知悉改密影响。

**为什么 Herdr 中复制的文本没有进入本机剪贴板？**

Herdr 在 SSH 场景使用 OSC 52 传递复制内容。请在 WebSSH 设置中开启 OSC 52 远程剪贴板写入，并使用 HTTPS 或 `localhost` 访问页面；如果浏览器不允许后台写入，请在弹出的预览框中点击“复制到剪贴板”。

**为什么不用每台目标一个 ttyd？**  
本项目单进程多会话，浏览器指定目标与私钥即可连接不同主机。

## License

本项目采用 [MIT License](./LICENSE) 开源。
