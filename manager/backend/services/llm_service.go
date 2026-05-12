package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// LLMConfig holds the configuration for the LLM provider
type LLMConfig struct {
	BaseURL   string `json:"base_url"`
	APIKey    string `json:"api_key"`
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens"`
}

// LLMService handles LLM API calls for feedback generation
type LLMService struct {
	config LLMConfig
	client *http.Client
}

// NewLLMService creates a new LLM service
func NewLLMService(config LLMConfig) *LLMService {
	if config.MaxTokens == 0 {
		config.MaxTokens = 1024
	}
	return &LLMService{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// GenerateFeedback sends a prompt to the LLM and returns the response text
func (s *LLMService) GenerateFeedback(systemPrompt, userPrompt string) (string, error) {
	req := chatRequest{
		Model: s.config.Model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		MaxTokens: s.config.MaxTokens,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", s.config.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.config.APIKey)

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("LLM request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("LLM returned %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("LLM returned no choices")
	}

	return chatResp.Choices[0].Message.Content, nil
}
