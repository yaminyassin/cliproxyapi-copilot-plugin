package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type responsesOpenAIStreamState struct {
	Started      bool
	Stopped      bool
	ID           string
	Model        string
	Created      int64
	SawText      bool
	SawReasoning bool
	HasToolCalls bool
	NextTool     int
	Tools        map[string]*responsesOpenAIToolState
	ToolsByID    map[string]*responsesOpenAIToolState
}

type responsesOpenAIToolState struct {
	Index        int
	Started      bool
	SawArguments bool
}

func responsesStreamToOpenAI(_ context.Context, model string, _, frame []byte, state *any) ([][]byte, error) {
	if state == nil {
		return nil, fmt.Errorf("Responses-to-OpenAI stream translation requires state")
	}
	streamState, ok := (*state).(*responsesOpenAIStreamState)
	if !ok {
		streamState = &responsesOpenAIStreamState{
			Model:     model,
			Tools:     make(map[string]*responsesOpenAIToolState),
			ToolsByID: make(map[string]*responsesOpenAIToolState),
		}
		*state = streamState
	}

	event, data, done, errFrame := parseSSEFrame(frame)
	if errFrame != nil {
		return nil, errFrame
	}
	if done {
		if streamState.Stopped {
			return nil, nil
		}
		return nil, fmt.Errorf("Responses stream ended before a terminal response event")
	}
	if len(data) == 0 {
		return nil, nil
	}
	payload, errDecode := decodeObject(data)
	if errDecode != nil {
		return nil, fmt.Errorf("decode Responses SSE event %q: %w", event, errDecode)
	}
	if event == "" {
		event = stringValue(payload["type"])
	}
	if event == "error" || event == "response.failed" {
		errorObject := objectValue(payload["error"])
		if response := objectValue(payload["response"]); len(response) > 0 {
			if nested := objectValue(response["error"]); len(nested) > 0 {
				errorObject = nested
			}
		}
		message := firstNonEmptyString(stringValue(errorObject["message"]), stringValue(payload["message"]), "unknown upstream stream error")
		return nil, fmt.Errorf("Copilot Responses stream failed: %s", message)
	}

	response := objectValue(payload["response"])
	var out [][]byte
	if !streamState.Started && strings.HasPrefix(event, "response.") {
		out = append(out, streamState.start(response, payload)...)
	}

	switch event {
	case "response.content_part.added":
		part := objectValue(payload["part"])
		if text := firstRawString(part, "text", "refusal"); text != "" {
			streamState.SawText = true
			out = append(out, streamState.delta(map[string]any{"content": text})...)
		}
	case "response.output_text.delta":
		if delta := rawStringValue(payload["delta"]); delta != "" {
			streamState.SawText = true
			out = append(out, streamState.delta(map[string]any{"content": delta})...)
		}
	case "response.refusal.delta":
		if delta := rawStringValue(payload["delta"]); delta != "" {
			streamState.SawText = true
			out = append(out, streamState.delta(map[string]any{"refusal": delta})...)
		}
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		if delta := rawStringValue(payload["delta"]); delta != "" {
			streamState.SawReasoning = true
			out = append(out, streamState.delta(map[string]any{"reasoning_content": delta})...)
		}
	case "response.output_item.added":
		item := objectValue(payload["item"])
		if isResponsesToolItem(item) {
			out = append(out, streamState.startTool(itemKey(payload), item)...)
		}
	case "response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
		key := itemKey(payload)
		tool := streamState.tool(key, payload)
		if !tool.Started {
			out = append(out, streamState.startTool(key, payload)...)
		}
		if delta := rawStringValue(payload["delta"]); delta != "" {
			tool.SawArguments = true
			out = append(out, streamState.toolArguments(tool.Index, delta)...)
		}
	case "response.output_item.done":
		item := objectValue(payload["item"])
		out = append(out, streamState.emitCompletedItem(itemKey(payload), item)...)
	case "response.completed", "response.incomplete":
		if event == "response.completed" {
			if errFailure := responsesFailure(response); errFailure != nil {
				return nil, errFailure
			}
		}
		out = append(out, streamState.emitCompletedResponse(response)...)
		out = append(out, streamState.finish(response)...)
	}
	return out, nil
}

func (s *responsesOpenAIStreamState) start(response, payload map[string]any) [][]byte {
	if s.Started {
		return nil
	}
	s.Started = true
	s.ID = firstNonEmptyString(stringValue(response["id"]), stringValue(payload["response_id"]), "chatcmpl_copilot")
	s.Model = firstNonEmptyString(stringValue(response["model"]), s.Model)
	s.Created = int64Value(response["created_at"])
	return [][]byte{s.chunk(map[string]any{"role": "assistant", "content": ""}, nil, nil)}
}

func (s *responsesOpenAIStreamState) delta(delta map[string]any) [][]byte {
	return [][]byte{s.chunk(delta, nil, nil)}
}

func (s *responsesOpenAIStreamState) tool(key string, item map[string]any) *responsesOpenAIToolState {
	if tool := s.Tools[key]; tool != nil {
		return tool
	}
	callID := firstString(item, "call_id", "id")
	if callID != "" {
		if tool := s.ToolsByID[callID]; tool != nil {
			s.Tools[key] = tool
			return tool
		}
	}
	tool := &responsesOpenAIToolState{Index: s.NextTool}
	s.NextTool++
	s.Tools[key] = tool
	if callID != "" {
		s.ToolsByID[callID] = tool
	}
	return tool
}

func (s *responsesOpenAIStreamState) startTool(key string, item map[string]any) [][]byte {
	tool := s.tool(key, item)
	if tool.Started {
		return nil
	}
	tool.Started = true
	s.HasToolCalls = true
	callID := firstString(item, "call_id", "id")
	return s.delta(map[string]any{"tool_calls": []any{map[string]any{
		"index": tool.Index,
		"id":    callID,
		"type":  "function",
		"function": map[string]any{
			"name":      stringValue(item["name"]),
			"arguments": "",
		},
	}}})
}

func (s *responsesOpenAIStreamState) toolArguments(index int, arguments string) [][]byte {
	return s.delta(map[string]any{"tool_calls": []any{map[string]any{
		"index": index,
		"function": map[string]any{
			"arguments": arguments,
		},
	}}})
}

func (s *responsesOpenAIStreamState) emitCompletedItem(key string, item map[string]any) [][]byte {
	if isResponsesToolItem(item) {
		tool := s.tool(key, item)
		var out [][]byte
		if !tool.Started {
			out = append(out, s.startTool(key, item)...)
		}
		if !tool.SawArguments {
			if arguments := firstRawString(item, "arguments", "input"); arguments != "" {
				tool.SawArguments = true
				out = append(out, s.toolArguments(tool.Index, arguments)...)
			}
		}
		return out
	}
	if stringValue(item["type"]) == "message" && !s.SawText {
		if text := responsesMessageText(item); text != "" {
			s.SawText = true
			return s.delta(map[string]any{"content": text})
		}
	}
	if stringValue(item["type"]) == "reasoning" && !s.SawReasoning {
		if reasoning := responsesReasoningText(item); reasoning != "" {
			s.SawReasoning = true
			return s.delta(map[string]any{"reasoning_content": reasoning})
		}
	}
	return nil
}

func (s *responsesOpenAIStreamState) emitCompletedResponse(response map[string]any) [][]byte {
	var out [][]byte
	for index, rawItem := range arrayValue(response["output"]) {
		item := objectValue(rawItem)
		key := fmt.Sprintf("item:%d", index)
		out = append(out, s.emitCompletedItem(key, item)...)
	}
	return out
}

func (s *responsesOpenAIStreamState) finish(response map[string]any) [][]byte {
	if s.Stopped {
		return nil
	}
	s.Stopped = true
	return [][]byte{
		s.chunk(map[string]any{}, openAIFinishReason(response, s.HasToolCalls), openAIUsage(response)),
		[]byte("[DONE]"),
	}
}

func (s *responsesOpenAIStreamState) chunk(delta map[string]any, finishReason any, usage map[string]any) []byte {
	payload := map[string]any{
		"id":      s.ID,
		"object":  "chat.completion.chunk",
		"created": s.Created,
		"model":   s.Model,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         delta,
			"finish_reason": finishReason,
		}},
	}
	if usage != nil {
		payload["usage"] = usage
	}
	data, _ := json.Marshal(payload)
	return data
}

func isResponsesToolItem(item map[string]any) bool {
	switch strings.ToLower(stringValue(item["type"])) {
	case "function_call", "custom_tool_call":
		return true
	default:
		return false
	}
}

func responsesMessageText(item map[string]any) string {
	var text strings.Builder
	for _, rawPart := range arrayValue(item["content"]) {
		part := objectValue(rawPart)
		switch strings.ToLower(stringValue(part["type"])) {
		case "output_text", "text":
			text.WriteString(rawStringValue(part["text"]))
		case "refusal":
			text.WriteString(firstRawString(part, "refusal", "text"))
		}
	}
	return text.String()
}
