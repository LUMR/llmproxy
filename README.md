# Claude Code → 智谱 代理服务

将 Claude Code 的请求转发到智谱 AI（智谱已兼容 Anthropic 格式）。

## 快速开始

```bash
# 1. 设置智谱 API Key
set ZHIPU_API_KEY=your_zhipu_api_key

# 2. 启动代理
proxy.exe

# 3. 配置 Claude Code
set ANTHROPIC_BASE_URL=http://localhost:8080
set ANTHROPIC_API_KEY=any_key
```

## 功能

- 模型名自动映射（Claude → 智谱）
- 流式响应透传
- 请求/响应 JSONL 日志

## 配置 (config.yaml)

```yaml
server:
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
```

## 日志格式

日志保存在 `./logs/requests_YYYY-MM-DD.jsonl`：

```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "request_id": "abc12345",
  "model": "claude-3-5-sonnet",
  "mapped_model": "glm-4-plus",
  "is_stream": true,
  "duration_ms": 1500,
  "token_usage": {"prompt_tokens": 100, "completion_tokens": 200, "total_tokens": 300}
}
```
