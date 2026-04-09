package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"anthropic-proxy/config"
)

type RequestLog struct {
	Timestamp     time.Time              `json:"timestamp"`
	RequestID     string                 `json:"request_id"`
	Model         string                 `json:"model"`
	MappedModel   string                 `json:"mapped_model"`
	Request       map[string]interface{} `json:"request"`
	Response      interface{}            `json:"response,omitempty"`
	StreamChunks  []interface{}          `json:"stream_chunks,omitempty"`
	Duration      int64                  `json:"duration_ms"`
	IsStream      bool                   `json:"is_stream"`
	Success       bool                   `json:"success"`
	ErrorMessage  string                 `json:"error_message,omitempty"`
	TokenUsage    *TokenUsage            `json:"token_usage,omitempty"`
	Method        string                 `json:"method,omitempty"`
	Path          string                 `json:"path,omitempty"`
}

type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type Logger struct {
	cfg *config.LoggingConfig
	mu  sync.Mutex
}

// ANSI 颜色代码
const (
	colorReset   = "\033[0m"
	colorGray    = "\033[90m"
	colorGreen   = "\033[32m"
	colorRed     = "\033[31m"
	colorYellow  = "\033[33m"
	colorCyan    = "\033[36m"
	colorMagenta = "\033[35m"
	colorBold    = "\033[1m"
)

func New(cfg *config.LoggingConfig) (*Logger, error) {
	if cfg.Dir != "" {
		if err := os.MkdirAll(cfg.Dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create log directory: %w", err)
		}
	}
	return &Logger{cfg: cfg}, nil
}

func (l *Logger) NewLog(requestID, model, mappedModel string, isStream bool) *RequestLog {
	return &RequestLog{
		Timestamp:    time.Now(),
		RequestID:    requestID,
		Model:        model,
		MappedModel:  mappedModel,
		IsStream:     isStream,
		StreamChunks: make([]interface{}, 0),
	}
}

func (l *RequestLog) SetRequest(req map[string]interface{}) {
	l.Request = req
}

func (l *RequestLog) SetResponse(resp interface{}) {
	l.Response = resp
}

func (l *RequestLog) AddStreamChunk(chunk interface{}) {
	l.StreamChunks = append(l.StreamChunks, chunk)
}

func (l *RequestLog) SetError(err string) {
	l.Success = false
	l.ErrorMessage = err
}

func (l *RequestLog) SetSuccess() {
	l.Success = true
}

func (l *RequestLog) SetTokenUsage(prompt, completion, total int) {
	l.TokenUsage = &TokenUsage{
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      total,
	}
}

func (l *RequestLog) SetHTTPInfo(method, path string) {
	l.Method = method
	l.Path = path
}

func (l *RequestLog) Finalize() {
	l.Duration = time.Since(l.Timestamp).Milliseconds()
}

func (l *Logger) Save(log *RequestLog) error {
	log.Finalize()

	// 控制台输出（始终执行）
	l.printConsole(log)

	// 文件日志
	if !l.cfg.Enabled {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// 按日期分文件
	dateStr := time.Now().Format("2006-01-02")
	filename := fmt.Sprintf("%s_%s.jsonl", l.cfg.FilePrefix, dateStr)
	path := filepath.Join(l.cfg.Dir, filename)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(log)
	if err != nil {
		return fmt.Errorf("failed to marshal log: %w", err)
	}

	_, err = f.WriteString(string(data) + "\n")
	return err
}

// printConsole 输出美化的控制台日志
func (l *Logger) printConsole(log *RequestLog) {
	if !l.cfg.Console {
		return
	}

	// 时间和请求ID
	timeStr := log.Timestamp.Format("15:04:05")
	idStr := log.RequestID
	if len(idStr) > 6 {
		idStr = idStr[:6]
	}

	// 状态颜色
	var statusIcon, statusColor string
	if log.Success {
		statusIcon = "✓"
		statusColor = colorGreen
	} else {
		statusIcon = "✗"
		statusColor = colorRed
	}

	// HTTP 方法颜色
	methodColor := colorCyan
	switch log.Method {
	case "POST":
		methodColor = colorYellow
	case "GET":
		methodColor = colorGreen
	case "DELETE":
		methodColor = colorRed
	}

	// 构建输出
	var sb strings.Builder

	// 第一行：时间、请求ID、HTTP信息
	sb.WriteString(fmt.Sprintf("%s%s%s [%s%s%s] %s%s%s %s\n",
		colorGray, timeStr, colorReset,
		colorMagenta, idStr, colorReset,
		methodColor, log.Method, colorReset,
		log.Path))

	// 第二行：模型映射
	sb.WriteString(fmt.Sprintf("  Model: %s%s%s → %s%s%s\n",
		colorCyan, log.Model, colorReset,
		colorYellow, log.MappedModel, colorReset))

	// 第三行：Token 使用和耗时
	if log.TokenUsage != nil {
		sb.WriteString(fmt.Sprintf("  Tokens: %s%d%s in / %s%d%s out",
			colorGreen, log.TokenUsage.PromptTokens, colorReset,
			colorGreen, log.TokenUsage.CompletionTokens, colorReset))

		// 流式标记
		if log.IsStream {
			sb.WriteString(fmt.Sprintf(" %s[stream]%s", colorCyan, colorReset))
		}

		sb.WriteString(fmt.Sprintf(" | Duration: %s%dms%s\n",
			colorYellow, log.Duration, colorReset))
	} else {
		sb.WriteString(fmt.Sprintf("  Duration: %s%dms%s\n",
			colorYellow, log.Duration, colorReset))
	}

	// 第四行：状态
	if log.Success {
		sb.WriteString(fmt.Sprintf("  Status: %s%s Success%s\n",
			statusColor, statusIcon, colorReset))
	} else {
		sb.WriteString(fmt.Sprintf("  Status: %s%s Error%s",
			statusColor, statusIcon, colorReset))
		if log.ErrorMessage != "" {
			// 截断过长的错误信息
			errMsg := log.ErrorMessage
			if len(errMsg) > 100 {
				errMsg = errMsg[:100] + "..."
			}
			sb.WriteString(fmt.Sprintf(" - %s", errMsg))
		}
		sb.WriteString("\n")
	}

	// 分隔线
	sb.WriteString(fmt.Sprintf("%s└────────────────────────────────────────%s\n",
		colorGray, colorReset))

	fmt.Print(sb.String())
}
