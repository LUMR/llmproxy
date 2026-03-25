package handler

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"litellm-proxy/config"
	"litellm-proxy/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ProxyHandler struct {
	cfg    *config.Config
	logger *logger.Logger
	client *http.Client
}

func NewProxyHandler(cfg *config.Config, log *logger.Logger) *ProxyHandler {
	return &ProxyHandler{
		cfg:    cfg,
		logger: log,
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// HandleAll 处理所有 /v1/* 请求
func (h *ProxyHandler) HandleAll(c *gin.Context) {
	requestID := uuid.New().String()[:8]
	path := c.Param("path")
	method := c.Request.Method

	// 读取请求体
	var reqBody []byte
	var reqData map[string]interface{}

	if method == "POST" || method == "PUT" || method == "PATCH" {
		var err error
		reqBody, err = io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
			return
		}
		json.Unmarshal(reqBody, &reqData)
	}

	// 获取并映射模型名
	model := ""
	if reqData != nil {
		if m, ok := reqData["model"].(string); ok {
			model = m
			reqBody, _ = json.Marshal(reqData)
		}
	}

	// 判断是否流式
	isStream := false
	if reqData != nil {
		if s, ok := reqData["stream"].(bool); ok {
			isStream = s
		}
	}

	// 创建日志
	reqLog := h.logger.NewLog(requestID, model, model, isStream)
	reqLog.SetRequest(reqData)
	reqLog.SetHTTPInfo(method, path)

	// 构建目标 URL
	targetURL := h.cfg.Zhipu.APIBase + "/" + path

	// 创建请求
	var httpReq *http.Request
	var err error

	if reqBody != nil {
		httpReq, err = http.NewRequest(method, targetURL, strings.NewReader(string(reqBody)))
	} else {
		httpReq, err = http.NewRequest(method, targetURL, nil)
	}

	if err != nil {
		reqLog.SetError(err.Error())
		h.logger.Save(reqLog)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create request"})
		return
	}

	// 设置 headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+h.cfg.Zhipu.APIKey)

	// 复制查询参数
	httpReq.URL.RawQuery = c.Request.URL.RawQuery

	// 发送请求
	resp, err := h.client.Do(httpReq)
	if err != nil {
		reqLog.SetError(err.Error())
		h.logger.Save(reqLog)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to reach upstream", "detail": err.Error()})
		return
	}
	defer resp.Body.Close()

	// 非 200 响应
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		reqLog.SetError(string(respBody))
		h.logger.Save(reqLog)
		c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), respBody)
		return
	}

	// 流式响应
	contentType := resp.Header.Get("Content-Type")
	if isStream || strings.Contains(contentType, "text/event-stream") {
		h.handleStreamResponse(c, resp, reqLog)
		return
	}

	// 非流式响应
	h.handleNormalResponse(c, resp, reqLog)
}

func (h *ProxyHandler) handleNormalResponse(c *gin.Context, resp *http.Response, reqLog *logger.RequestLog) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		reqLog.SetError(err.Error())
		h.logger.Save(reqLog)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response"})
		return
	}

	var respData map[string]interface{}
	json.Unmarshal(body, &respData)

	// 记录日志
	reqLog.SetResponse(respData)
	if respData != nil {
		if usage, ok := respData["usage"].(map[string]interface{}); ok {
			reqLog.SetTokenUsage(
				int(getFloat(usage, "input_tokens")),
				int(getFloat(usage, "output_tokens")),
				int(getFloat(usage, "input_tokens"))+int(getFloat(usage, "output_tokens")),
			)
		}
	}
	reqLog.SetSuccess()
	h.logger.Save(reqLog)

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(http.StatusOK, contentType, body)
}

func (h *ProxyHandler) handleStreamResponse(c *gin.Context, resp *http.Response, reqLog *logger.RequestLog) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	reader := bufio.NewReader(resp.Body)
	flusher, _ := c.Writer.(http.Flusher)

	var inputTokens, outputTokens int

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			break
		}

		// 直接转发
		c.Writer.WriteString(line)
		flusher.Flush()

		// 记录 chunk 并提取 usage
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data != "[DONE]" {
				var chunk map[string]interface{}
				if json.Unmarshal([]byte(data), &chunk) == nil {
					reqLog.AddStreamChunk(chunk)
					// 提取 token 使用量
					extractTokens(chunk, &inputTokens, &outputTokens)
				}
			}
		}
	}

	if inputTokens > 0 || outputTokens > 0 {
		reqLog.SetTokenUsage(inputTokens, outputTokens, inputTokens+outputTokens)
	}
	reqLog.SetSuccess()
	h.logger.Save(reqLog)
}

func extractTokens(chunk map[string]interface{}, inputTokens, outputTokens *int) {
	// message 响应中的 usage
	if usage, ok := chunk["usage"].(map[string]interface{}); ok {
		*inputTokens = int(getFloat(usage, "input_tokens"))
		*outputTokens = int(getFloat(usage, "output_tokens"))
	}
	// message_delta 中的 usage
	if msg, ok := chunk["message"].(map[string]interface{}); ok {
		if usage, ok := msg["usage"].(map[string]interface{}); ok {
			*inputTokens = int(getFloat(usage, "input_tokens"))
			*outputTokens = int(getFloat(usage, "output_tokens"))
		}
	}
}

func getFloat(m map[string]interface{}, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}
