package langfuse

import (
	"encoding/json"
	"log"
	"time"

	"anthropic-proxy/config"

	langfuseSDK "github.com/AEKurt/langfuse-go"
)

// Tracer 封装 Langfuse 异步客户端，提供对话级别的 trace 上报
type Tracer struct {
	client *langfuseSDK.AsyncClient
}

// NewTracer 创建 Langfuse Tracer
func NewTracer(cfg *config.LangfuseConfig) (*Tracer, error) {
	client, err := langfuseSDK.NewAsyncClient(
		langfuseSDK.Config{
			PublicKey: cfg.PublicKey,
			SecretKey: cfg.SecretKey,
			BaseURL:   cfg.BaseURL,
		},
		langfuseSDK.DefaultBatchConfig(),
	)
	if err != nil {
		return nil, err
	}
	return &Tracer{client: client}, nil
}

// TraceContext 保存一次请求的 trace 信息，用于在响应完成后更新
type TraceContext struct {
	traceID      string
	generationID string
}

// StartTrace 为一次 API 请求创建 trace 和 generation
func (t *Tracer) StartTrace(requestID, model string, reqData map[string]interface{}) *TraceContext {
	if t == nil || t.client == nil {
		return nil
	}

	now := time.Now()

	// 从 request metadata 提取 session_id 用于分组
	sessionID := extractSessionID(reqData)

	// 提取 user_id
	userID := extractUserID(reqData)

	// 提取 messages 作为 input
	messages := reqData["messages"]

	// 创建 trace
	traceID, err := t.client.CreateTraceAsync(langfuseSDK.Trace{
		Name:      "api-request",
		SessionID: sessionID,
		UserID:    userID,
		Metadata: map[string]interface{}{
			"request_id": requestID,
		},
		Timestamp: &now,
	})
	if err != nil {
		log.Printf("[langfuse] failed to create trace: %v", err)
		return nil
	}

	// 创建 generation
	genID, err := t.client.CreateGenerationAsync(langfuseSDK.Generation{
		TraceID:   traceID,
		Name:      model,
		Model:     model,
		StartTime: &now,
		Input:     messages,
	})
	if err != nil {
		log.Printf("[langfuse] failed to create generation: %v", err)
		return nil
	}

	return &TraceContext{
		traceID:      traceID,
		generationID: genID,
	}
}

// FinishGenerationSuccess 流式响应完成后，更新 generation 的 output 和 usage
func (t *Tracer) FinishGenerationSuccess(ctx *TraceContext, output interface{}, inputTokens, outputTokens int) {
	if t == nil || t.client == nil || ctx == nil {
		return
	}

	now := time.Now()
	usage := &langfuseSDK.Usage{
		Input:  inputTokens,
		Output: outputTokens,
		Total:  inputTokens + outputTokens,
		Unit:   "TOKENS",
	}

	err := t.client.UpdateGenerationAsync(ctx.generationID, langfuseSDK.GenerationUpdate{
		EndTime: &now,
		Output:  output,
		Usage:   usage,
	})
	if err != nil {
		log.Printf("[langfuse] failed to update generation: %v", err)
	}
}

// FinishGenerationError 请求失败时更新 generation 的错误信息
func (t *Tracer) FinishGenerationError(ctx *TraceContext, errMsg string) {
	if t == nil || t.client == nil || ctx == nil {
		return
	}

	now := time.Now()
	statusMsg := errMsg
	if len(statusMsg) > 500 {
		statusMsg = statusMsg[:500]
	}

	err := t.client.UpdateGenerationAsync(ctx.generationID, langfuseSDK.GenerationUpdate{
		EndTime:       &now,
		StatusMessage: &statusMsg,
	})
	if err != nil {
		log.Printf("[langfuse] failed to update generation error: %v", err)
	}
}

// Flush 刷新待发送的事件
func (t *Tracer) Flush() {
	if t == nil || t.client == nil {
		return
	}
	if err := t.client.Flush(); err != nil {
		log.Printf("[langfuse] flush error: %v", err)
	}
}

// Shutdown 关闭客户端，确保所有事件发送完成
func (t *Tracer) Shutdown() {
	if t == nil || t.client == nil {
		return
	}
	if err := t.client.Shutdown(); err != nil {
		log.Printf("[langfuse] shutdown error: %v", err)
	}
}

// AssembleStreamContent 从 stream chunks 拼接完整的响应内容
// 返回适合 Langfuse 显示的格式
func AssembleStreamContent(chunks []interface{}) interface{} {
	if len(chunks) == 0 {
		return nil
	}

	type ContentBlock struct {
		Type     string `json:"type"`
		Thinking string `json:"thinking,omitempty"`
		Text     string `json:"text,omitempty"`
	}

	// 按原始顺序收集所有完成的块
	var orderedBlocks []ContentBlock
	var currentBlock *ContentBlock

	for _, raw := range chunks {
		chunk, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		chunkType, _ := chunk["type"].(string)

		switch chunkType {
		case "content_block_start":
			cb, _ := chunk["content_block"].(map[string]interface{})
			cbType, _ := cb["type"].(string)
			if cbType == "thinking" {
				currentBlock = &ContentBlock{Type: "thinking"}
			} else if cbType == "text" {
				currentBlock = &ContentBlock{Type: "text"}
			}

		case "content_block_delta":
			if currentBlock == nil {
				continue
			}
			delta, _ := chunk["delta"].(map[string]interface{})
			if delta == nil {
				continue
			}
			deltaType, _ := delta["type"].(string)
			if deltaType == "thinking_delta" {
				if text, ok := delta["thinking"].(string); ok {
					currentBlock.Thinking += text
				}
			} else if deltaType == "text_delta" {
				if text, ok := delta["text"].(string); ok {
					currentBlock.Text += text
				}
			}

		case "content_block_stop":
			if currentBlock != nil {
				orderedBlocks = append(orderedBlocks, *currentBlock)
				currentBlock = nil
			}
		}
	}

	// 保存最后未关闭的块
	if currentBlock != nil {
		orderedBlocks = append(orderedBlocks, *currentBlock)
	}

	if len(orderedBlocks) == 0 {
		return nil
	}

	return orderedBlocks
}

// AssembleStreamContentFromRaw 从原始 SSE data 字符串拼接内容
func AssembleStreamContentFromRaw(dataLines []string) interface{} {
	var chunks []interface{}
	for _, line := range dataLines {
		var chunk map[string]interface{}
		if json.Unmarshal([]byte(line), &chunk) == nil {
			chunks = append(chunks, chunk)
		}
	}
	return AssembleStreamContent(chunks)
}

// extractSessionID 从请求 metadata 中提取 session_id
func extractSessionID(reqData map[string]interface{}) string {
	if reqData == nil {
		return ""
	}
	meta, _ := reqData["metadata"].(map[string]interface{})
	if meta != nil {
		if uid, ok := meta["user_id"].(string); ok && uid != "" {
			// 解析 user_id 中的 JSON，提取 session_id
			var data map[string]interface{}
			if json.Unmarshal([]byte(uid), &data) == nil {
				if sid, ok := data["session_id"].(string); ok {
					return sid
				}
			}
			return uid
		}
	}
	return ""
}

// extractUserID 从请求 metadata 中提取 user_id
func extractUserID(reqData map[string]interface{}) string {
	if reqData == nil {
		return ""
	}
	meta, _ := reqData["metadata"].(map[string]interface{})
	if meta != nil {
		if uid, ok := meta["user_id"].(string); ok && uid != "" {
			var data map[string]interface{}
			if json.Unmarshal([]byte(uid), &data) == nil {
				if aid, ok := data["account_uuid"].(string); ok && aid != "" {
					return aid
				}
			}
			return uid
		}
	}
	return ""
}
