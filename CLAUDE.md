# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

This is a Go-based HTTP proxy server that forwards Anthropic/Claude API requests to Zhipu AI's Anthropic-compatible endpoint (`https://open.bigmodel.cn/api/anthropic`). It allows Claude Code to use Zhipu's models as a drop-in replacement.

## Build and Run

```bash
# Build
go build -o proxy.exe .

# Run (requires ZHIPU_API_KEY environment variable)
set ZHIPU_API_KEY=your_api_key
proxy.exe

# Or directly with go run
go run .
```

## Architecture

**Entry Point (`main.go`)**
- Loads config from `config.yaml` (or `CONFIG_PATH` env var)
- Initializes logger and proxy handler
- Starts Gin server on configured host:port

**Config Module (`config/`)**
- Loads YAML configuration with environment variable expansion (`${VAR}` syntax)
- Server settings (host, port), Zhipu API endpoint/credentials, model mapping, logging config
- Note: `MapModel()` method exists but model mapping is currently handled by Zhipu's endpoint directly

**Handler (`handler/proxy.go`)**
- `HandleAll()`: Main proxy handler for all incoming requests
- Forwards requests to Zhipu API with Bearer token auth
- Handles both streaming (`text/event-stream`) and non-streaming responses
- Extracts token usage from response (`input_tokens`, `output_tokens`)

**Logger (`logger/`)**
- Writes JSONL logs to `./logs/requests_YYYY-MM-DD.jsonl`
- Each log entry: timestamp, request_id, model, request/response data, token usage, duration
- Thread-safe with mutex locking

## Configuration

`config.yaml` controls server port, Zhipu API base URL, API key (via env var), and logging. The proxy forwards all paths verbatim to the configured `api_base`.

## Key Implementation Details

- **Streaming**: Uses `bufio.Reader` to read SSE chunks line-by-line, forwards immediately with `http.Flusher`
- **Token Usage**: Extracted from response `usage` field or message delta chunks
- **Error Handling**: Upstream errors (400+) are logged and forwarded to client with original body
- **Request ID**: Generated per request using UUID (first 8 chars) for log correlation
