package openclaw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GatewayClient 抽象 OpenClaw 网关调用。
// 规划上应由 github.com/a3tai/openclaw-go 的 client 实现；
// 当前通过兼容实现保持请求/响应协议一致，便于后续平滑替换。
type GatewayClient interface {
	SendMessage(ctx context.Context, text, deviceID string) (GatewayResponse, error)
}

type openclawGoCompatibleClient struct {
	baseURL    string
	authType   string
	token      string
	httpClient *http.Client
}

func newGatewayClient(cfg RuntimeConfig) GatewayClient {
	return &openclawGoCompatibleClient{
		baseURL:    strings.TrimSpace(cfg.BaseURL),
		authType:   strings.TrimSpace(cfg.AuthType),
		token:      strings.TrimSpace(cfg.Token),
		httpClient: &http.Client{Timeout: 8 * time.Second},
	}
}

func (c *openclawGoCompatibleClient) SendMessage(ctx context.Context, text, deviceID string) (GatewayResponse, error) {
	var out GatewayResponse
	body := map[string]interface{}{"message": text, "device_id": deviceID}
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if strings.EqualFold(c.authType, "bearer") && c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("gateway http %d", resp.StatusCode)
	}
	_ = json.Unmarshal(b, &out)
	if out.Reply == "" && !out.Pending {
		out.Reply = strings.TrimSpace(string(b))
	}
	return out, nil
}
