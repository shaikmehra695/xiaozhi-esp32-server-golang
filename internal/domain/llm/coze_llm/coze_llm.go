package coze_llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	llm_common "xiaozhi-esp32-server-golang/internal/domain/llm/common"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
	sse "github.com/tmaxmax/go-sse"
)

const (
	defaultCozeBaseURL  = "https://api.coze.com"
	defaultConnectorID  = "1024"
	defaultUserPrefix   = "xiaozhi"
	llmExtraErrorKey    = "error"
	streamCreatePath    = "/v3/chat"
	streamCreatePathV2  = "/v3/chats"
	maxIdleConns        = 200
	maxIdleConnsPerHost = 50
	idleConnTimeout     = 90 * time.Second
	dialTimeout         = 30 * time.Second
	keepAliveTimeout    = 30 * time.Second
)

var (
	httpClientOnce sync.Once
	httpClientInst *http.Client
)

type CozeLLMProvider struct {
	apiKey      string
	baseURL     string
	botID       string
	userPrefix  string
	connectorID string
	httpClient  *http.Client
}

type cozeMessage struct {
	Role        string `json:"role"`
	Type        string `json:"type"`
	Content     string `json:"content"`
	ContentType string `json:"content_type"`
}

type cozeCreateChatRequest struct {
	BotID              string         `json:"bot_id"`
	UserID             string         `json:"user_id"`
	Stream             bool           `json:"stream"`
	ConnectorID        string         `json:"connector_id,omitempty"`
	AdditionalMessages []*cozeMessage `json:"additional_messages,omitempty"`
}

type cozeStreamEvent struct {
	Event   string            `json:"event"`
	Message *cozeEventMessage `json:"message,omitempty"`
	Chat    *cozeEventChat    `json:"chat,omitempty"`
	Code    int               `json:"code,omitempty"`
	Msg     string            `json:"msg,omitempty"`
}

type cozeEventMessage struct {
	Content string `json:"content"`
}

type cozeEventChat struct {
	LastError *cozeLastError `json:"last_error,omitempty"`
}

type cozeLastError struct {
	Code int    `json:"code,omitempty"`
	Msg  string `json:"msg,omitempty"`
}

func getHTTPClient() *http.Client {
	httpClientOnce.Do(func() {
		transport := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   dialTimeout,
				KeepAlive: keepAliveTimeout,
			}).DialContext,
			MaxIdleConns:        maxIdleConns,
			MaxIdleConnsPerHost: maxIdleConnsPerHost,
			IdleConnTimeout:     idleConnTimeout,
			DisableKeepAlives:   false,
		}
		httpClientInst = &http.Client{
			Transport: transport,
			Timeout:   0,
		}
	})
	return httpClientInst
}

func NewCozeLLMProvider(config map[string]interface{}) (*CozeLLMProvider, error) {
	apiKey, _ := config["api_key"].(string)
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("coze api_key不能为空")
	}

	botID, _ := config["bot_id"].(string)
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return nil, fmt.Errorf("coze bot_id不能为空")
	}

	baseURL, _ := config["base_url"].(string)
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultCozeBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	userPrefix, _ := config["user_prefix"].(string)
	userPrefix = strings.TrimSpace(userPrefix)
	if userPrefix == "" {
		userPrefix = defaultUserPrefix
	}

	connectorID, _ := config["connector_id"].(string)
	connectorID = strings.TrimSpace(connectorID)
	if connectorID == "" {
		connectorID = defaultConnectorID
	}

	return &CozeLLMProvider{
		apiKey:      apiKey,
		baseURL:     baseURL,
		botID:       botID,
		userPrefix:  userPrefix,
		connectorID: connectorID,
		httpClient:  getHTTPClient(),
	}, nil
}

func (p *CozeLLMProvider) ResponseWithContext(ctx context.Context, sessionID string, dialogue []*schema.Message, _ []*schema.ToolInfo) chan *schema.Message {
	out := make(chan *schema.Message, 200)

	go func() {
		defer close(out)

		query := llm_common.BuildPromptFromDialogue(dialogue)
		if strings.TrimSpace(query) == "" {
			sendLLMError(out, fmt.Errorf("coze query不能为空"))
			return
		}

		reqBody := cozeCreateChatRequest{
			BotID:       p.botID,
			UserID:      llm_common.BuildStableUserID(p.userPrefix, sessionID),
			Stream:      true,
			ConnectorID: p.connectorID,
			AdditionalMessages: []*cozeMessage{
				{
					Role:        "user",
					Type:        "question",
					Content:     query,
					ContentType: "text",
				},
			},
		}

		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			sendLLMError(out, err)
			return
		}

		resp, err := p.openStreamRequest(ctx, bodyBytes)
		if err != nil {
			sendLLMError(out, err)
			return
		}
		defer resp.Body.Close()

		seenDelta := false
		for event, eventErr := range sse.Read(resp.Body, nil) {
			if eventErr != nil {
				if ctx.Err() != nil {
					return
				}
				sendLLMError(out, fmt.Errorf("coze流读取失败: %w", eventErr))
				return
			}

			data := strings.TrimSpace(event.Data)
			if data == "" {
				continue
			}
			if data == "[DONE]" {
				return
			}

			var streamEvent cozeStreamEvent
			if err := json.Unmarshal([]byte(data), &streamEvent); err != nil {
				log.Warnf("解析coze流事件失败: %v, data=%s", err, previewString(data, 256))
				continue
			}

			switch streamEvent.Event {
			case "conversation.message.delta":
				if streamEvent.Message != nil && streamEvent.Message.Content != "" {
					seenDelta = true
					out <- &schema.Message{
						Role:    schema.Assistant,
						Content: streamEvent.Message.Content,
					}
				}
			case "conversation.message.completed":
				// Some models may only emit completed messages without deltas.
				if !seenDelta && streamEvent.Message != nil && streamEvent.Message.Content != "" {
					out <- &schema.Message{
						Role:    schema.Assistant,
						Content: streamEvent.Message.Content,
					}
				}
			case "conversation.chat.completed", "done":
				return
			case "conversation.chat.failed", "error":
				sendLLMError(out, errors.New(extractCozeError(streamEvent)))
				return
			}
		}
	}()

	return out
}

func (p *CozeLLMProvider) openStreamRequest(ctx context.Context, bodyBytes []byte) (*http.Response, error) {
	paths := []string{streamCreatePath, streamCreatePathV2}
	var lastErr error

	for i, path := range paths {
		url := p.baseURL + path
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")

		resp, err := p.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("coze请求失败: %w", err)
		}

		if resp.StatusCode == http.StatusNotFound && i < len(paths)-1 {
			resp.Body.Close()
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			resp.Body.Close()
			lastErr = fmt.Errorf("coze请求失败 status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(errBody)))
			continue
		}

		return resp, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("coze请求失败")
}

func extractCozeError(event cozeStreamEvent) string {
	if event.Chat != nil && event.Chat.LastError != nil && event.Chat.LastError.Msg != "" {
		return event.Chat.LastError.Msg
	}
	if event.Msg != "" {
		return event.Msg
	}
	return "coze返回错误"
}

func (p *CozeLLMProvider) ResponseWithVllm(_ context.Context, _ []byte, _ string, _ string) (string, error) {
	return "", fmt.Errorf("coze provider不支持vllm能力")
}

func (p *CozeLLMProvider) GetModelInfo() map[string]interface{} {
	return map[string]interface{}{
		"type":         "coze",
		"provider":     "coze",
		"base_url":     p.baseURL,
		"bot_id":       p.botID,
		"user_prefix":  p.userPrefix,
		"connector_id": p.connectorID,
	}
}

func (p *CozeLLMProvider) Close() error {
	return nil
}

func (p *CozeLLMProvider) IsValid() bool {
	return p != nil && p.apiKey != "" && p.botID != ""
}

func sendLLMError(ch chan *schema.Message, err error) {
	ch <- &schema.Message{
		Role:  schema.System,
		Extra: map[string]any{llmExtraErrorKey: err.Error()},
	}
}

func previewString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
