package thinking

import (
	"encoding/json"
	"testing"
)

func TestDowngradeThinkingToText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		provider string
		check    func(t *testing.T, result []byte)
	}{
		{
			name:     "Empty body",
			input:    "",
			provider: "claude",
			check: func(t *testing.T, result []byte) {
				if len(result) != 0 {
					t.Errorf("expected empty result, got %s", string(result))
				}
			},
		},
		{
			name:     "Invalid JSON",
			input:    "not json",
			provider: "claude",
			check: func(t *testing.T, result []byte) {
				if string(result) != "not json" {
					t.Errorf("expected unchanged input, got %s", string(result))
				}
			},
		},
		{
			name: "No messages field",
			input: `{
				"model": "claude-3-5-sonnet",
				"max_tokens": 1024
			}`,
			provider: "claude",
			check: func(t *testing.T, result []byte) {
				var data map[string]interface{}
				if err := json.Unmarshal(result, &data); err != nil {
					t.Fatalf("failed to unmarshal result: %v", err)
				}
				if data["model"] != "claude-3-5-sonnet" {
					t.Errorf("expected model to be preserved")
				}
			},
		},
		{
			name: "Messages without content array",
			input: `{
				"model": "claude-3-5-sonnet",
				"messages": [
					{"role": "user", "content": "Hello"}
				]
			}`,
			provider: "claude",
			check: func(t *testing.T, result []byte) {
				var data map[string]interface{}
				if err := json.Unmarshal(result, &data); err != nil {
					t.Fatalf("failed to unmarshal result: %v", err)
				}
				messages := data["messages"].([]interface{})
				if len(messages) != 1 {
					t.Errorf("expected 1 message, got %d", len(messages))
				}
			},
		},
		{
			name: "Convert thinking block to text",
			input: `{
				"model": "claude-3-5-sonnet",
				"messages": [
					{
						"role": "assistant",
						"content": [
							{
								"type": "thinking",
								"thinking": "Let me analyze this problem step by step...",
								"signature": "abc123"
							},
							{
								"type": "text",
								"text": "Here is my answer."
							}
						]
					}
				]
			}`,
			provider: "claude",
			check: func(t *testing.T, result []byte) {
				var data map[string]interface{}
				if err := json.Unmarshal(result, &data); err != nil {
					t.Fatalf("failed to unmarshal result: %v", err)
				}
				messages := data["messages"].([]interface{})
				msg := messages[0].(map[string]interface{})
				content := msg["content"].([]interface{})

				// First content block should be converted to text
				firstBlock := content[0].(map[string]interface{})
				if firstBlock["type"] != "text" {
					t.Errorf("expected type to be 'text', got %v", firstBlock["type"])
				}
				if firstBlock["text"] != "Let me analyze this problem step by step..." {
					t.Errorf("expected thinking content to be preserved as text")
				}
				if _, hasSignature := firstBlock["signature"]; hasSignature {
					t.Errorf("expected signature to be removed")
				}
				if _, hasThinking := firstBlock["thinking"]; hasThinking {
					t.Errorf("expected thinking field to be removed")
				}

				// Second content block should be unchanged
				secondBlock := content[1].(map[string]interface{})
				if secondBlock["type"] != "text" {
					t.Errorf("expected second block type to be 'text'")
				}
			},
		},
		{
			name: "Remove signature from non-thinking blocks",
			input: `{
				"model": "claude-3-5-sonnet",
				"messages": [
					{
						"role": "assistant",
						"content": [
							{
								"type": "text",
								"text": "Some text",
								"signature": "should-be-removed"
							}
						]
					}
				]
			}`,
			provider: "claude",
			check: func(t *testing.T, result []byte) {
				var data map[string]interface{}
				if err := json.Unmarshal(result, &data); err != nil {
					t.Fatalf("failed to unmarshal result: %v", err)
				}
				messages := data["messages"].([]interface{})
				msg := messages[0].(map[string]interface{})
				content := msg["content"].([]interface{})
				block := content[0].(map[string]interface{})

				if _, hasSignature := block["signature"]; hasSignature {
					t.Errorf("expected signature to be removed from text block")
				}
				if block["text"] != "Some text" {
					t.Errorf("expected text content to be preserved")
				}
			},
		},
		{
			name: "Strip thinking config for Claude",
			input: `{
				"model": "claude-3-5-sonnet",
				"thinking": {"type": "enabled", "budget_tokens": 10000},
				"messages": [{"role": "user", "content": "Hello"}]
			}`,
			provider: "claude",
			check: func(t *testing.T, result []byte) {
				var data map[string]interface{}
				if err := json.Unmarshal(result, &data); err != nil {
					t.Fatalf("failed to unmarshal result: %v", err)
				}
				if _, hasThinking := data["thinking"]; hasThinking {
					t.Errorf("expected thinking config to be stripped")
				}
			},
		},
		{
			name: "Strip thinking config for Gemini",
			input: `{
				"model": "gemini-2.5-flash",
				"generationConfig": {"thinkingConfig": {"thinkingBudget": 10000}},
				"contents": [{"role": "user", "parts": [{"text": "Hello"}]}]
			}`,
			provider: "gemini",
			check: func(t *testing.T, result []byte) {
				var data map[string]interface{}
				if err := json.Unmarshal(result, &data); err != nil {
					t.Fatalf("failed to unmarshal result: %v", err)
				}
				genConfig, ok := data["generationConfig"].(map[string]interface{})
				if ok {
					if _, hasThinking := genConfig["thinkingConfig"]; hasThinking {
						t.Errorf("expected thinkingConfig to be stripped")
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DowngradeThinkingToText([]byte(tt.input), tt.provider)
			tt.check(t, result)
		})
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{10, "10"},
		{123, "123"},
		{-1, "-1"},
		{-123, "-123"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := itoa(tt.input)
			if result != tt.expected {
				t.Errorf("itoa(%d) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}
