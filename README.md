# Claude Code → 智谱 AI 代理服务

将 Claude Code 的请求转发到智谱 AI（智谱已兼容 Anthropic 格式），让 Claude Code 可以使用智谱的模型。

## 快速开始

### 1. 构建

```bash
go build -o proxy.exe .
```

### 2. 配置

创建 `config.yaml` 文件：

```yaml
server:
  host: ""
  port: 8080

zhipu:
  api_base: "https://open.bigmodel.cn/api/anthropic"
  api_key: "${ZHIPU_API_KEY}"

model_mapping:
  "claude-3-5-sonnet": "glm-4-plus"
  "claude-3-haiku": "glm-4-flash"

logging:
  enabled: true
  dir: "./logs"
  file_prefix: "requests_"
  console: true
```

### 3. 运行

```bash
# 设置智谱 API Key
set ZHIPU_API_KEY=your_zhipu_api_key

# 启动代理
proxy.exe

# 或直接运行
go run .
```

### 4. 配置 Claude Code

```bash
set ANTHROPIC_BASE_URL=http://localhost:8080
set ANTHROPIC_API_KEY=any_key
```

现在可以正常使用 Claude Code，请求会自动转发到智谱 AI。

## 功能特性

- **透明代理**：转发所有 Anthropic API 请求到智谱兼容端点
- **流式响应**：完整支持 SSE 流式输出
- **请求日志**：JSONL 格式记录所有请求/响应
- **环境变量**：配置文件支持 `${VAR}` 语法引用环境变量

## 配置说明

| 字段 | 说明 | 默认值 |
|------|------|--------|
| `server.host` | 监听地址，空字符串表示所有网卡 | `""` |
| `server.port` | 监听端口 | `8080` |
| `zhipu.api_base` | 智谱 API 地址 | 智谱 Anthropic 兼容端点 |
| `zhipu.api_key` | API Key，支持环境变量 | - |
| `logging.enabled` | 是否启用日志 | `true` |
| `logging.dir` | 日志目录 | `./logs` |
| `logging.console` | 是否输出到控制台 | `true` |

环境变量：
- `ZHIPU_API_KEY`：智谱 API Key（必需）
- `CONFIG_PATH`：配置文件路径（可选，默认 `config.yaml`）

## 日志格式

日志保存在 `./logs/requests_YYYY-MM-DD.jsonl`：

```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "request_id": "abc12345",
  "model": "claude-3-5-sonnet",
  "is_stream": true,
  "duration_ms": 1500,
  "token_usage": {
    "input_tokens": 100,
    "output_tokens": 200
  }
}
```

## 项目结构

```
.
├── main.go           # 入口，启动 Gin 服务器
├── config/config.go  # 配置加载，环境变量展开
├── handler/proxy.go  # 代理处理器，流式转发
├── logger/logger.go  # JSONL 日志记录
└── config.yaml       # 配置文件
```
