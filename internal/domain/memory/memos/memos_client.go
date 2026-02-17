package memos

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/schema"
)

// Client 表示 MemOS 独立 provider 客户端。
//
// 注意：具体 API 已由用户指定官方文档地址，当前环境无法直接拉取文档（网络 403），
// 因此此处仅先保留独立 provider 结构，避免继续与 mem0 混用。
type Client struct {
	config map[string]interface{}
}

func GetWithConfig(config map[string]interface{}) (*Client, error) {
	if config == nil {
		config = map[string]interface{}{}
	}
	return &Client{config: config}, nil
}

func (c *Client) AddMessage(ctx context.Context, agentID string, msg schema.Message) error {
	return notImplementedError()
}

func (c *Client) GetMessages(ctx context.Context, agentID string, count int) ([]*schema.Message, error) {
	return nil, notImplementedError()
}

func (c *Client) GetContext(ctx context.Context, agentID string, maxToken int) (string, error) {
	return "", notImplementedError()
}

func (c *Client) Search(ctx context.Context, agentID string, query string, topK int, timeRangeDays int64) (string, error) {
	return "", notImplementedError()
}

func (c *Client) Flush(ctx context.Context, agentID string) error {
	return notImplementedError()
}

func (c *Client) ResetMemory(ctx context.Context, agentID string) error {
	return notImplementedError()
}

func notImplementedError() error {
	return fmt.Errorf("memos provider API is not implemented yet: confirm and map official docs https://memos-docs.openmem.net/cn/api_docs/start/overview")
}
