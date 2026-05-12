package handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"litellm-proxy/logger"

	"github.com/gin-gonic/gin"
)

// streamAssembler 在流式转发过程中逐步拼接完整响应
type streamAssembler struct {
	textBuilder   strings.Builder
	messageID     string
	role          string
	model         string
	stopReason    string
	inputTokens   int
	outputTokens  int
	// OpenAI 格式字段
	isOpenAI        bool
	oaiFinishReason string
	// Anthropic 工具调用
	contentBlocks []map[string]interface{}
}

// handleNormalResponse 处理非流式响应
func handleNormalResponse(c *gin.Context, resp *http.Response, reqLog *logger.RequestLog, log *logger.Logger) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		reqLog.ErrorMessage = err.Error()
		log.Save(reqLog)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response"})
		return
	}

	var respData map[string]interface{}
	if err := json.Unmarshal(body, &respData); err != nil {
		// 上游返回了非 JSON，直接透传
		contentType := resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/json"
		}
		reqLog.ErrorMessage = "response is not valid JSON"
		log.Save(reqLog)
		c.Data(resp.StatusCode, contentType, body)
		return
	}

	// 记录日志
	reqLog.Response = respData
	if usage, ok := respData["usage"].(map[string]interface{}); ok {
		reqLog.SetTokenUsage(
			int(getFloat(usage, "input_tokens")),
			int(getFloat(usage, "output_tokens")),
			int(getFloat(usage, "input_tokens"))+int(getFloat(usage, "output_tokens")),
		)
	}
	reqLog.Success = true
	log.Save(reqLog)

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(resp.StatusCode, contentType, body)
}

// handleStreamResponse 处理流式响应
func handleStreamResponse(c *gin.Context, resp *http.Response, reqLog *logger.RequestLog, log *logger.Logger) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	reader := bufio.NewReader(resp.Body)
	flusher, _ := c.Writer.(http.Flusher)

	asm := &streamAssembler{}

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				reqLog.ErrorMessage = fmt.Sprintf("stream read error: %v", err)
			}
			break
		}

		// 直接转发原始行
		c.Writer.WriteString(line)
		flusher.Flush()

		// 解析 data 行提取内容
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "data: ") {
			data := strings.TrimPrefix(trimmed, "data: ")
			if data != "[DONE]" {
				var chunk map[string]interface{}
				if err := json.Unmarshal([]byte(data), &chunk); err == nil {
					asm.processChunk(chunk)
				}
			}
		}
	}

	reqLog.Response = asm.assemble()
	if asm.inputTokens > 0 || asm.outputTokens > 0 {
		reqLog.SetTokenUsage(asm.inputTokens, asm.outputTokens, asm.inputTokens+asm.outputTokens)
	}
	reqLog.Success = true
	log.Save(reqLog)
}

// processChunk 从 SSE chunk 中提取关键信息
func (a *streamAssembler) processChunk(chunk map[string]interface{}) {
	if a.isOpenAI {
		a.processOpenAIChunk(chunk)
		return
	}
	if _, ok := chunk["choices"]; ok {
		a.isOpenAI = true
		a.processOpenAIChunk(chunk)
		return
	}
	a.processAnthropicChunk(chunk)
}

func (a *streamAssembler) processAnthropicChunk(chunk map[string]interface{}) {
	eventType, _ := chunk["type"].(string)

	switch eventType {
	case "message_start":
		if msg, ok := chunk["message"].(map[string]interface{}); ok {
			a.messageID, _ = msg["id"].(string)
			a.role, _ = msg["role"].(string)
			a.model, _ = msg["model"].(string)
			if usage, ok := msg["usage"].(map[string]interface{}); ok {
				a.inputTokens = int(getFloat(usage, "input_tokens"))
				a.outputTokens = int(getFloat(usage, "output_tokens"))
			}
		}
	case "content_block_start":
		if cb, ok := chunk["content_block"].(map[string]interface{}); ok {
			a.contentBlocks = append(a.contentBlocks, cb)
		}
	case "content_block_delta":
		if delta, ok := chunk["delta"].(map[string]interface{}); ok {
			if text, ok := delta["text"].(string); ok {
				a.textBuilder.WriteString(text)
			}
			if partialJSON, ok := delta["partial_json"].(string); ok {
				a.textBuilder.WriteString(partialJSON)
			}
		}
	case "message_delta":
		if delta, ok := chunk["delta"].(map[string]interface{}); ok {
			a.stopReason, _ = delta["stop_reason"].(string)
		}
		if usage, ok := chunk["usage"].(map[string]interface{}); ok {
			a.outputTokens = int(getFloat(usage, "output_tokens"))
		}
	}
}

func (a *streamAssembler) processOpenAIChunk(chunk map[string]interface{}) {
	if id, ok := chunk["id"].(string); ok && id != "" {
		a.messageID = id
	}
	if model, ok := chunk["model"].(string); ok && model != "" {
		a.model = model
	}

	if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if delta, ok := choice["delta"].(map[string]interface{}); ok {
				if content, ok := delta["content"].(string); ok {
					a.textBuilder.WriteString(content)
				}
				if role, ok := delta["role"].(string); ok && role != "" {
					a.role = role
				}
			}
			if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
				a.oaiFinishReason = fr
			}
		}
	}

	if usage, ok := chunk["usage"].(map[string]interface{}); ok {
		a.inputTokens = int(getFloat(usage, "prompt_tokens"))
		a.outputTokens = int(getFloat(usage, "completion_tokens"))
	}
}

// assemble 将收集到的信息组装为完整的响应对象
func (a *streamAssembler) assemble() map[string]interface{} {
	text := a.textBuilder.String()

	if a.isOpenAI {
		return map[string]interface{}{
			"id":     a.messageID,
			"object": "chat.completion",
			"model":  a.model,
			"choices": []interface{}{
				map[string]interface{}{
					"index": 0,
					"message": map[string]interface{}{
						"role":    a.role,
						"content": text,
					},
					"finish_reason": a.oaiFinishReason,
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     a.inputTokens,
				"completion_tokens": a.outputTokens,
				"total_tokens":      a.inputTokens + a.outputTokens,
			},
		}
	}

	var content interface{}
	if len(a.contentBlocks) > 0 {
		if text != "" {
			a.contentBlocks = append([]map[string]interface{}{
				{"type": "text", "text": text},
			}, a.contentBlocks...)
		}
		content = a.contentBlocks
	} else if text != "" {
		content = []interface{}{
			map[string]interface{}{"type": "text", "text": text},
		}
	}

	return map[string]interface{}{
		"id":          a.messageID,
		"type":        "message",
		"role":        a.role,
		"content":     content,
		"model":       a.model,
		"stop_reason": a.stopReason,
		"usage": map[string]interface{}{
			"input_tokens":  a.inputTokens,
			"output_tokens": a.outputTokens,
		},
	}
}

func getFloat(m map[string]interface{}, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}
