// Package thinking provides unified thinking configuration processing.
package thinking

import (
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// StripThinkingConfig removes thinking configuration fields from request body.
//
// This function is used when a model doesn't support thinking but the request
// contains thinking configuration. The configuration is silently removed to
// prevent upstream API errors.
//
// Parameters:
//   - body: Original request body JSON
//   - provider: Provider name (determines which fields to strip)
//
// Returns:
//   - Modified request body JSON with thinking configuration removed
//   - Original body is returned unchanged if:
//   - body is empty or invalid JSON
//   - provider is unknown
//   - no thinking configuration found
func StripThinkingConfig(body []byte, provider string) []byte {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return body
	}

	var paths []string
	switch provider {
	case "claude":
		paths = []string{"thinking"}
	case "gemini":
		paths = []string{"generationConfig.thinkingConfig"}
	case "gemini-cli", "antigravity":
		paths = []string{"request.generationConfig.thinkingConfig"}
	case "openai":
		paths = []string{"reasoning_effort"}
	case "codex":
		paths = []string{"reasoning.effort"}
	case "iflow":
		paths = []string{
			"chat_template_kwargs.enable_thinking",
			"chat_template_kwargs.clear_thinking",
			"reasoning_split",
			"reasoning_effort",
		}
	default:
		return body
	}

	result := body
	for _, path := range paths {
		result, _ = sjson.DeleteBytes(result, path)
	}
	return result
}

// DowngradeThinkingToText converts thinking blocks to regular text blocks.
// This is used when routing from a thinking-capable model to one that doesn't support thinking.
// Unlike StripThinkingConfig which only removes configuration, this function:
// 1. Removes thinking configuration
// 2. Converts thinking content blocks to text blocks (preserving the content)
// 3. Removes signature fields
//
// This approach follows Antigravity-Manager's strategy of preserving thinking content
// as regular text rather than discarding it entirely.
func DowngradeThinkingToText(body []byte, provider string) []byte {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return body
	}

	// Step 1: Strip thinking configuration
	body = StripThinkingConfig(body, provider)

	// Step 2: Convert thinking blocks in messages to text blocks
	// This handles Claude-format messages with content arrays
	messagesResult := gjson.GetBytes(body, "messages")
	if !messagesResult.Exists() || !messagesResult.IsArray() {
		return body
	}

	// Process each message
	messages := messagesResult.Array()
	modified := false

	for msgIdx, msg := range messages {
		contentResult := msg.Get("content")
		if !contentResult.Exists() || !contentResult.IsArray() {
			continue
		}

		contents := contentResult.Array()
		for contentIdx, content := range contents {
			contentType := content.Get("type").String()

			// Convert thinking blocks to text blocks
			if contentType == "thinking" {
				thinkingText := content.Get("thinking").String()
				if thinkingText == "" {
					thinkingText = content.Get("text").String()
				}

				if thinkingText != "" {
					// Replace the thinking block with a text block
					path := "messages." + itoa(msgIdx) + ".content." + itoa(contentIdx)

					// Set type to text
					body, _ = sjson.SetBytes(body, path+".type", "text")
					// Set text content (use thinking content)
					body, _ = sjson.SetBytes(body, path+".text", thinkingText)
					// Remove thinking-specific fields
					body, _ = sjson.DeleteBytes(body, path+".thinking")
					body, _ = sjson.DeleteBytes(body, path+".signature")

					modified = true
				}
			}

			// Also remove signature from any content block that has it
			if content.Get("signature").Exists() {
				path := "messages." + itoa(msgIdx) + ".content." + itoa(contentIdx) + ".signature"
				body, _ = sjson.DeleteBytes(body, path)
				modified = true
			}
		}
	}

	if modified {
		// Also clean up any top-level thinking-related fields that might cause issues
		body, _ = sjson.DeleteBytes(body, "thinking")
	}

	return body
}

// itoa converts an integer to a string (simple helper to avoid importing strconv)
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b strings.Builder
	if i < 0 {
		b.WriteByte('-')
		i = -i
	}
	var digits []byte
	for i > 0 {
		digits = append(digits, byte('0'+i%10))
		i /= 10
	}
	for j := len(digits) - 1; j >= 0; j-- {
		b.WriteByte(digits[j])
	}
	return b.String()
}
