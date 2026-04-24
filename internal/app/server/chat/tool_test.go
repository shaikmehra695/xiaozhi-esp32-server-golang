package chat

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestNormalizeToolCallIDsFillsMissingIDs(t *testing.T) {
	calls := []schema.ToolCall{
		{
			Type: "function",
			Function: schema.FunctionCall{
				Name:      "get_eth0_ip",
				Arguments: `{}`,
			},
		},
		{
			ID:   "existing-id",
			Type: "function",
			Function: schema.FunctionCall{
				Name:      "get_weather",
				Arguments: `{"city":"Shanghai"}`,
			},
		},
	}

	normalized := normalizeToolCallIDs(calls)
	if normalized[0].ID == "" {
		t.Fatalf("expected missing tool call id to be generated")
	}
	if normalized[1].ID != "existing-id" {
		t.Fatalf("expected existing id to remain unchanged, got %q", normalized[1].ID)
	}
}
