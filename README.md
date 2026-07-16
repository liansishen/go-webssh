# Go WebSSH

轻量 WebSSH 服务：用浏览器通过 **Go + xterm.js** 连接多台 SSH 目标主机。单个 Go 进程处理 Web UI 与全部 SSH 会话，无需为每台目标服务器启动独立 ttyd/进程。

## 安全声明（必读）

本项目是**服务端代理式 WebSSH**：

```text
浏览器 -> Go WebSSH 服务端 -> 目标 SSH 服务器
```

- **私钥会发送到 WebSSH 服务端内存**，用于 SSH 握手与认证。
- **默认不会**把私钥/passphrase 保存到磁盘、数据库、日志或浏览器 `localStorage`/`sessionStorage`。
- 请只在你信任的 WebSSH 部署上使用私钥；生产环境务必 HTTPS。
- 建议使用专用 SSH key，并在目标机 `authorized_keys` 中限制来源 IP。

## 功能（MVP）

- 单用户 Web 登录（bcrypt `password_hash` 或开发用明文 `password`）
- 登录页可选择保持登录状态 30 天；会话使用浏览器中的加密认证 Cookie，服务重启后仍有效；用户名下拉菜单提供退出登录操作
- 私钥登录（ed25519 / RSA / ECDSA，支持 passphrase）
- WebSocket 交互式终端（xterm.js + fit）
- 终端 resize、主题/字体/字号等 UI 设置（localStorage）
- Host Key：`known-hosts`（默认）或显式 `insecure-ignore`
- Network Policy：默认拒绝 loopback / link-local / metadata / 私网（可配置）
- 前端资源 `embed` 进单二进制
- AES-256-GCM 加密凭据库（密钥由登录密码经 Argon2id 派生）
- 应用内多连接标签页；每个标签最多包含 4 个独立 SSH 窗格
- 网络中断最多自动重连 5 次；启用 tmux 时恢复原远端终端
- Linux 服务器 CPU、内存、网络、负载、运行时间和 WebSocket 往返延迟实时监控
- 登录后主页统一管理保存的连接，连接和终端设置使用模态窗口
- English/简体中文即时切换，以及登录前后的日间/夜间快捷按钮
- 终端监控侧栏支持拖动宽度，并提供单会话断开和重连操作
- 终端支持自定义多个本机字体名称，并遵循 CSS fallback 顺序
- 每个服务器标签支持最多 4 个独立 SSH 窗格，可向右/向下分屏、四宫格显示并拖动分隔线
- 终端右键使用自定义菜单，提供复制、粘贴、全选、清屏、分屏和窗格关闭操作
- 支持 `Ctrl+Shift+C/V`（macOS 为 `Cmd+C/V`）复制粘贴，多行或控制字符粘贴可安全确认

## 快速启动

```bash
cd go-webssh
go test ./...
go build -o go-webssh ./cmd/webssh

export GOWEBSSH_USERNAME=admin
export GOWEBSSH_PASSWORD='strong-password'
export GOWEBSSH_SESSION_SECRET='change-me-random-32bytes'
# 开发调试可跳过 host key（生产请用 known-hosts）
export GOWEBSSH_HOST_KEY_POLICY=insecure-ignore

./go-webssh --listen 127.0.0.1:8080
```

浏览器访问：`http://127.0.0.1:8080/`

健康检查：

```bash
curl -s http://127.0.0.1:8080/api/healthz
# {"ok":true}
```

## 配置文件示例

见 [config.example.yaml](./config.example.yaml)。

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
```

启动：

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
| `GOWEBSSH_PASSWORD` | Web 登录明文密码（仅建议开发） |
| `GOWEBSSH_PASSWORD_HASH` | bcrypt 密码哈希 |
| `GOWEBSSH_SESSION_SECRET` | Session 相关密钥（至少 16 字符） |
| `GOWEBSSH_HOST_KEY_POLICY` | `known-hosts` / `insecure-ignore` |
| `GOWEBSSH_KNOWN_HOSTS_FILE` | known_hosts 路径 |
| `GOWEBSSH_ALLOW_PRIVATE_RANGES` | `true` 允许 RFC1918 等私网 |
| `GOWEBSSH_SECURE_COOKIE` | `true` 设置 Secure cookie |
| `GOWEBSSH_LOG_LEVEL` | `debug`/`info`/`warn`/`error` |

## 生成 bcrypt password_hash

```bash
# 在项目目录执行
cat > /tmp/gowebssh-hash.go <<'EOF'
package main

import (
\t"fmt"
\t"os"

\t"golang.org/x/crypto/bcrypt"
)

func main() {
\th, err := bcrypt.GenerateFromPassword([]byte(os.Args[1]), 12)
\tif err != nil {
\t\tpanic(err)
\t}
\tfmt.Println(string(h))
}
EOF

cd /path/to/go-webssh
go run /tmp/gowebssh-hash.go 'your-password'
```

将输出写入配置的 `auth.password_hash`，并删除明文 `password`。

## Host Key 策略

| 策略 | 行为 |
|---|---|
| `known-hosts`（默认） | 使用 `known_hosts_file` 校验。未知/变更的 host key 会拒绝连接。若文件不存在，服务会创建空文件并拒绝未知主机。 |
| `insecure-ignore` | **跳过校验**。启动日志与 UI 都会显示明显警告。存在中间人风险。 |

生产建议：

```bash
ssh-keyscan -H example.com >> known_hosts
```

并设置：

```yaml
ssh:
  host_key_policy: known-hosts
  known_hosts_file: ./known_hosts
```

## Network Policy

默认保护，避免把 WebSSH 变成内网扫描 / SSRF 跳板：

- 禁止 `localhost` / `127.0.0.0/8` / `::1`
- 禁止 `0.0.0.0/8`
- 禁止 `169.254.0.0/16`（含云 metadata `169.254.169.254`）
- 默认禁止私网（`10/8`、`172.16/12`、`192.168/16`、`fc00::/7`、`fe80::/10`）
- 域名会解析，**任一**解析结果命中拒绝列表则拒绝

若需要连接内网目标：

```yaml
network_policy:
  allow_private_ranges: true
```

或环境变量：`GOWEBSSH_ALLOW_PRIVATE_RANGES=true`。

注意：即使允许私网，**loopback 与 link-local/metadata 仍默认拒绝**。如需例外，可在配置中使用 `allow` 前缀列表（allow 优先于 deny）。

## 私钥默认不会被保存

- 连接时经 WebSocket 发到服务端内存
- 解析 signer 后尽快清理 `[]byte` 引用（Go 无法保证字符串立即清零）
- 不写盘、不入库、不记日志、不进 localStorage
- 连接页有明确安全提示

## 加密保存凭据

启用 `credentials.enabled` 后，可以在连接页保存连接信息、私钥和 passphrase。保存是显式操作，未点击保存的私钥仍只存在于页面和连接内存中。

- 登录密码通过 Argon2id 和数据库随机 salt 派生 256 位 Vault Key。
- 整条凭据使用 AES-256-GCM 加密后写入 bbolt 数据库。
- 普通 Session 的 Vault Key 只保存在服务端内存中；选择保持登录 30 天时，Vault Key 随会话数据加密封装在 HttpOnly Cookie 中，因此服务重启后仍能解密已保存凭据。
- 数据库权限固定为 `0600`，数据库中不保存明文主机、用户名、私钥或 passphrase。
- 修改 Web 登录密码后，旧密码加密的凭据无法自动解密；修改前请先备份或重新保存凭据。

## 多连接、断线恢复与服务器监控

- 点击终端标签栏的 `＋` 可返回连接页创建新连接，已有标签保持在线。
- 0.3 起 `＋` 会打开连接设置窗口；主页可直接添加、编辑、删除或连接加密凭据。
- 每个标签代表一台服务器，可包含最多 4 个独立 SSH 窗格；左侧服务器监控数据在同标签窗格间共享。
- WebSocket 意外断开时采用指数退避自动重连，最多重试 5 次，间隔约为 1、2、4、8、15 秒；连接成功后重置计数。
- 勾选 tmux 恢复后，服务使用 `tmux new-session -A`；目标机需要安装 tmux。未启用 tmux 时重连会创建新 shell，无法恢复未保存的进程状态。
- Linux 目标每 3 秒读取 `/proc/stat`、`/proc/meminfo`、`/proc/net/dev`、loadavg 和 uptime。非 Linux 或权限受限目标的监控栏显示不可用，但不影响终端。

## HTTPS 反代示例

后端监听本机回环，由反代提供 TLS。

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

生产建议开启：

```yaml
server:
  secure_cookie: true
```

## API 摘要

- `GET /` 前端
- `GET /api/healthz`
- `POST /api/login` JSON `{username,password,remember}`
- `POST /api/logout`
- `GET /api/session`
- `GET /api/config/ui`
- `GET /api/ws/ssh` WebSocket（需登录；首条消息 `connect`）
- `GET /api/credentials` 加密凭据摘要列表
- `POST /api/credentials` 新增或更新加密凭据
- `GET /api/credentials/{id}` 解密单条凭据
- `DELETE /api/credentials/{id}` 删除凭据

## 构建与测试

```bash
go mod tidy
go test ./...
go build -o go-webssh ./cmd/webssh
```

## Tag 自动构建

推送以 `v` 开头的 Tag 会触发 GitHub Actions，例如：

```bash
git tag v0.5.4
git push origin v0.5.4
```

工作流会先执行测试和 `go vet`，然后交叉编译以下 Linux 平台，并自动创建或更新对应的 GitHub Release：

- Linux amd64 / arm64

Release 同时包含每个平台的压缩包和 `SHA256SUMS` 校验文件。构建时会把 Tag 名写入 `go-webssh --version`。

## 常见问题

**Q: 连接失败提示 host key？**

A: 默认 `known-hosts`。用 `ssh-keyscan` 写入 known_hosts，或在明确风险下改用 `insecure-ignore`。

**Q: 连不上内网 IP？**

A: 默认 `allow_private_ranges=false`。需要时显式打开。

**Q: 连不上 127.0.0.1？**

A: 默认禁止 loopback，防止 SSRF。不要为图方便默认放开；如确有需要，使用 `network_policy.allow` 显式放行并清楚风险。

**Q: 私钥安全吗？**

A: 会经过服务端内存。请使用 HTTPS、强 Web 登录密码、受信服务器与专用 key。

**Q: 为什么不用每个目标一个 ttyd？**

A: 本项目单进程多会话，浏览器提交目标与私钥即可连不同主机。

## License

当前仓库未授予开源许可证。未经仓库所有者许可，不得复制、分发或用于衍生项目。
