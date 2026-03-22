# wx-mcp — 微信 Bot MCP Server

Go 实现的 WeChat iLink Bot MCP Server，支持 **stdio** 和 **SSE** 双传输协议。

## 安装

```bash
cd wx-mcp
go build -o wx-mcp .
```

---

## 传输协议

### stdio（默认）

用于 Claude Desktop、Cursor、Continue 等本地 MCP 客户端。

```bash
./wx-mcp                        # 默认 stdio
./wx-mcp -transport stdio       # 显式指定
```

### SSE（HTTP）

用于远程访问或 Web 客户端。

```bash
./wx-mcp -transport sse -addr :8081
```

| 端点 | 说明 |
|------|------|
| `GET /sse` | SSE 事件流（客户端长连接） |
| `POST /message?sessionId=<id>` | 发送 JSON-RPC 请求 |
| `GET /health` | 健康检查 |

---

## Claude Desktop 配置

`claude_desktop_config.json`：

```json
{
  "mcpServers": {
    "wx-mcp": {
      "command": "wx-mcp",
      "args": ["-transport", "stdio"]
    }
  }
}
```

---

## Cursor 配置

`.cursor/mcp.json`（项目级）或 `~/.cursor/mcp.json`（全局）：

```json
{
  "mcpServers": {
    "wx-mcp": {
      "command": "wx-mcp",
      "args": []
    }
  }
}
```

---

## 可用工具（Tools）

| 工具名 | 描述 |
|--------|------|
| `wx_login_start` | 获取扫码登录二维码 URL |
| `wx_login_poll` | 轮询扫码状态，确认后自动注册账号并开始消息轮询 |
| `wx_list_accounts` | 列出所有已登录账号 |
| `wx_account_status` | 查看账号详细状态（是否暂停/冷却） |
| `wx_remove_account` | 移除账号并停止轮询 |
| `wx_list_conversations` | 列出账号所有会话及未读数 |
| `wx_get_messages` | 获取与某用户的聊天记录 |
| `wx_get_unread` | 获取所有会话未读汇总 |
| `wx_send_text` | 向微信用户发送文字消息 |
| `wx_send_image` | 发送图片（本地路径或 base64） |
| `wx_send_file` | 发送文件附件 |

---

## 典型使用流程

```
1. wx_login_start        → 获取 qrcode_scan_url 和 session_key
2. 用微信扫二维码
3. wx_login_poll         → 轮询直到 status=confirmed，得到 account_id
4. wx_list_conversations → 查看有哪些用户发来了消息
5. wx_get_messages       → 查看具体对话内容
6. wx_send_text          → 回复用户
```

---

## SSE 手动测试

```bash
# 1. 启动 SSE 服务
./wx-mcp -transport sse -addr :8081

# 2. 连接 SSE（新终端）
curl -N http://localhost:8081/sse
# 收到: event: endpoint
#       data: /message?sessionId=<UUID>

# 3. 发送 initialize 请求
curl -s -X POST "http://localhost:8081/message?sessionId=<UUID>" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}'

# 4. 列出工具
curl -s -X POST "http://localhost:8081/message?sessionId=<UUID>" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
```
