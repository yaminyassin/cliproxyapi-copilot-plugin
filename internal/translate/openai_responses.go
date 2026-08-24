package translate

import (
	"encoding/json"
	"strings"
)

func responsesResponseToOpenAI(model string, body []byte) ([]byte, error) {
	root, errDecode := decodeObject(body)
	if errDecode != nil {
		return nil, errDecode
	}
	if errFailure := responsesFailure(root); errFailure != nil {
		return nil, errFailure
	}

	var text strings.Builder
	var reasoning strings.Builder
	var refusal strings.Builder
	toolCalls := make([]any, 0)
	for _, rawItem := range arrayValue(root["output"]) {
		item := objectValue(rawItem)
		switch strings.ToLower(stringValue(item["type"])) {
		case "message":
			for _, rawPart := range arrayValue(item["content"]) {
				part := objectValue(rawPart)
				switch strings.ToLower(stringValue(part["type"])) {
				case "output_text", "text":
					text.WriteString(rawStringValue(part["text"]))
				case "refusal":
					refusal.WriteString(firstRawString(part, "refusal", "text"))
				}
			}
		case "reasoning":
			reasoning.WriteString(responsesReasoningText(item))
		case "function_call", "custom_tool_call":
			callID := firstString(item, "call_id", "id")
			toolCalls = append(toolCalls, map[string]any{
				"id":   callID,
				"type": "function",
				"function": map[string]any{
					"name":      stringValue(item["name"]),
					"arguments": firstRawString(item, "arguments", "input"),
				},
			})
		}
	}
	if text.Len() == 0 {
		text.WriteString(rawStringValue(root["output_text"]))
	}

	message := map[string]any{
		"role":    "assistant",
		"content": text.String(),
	}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	if refusal.Len() > 0 {
		message["refusal"] = refusal.String()
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	result := map[string]any{
		"id":      stringValue(root["id"]),
		"object":  "chat.completion",
		"created": int64Value(root["created_at"]),
		"model":   firstNonEmptyString(stringValue(root["model"]), model),
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": openAIFinishReason(root, len(toolCalls) > 0),
		}},
		"usage": openAIUsage(root),
	}
	out, errMarshal := json.Marshal(result)
	if errMarshal != nil {
		return nil, errMarshal
	}
	return out, nil
}

func openAIUsage(root map[string]any) map[string]any {
	usageIn := objectValue(root["usage"])
	inputTokens := int64Value(usageIn["input_tokens"])
	outputTokens := int64Value(usageIn["output_tokens"])
	totalTokens := int64Value(usageIn["total_tokens"])
	if totalTokens == 0 && (inputTokens > 0 || outputTokens > 0) {
		totalTokens = inputTokens + outputTokens
	}
	usage := map[string]any{
		"prompt_tokens":     inputTokens,
		"completion_tokens": outputTokens,
		"total_tokens":      totalTokens,
	}
	inputDetails := objectValue(usageIn["input_tokens_details"])
	if cachedTokens := int64Value(inputDetails["cached_tokens"]); cachedTokens > 0 {
		usage["prompt_tokens_details"] = map[string]any{"cached_tokens": cachedTokens}
	}
	outputDetails := objectValue(usageIn["output_tokens_details"])
	if reasoningTokens := int64Value(outputDetails["reasoning_tokens"]); reasoningTokens > 0 {
		usage["completion_tokens_details"] = map[string]any{"reasoning_tokens": reasoningTokens}
	}
	return usage
}

func openAIFinishReason(root map[string]any, hasToolCalls bool) string {
	finishReason := "stop"
	if hasToolCalls {
		finishReason = "tool_calls"
	}
	incompleteReason := strings.ToLower(stringValue(objectValue(root["incomplete_details"])["reason"]))
	switch incompleteReason {
	case "max_output_tokens", "max_tokens":
		return "length"
	case "content_filter":
		return "content_filter"
	default:
		return finishReason
	}
}
