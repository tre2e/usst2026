# 一网畅学签到监听服务

## 概述

一个后台监听服务，自动登录上海理工大学一网畅学平台（1906.usst.edu.cn），定时轮询签到接口，发现新签到时通过 SMTP 邮件即时通知。

## 背景

一网畅学由统一身份认证（CAS）保护，session 有时效限制。学生需要持续关注平台才能及时获知签到，本服务将这一过程自动化：自动维护登录状态，7×24 监听签到并推送邮件。

## 架构

```
┌──────────┐     ┌──────────┐     ┌──────────┐
│ CAS 登录  │ ──→ │  轮询器   │ ──→ │ SMTP 通知 │
│ (30min刷新)│     │ (可配间隔) │     │ (TLS/STARTTLS)│
└──────────┘     └────┬─────┘     └──────────┘
                      │
                 ┌────▼────┐
                 │ 去重存储  │
                 │ (JSON文件)│
                 └─────────┘
```

单 Go 二进制，无外部依赖（仅标准库扩展 `x/net/html`），一个进程完成认证、监听、通知全部工作。

## 技术方案

### 认证流程

1. GET `https://1906.usst.edu.cn/user/index` → 302 重定向到 CAS 登录页
2. 解析 CAS 表单，提取 `lt`、`execution` 等动态隐藏字段
3. POST 凭据到 CAS → 302 重定向回一网畅学
4. 从 Cookie Jar 提取 `session` cookie

Session 每 30 分钟自动刷新。刷新失败时保留旧 session 继续工作，避免短暂网络波动导致服务中断。

### 签到监听

- **接口**: `GET /api/radar/rollcalls?api_version=1.1.0`
- **轮询间隔**: 可配置，默认 30 秒
- **响应解析**: 兼容多种 JSON 结构（直接数组、`{data: [...]}` 包裹、任意嵌套）

### 去重机制

- 内存 `map[string]int64` 保存已通知的签到 ID
- 每次通知后写入 `data/seen.json` 持久化
- 启动时清理 7 天前的记录，防止文件膨胀

### 邮件通知

- 支持 TLS（465 端口）和 STARTTLS（587 端口）
- 主题格式: `【签到通知】{课程名} — {状态}`
- 正文包含课程名、教师、状态、详情

## 项目结构

```
goforsigning/
├── main.go          # 入口：配置加载、.env解析、信号处理、主循环
├── config.go        # 环境变量读取与校验
├── login.go         # CAS 认证：重定向链跟踪、表单解析、cookie提取
├── monitor.go       # 轮询器：HTTP请求、JSON弹性解析、新签到检测
├── notify.go        # 邮件发送：SMTP TLS/STARTTLS 双模式
├── store.go         # 去重存储：内存map + JSON文件持久化
├── data/            # 运行时数据（seen.json）
├── .env.example     # 配置模板
└── go.mod
```

## 配置

通过 `.env` 文件或环境变量配置：

| 变量 | 说明 | 必填 | 默认值 |
|---|---|---|---|
| `USST_USERNAME` | 统一身份认证用户名（学号） | ✓ | — |
| `USST_PASSWORD` | 统一身份认证密码 | ✓ | — |
| `SMTP_HOST` | SMTP 服务器地址 | ✗ | smtp.qq.com |
| `SMTP_PORT` | SMTP 端口（465/587） | ✗ | 587 |
| `SMTP_USER` | SMTP 发件邮箱 | ✓ | — |
| `SMTP_PASS` | SMTP 授权码 | ✓ | — |
| `NOTIFY_EMAIL` | 接收通知的邮箱 | ✓ | — |
| `POLL_INTERVAL` | 轮询间隔（秒） | ✗ | 30 |

## 部署

### 本地运行

```bash
cp .env.example .env
# 编辑 .env 填入实际值
./goforsigning
```

### systemd 服务

```ini
[Unit]
Description=一网畅学签到监听
After=network.target

[Service]
Type=simple
WorkingDirectory=/opt/goforsigning
EnvironmentFile=/opt/goforsigning/.env
ExecStart=/opt/goforsigning/goforsigning
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

### 编译

```bash
go build -ldflags="-s -w" -o goforsigning .
```

## 容错设计

- **网络抖动**: 单次轮询失败仅记录日志，不中断服务
- **Session 过期**: 检测到 401/302 时打日志，等待下次定时刷新
- **SMTP 故障**: 邮件发送失败仅记录日志，不影响轮询继续
- **优雅退出**: 捕获 SIGINT/SIGTERM，立即退出（无脏状态）
- **启动即测**: 启动时发送测试邮件，提前发现 SMTP 配置问题
