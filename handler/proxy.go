package handler

import (
	"bytes"
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
		if len(reqBody) > 0 {
			if err := json.Unmarshal(reqBody, &reqData); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON in request body"})
				return
			}
		}
	}

	// 获取并映射模型名
	model := ""
	mappedModel := ""
	if reqData != nil {
		if m, ok := reqData["model"].(string); ok {
			model = m
			mappedModel = h.cfg.MapModel(m)
			reqData["model"] = mappedModel
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

	// 判断上游端点：/chat/completions 走 OpenAI 兼容，其余走 Anthropic 兼容
	var apiBase, apiKey string
	if strings.Contains(path, "/chat/completions") && h.cfg.OpenAI.APIBase != "" {
		apiBase = h.cfg.OpenAI.APIBase
		apiKey = h.cfg.OpenAI.APIKey
		path = "/chat/completions"
	} else {
		apiBase = h.cfg.Zhipu.APIBase
		apiKey = h.cfg.Zhipu.APIKey
	}

	reqLog := h.logger.NewLog(requestID, model, mappedModel, isStream)
	reqLog.Request = reqData
	reqLog.Method = method

	// 构建目标 URL
	targetURL := apiBase + path
	reqLog.Path = targetURL

	// 创建请求
	var httpReq *http.Request
	var err error

	if reqBody != nil {
		httpReq, err = http.NewRequest(method, targetURL, bytes.NewReader(reqBody))
	} else {
		httpReq, err = http.NewRequest(method, targetURL, nil)
	}

	if err != nil {
		reqLog.ErrorMessage = err.Error()
		h.logger.Save(reqLog)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create request"})
		return
	}

	// 设置 headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	// 复制查询参数
	httpReq.URL.RawQuery = c.Request.URL.RawQuery

	// 发送请求
	resp, err := h.client.Do(httpReq)
	if err != nil {
		reqLog.ErrorMessage = err.Error()
		h.logger.Save(reqLog)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to reach upstream", "detail": err.Error()})
		return
	}
	defer resp.Body.Close()

	// 非 200 响应
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		reqLog.ErrorMessage = string(respBody)
		h.logger.Save(reqLog)
		c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), respBody)
		return
	}

	// 流式响应
	contentType := resp.Header.Get("Content-Type")
	if isStream || strings.Contains(contentType, "text/event-stream") {
		handleStreamResponse(c, resp, reqLog, h.logger)
		return
	}

	// 非流式响应
	handleNormalResponse(c, resp, reqLog, h.logger)
}
