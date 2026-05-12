# 流式响应拼接为完整 JSON 日志

## 目标

将流式 SSE 响应的零散 chunk 拼接成一个完整的响应 JSON 对象写入日志，与非流式响应的日志格式保持一致。丢弃原始 StreamChunks 数据，使日志更简洁易读。

## 方案

在 `handleStreamResponse` 的 SSE 读取循环中，边转发边提取关键信息，循环结束后组装完整响应对象。

## 文件改动

### 1. `handler/stream.go` — handleStreamResponse

- 新增 `strings.Builder` 累积文本内容
- 新增变量收集 message_id、role、model、stop_reason
- 根据 chunk 结构判断 Anthropic 或 OpenAI 格式，分别提取：
  - Anthropic：`message_start` → id/role/model/usage(input)；`content_block_delta` → delta.text；`message_delta` → stop_reason/usage(output)
  - OpenAI：`choices[0].delta.content` → 文本；`id`/`model` 从 chunk 提取
- 循环结束后组装完整 response map 存入 `reqLog.Response`
- 移除 `reqLog.AddStreamChunk(chunk)` 调用

### 2. `logger/logger.go` — RequestLog

- 移除 `StreamChunks []interface{}` 字段
- 移除 `NewLog` 中初始化 StreamChunks 的代码
- 移除 `AddStreamChunk` 方法

## 拼接后的日志格式

```json
{
  "timestamp": "2026-05-12T10:00:00Z",
  "request_id": "abc12345",
  "model": "claude-sonnet-4-20250514",
  "mapped_model": "claude-sonnet-4-20250514",
  "request": { "model": "claude-sonnet-4-20250514", "stream": true, ... },
  "response": {
    "id": "msg_xxx",
    "type": "message",
    "role": "assistant",
    "content": [{"type": "text", "text": "完整的回复内容"}],
    "model": "claude-sonnet-4-20250514",
    "stop_reason": "end_turn"
  },
  "duration_ms": 1234,
  "is_stream": true,
  "success": true,
  "token_usage": {"prompt_tokens": 100, "completion_tokens": 50, "total_tokens": 150}
}
```
