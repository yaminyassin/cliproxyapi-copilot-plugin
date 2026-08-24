package translate

import (
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestClaudeRequestToResponsesPreservesCoreContent(t *testing.T) {
	t.Parallel()

	claudeRequest := []byte(`{
		"model":"ignored",
		"max_tokens":321,
		"system":"Be concise.",
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"describe this"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"YWJj"}}
			]},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"inspect first","signature":"sig"},
				{"type":"tool_use","id":"call_1","name":"lookup","input":{"q":"x"}}
			]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"found"}]}
		],
		"tools":[{"name":"lookup","description":"Look up data","input_schema":{"type":"object","properties":{"q":{"type":"string"}}}}]
	}`)

	out, err := RequestForEndpointFrom("claude", "gpt-5.6-sol", claudeRequest, false, EndpointResponses)
	if err != nil {
		t.Fatalf("translate Claude request: %v", err)
	}
	data := gjson.ParseBytes(out)
	if got := data.Get("model").String(); got != "gpt-5.6-sol" {
		t.Fatalf("model = %q", got)
	}
	if got := data.Get("max_output_tokens").Int(); got != 321 {
		t.Fatalf("max_output_tokens = %d", got)
	}
	text := string(out)
	for _, needle := range []string{
		"Be concise.",
		"describe this",
		"data:image/png;base64,YWJj",
		"inspect first",
		`"type":"function_call"`,
		`"call_id":"call_1"`,
		`"type":"function_call_output"`,
		`"name":"lookup"`,
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("translated request omits %q: %s", needle, out)
		}
	}
}

func TestClaudeStructuredOutputAddsResponsesSchemaName(t *testing.T) {
	t.Parallel()

	original := []byte(`{
		"model":"gpt-5.6-terra",
		"messages":[{"role":"user","content":"Create a title."}],
		"output_config":{"format":{"type":"json_schema","schema":{"type":"object"}}}
	}`)
	out, err := RequestForEndpointFrom("claude", "gpt-5.6-terra", original, true, EndpointResponses)
	if err != nil {
		t.Fatalf("translate request: %v", err)
	}
	if got := gjson.GetBytes(out, "text.format.name").String(); got != "claude_structured_output" {
		t.Fatalf("text.format.name = %q; request=%s", got, out)
	}
}

func TestChatNonStreamResponseToResponses(t *testing.T) {
	t.Parallel()

	original := []byte(`{"model":"gpt-test","input":[{"role":"user","content":[{"type":"input_text","text":"ping"}]}]}`)
	translated, err := RequestForEndpoint("gpt-test", original, false, EndpointChatCompletions)
	if err != nil {
		t.Fatalf("translate request: %v", err)
	}
	upstream := []byte(`{
		"id":"chatcmpl_1","object":"chat.completion","created":1,"model":"gpt-test",
		"choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}
	}`)
	out, err := ResponseToResponses(context.Background(), EndpointChatCompletions, "gpt-test", original, translated, upstream)
	if err != nil {
		t.Fatalf("translate response: %v", err)
	}
	data := gjson.ParseBytes(out)
	if got := data.Get("status").String(); got != "completed" {
		t.Fatalf("status = %q; response=%s", got, out)
	}
	if got := data.Get("output.0.content.0.text").String(); got != "pong" {
		t.Fatalf("output text = %q; response=%s", got, out)
	}
	if got := data.Get("usage.total_tokens").Int(); got != 3 {
		t.Fatalf("total tokens = %d; response=%s", got, out)
	}
}

func TestOpenAIChatRequestToResponses(t *testing.T) {
	t.Parallel()

	original := []byte(`{
		"model":"ignored",
		"messages":[{"role":"user","content":"ping"}],
		"max_tokens":64
	}`)
	claudeRequest, err := RequestForEndpointFrom("openai", "gpt-5.6-luna", original, false, EndpointMessages)
	if err != nil {
		t.Fatalf("translate request through Claude bridge: %v", err)
	}
	if !gjson.ValidBytes(claudeRequest) {
		t.Fatalf("Claude bridge request is invalid JSON: %s", claudeRequest)
	}
	out, err := RequestForEndpointFrom("openai", "gpt-5.6-luna", original, false, EndpointResponses)
	if err != nil {
		t.Fatalf("translate request: %v", err)
	}
	data := gjson.ParseBytes(out)
	if got := data.Get("model").String(); got != "gpt-5.6-luna" {
		t.Fatalf("model = %q; request=%s", got, out)
	}
	if got := data.Get("max_output_tokens").Int(); got != 64 {
		t.Fatalf("max_output_tokens = %d; request=%s", got, out)
	}
	if !strings.Contains(string(out), "ping") {
		t.Fatalf("translated request omits user text: %s", out)
	}
}

func TestResponsesNonStreamResponseToOpenAIChat(t *testing.T) {
	t.Parallel()

	original := []byte(`{"model":"gpt-5.6-luna","messages":[{"role":"user","content":"ping"}]}`)
	translated, err := RequestForEndpointFrom("openai", "gpt-5.6-luna", original, false, EndpointResponses)
	if err != nil {
		t.Fatalf("translate request: %v", err)
	}
	upstream := []byte(`{
		"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"gpt-5.6-luna",
		"output":[
			{"id":"reason_1","type":"reasoning","summary":[{"type":"summary_text","text":"brief thought"}]},
			{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"pong"}]}
		],
		"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3,"input_tokens_details":{"cached_tokens":1},"output_tokens_details":{"reasoning_tokens":1}}
	}`)
	out, err := ResponseFromEndpoint(context.Background(), EndpointResponses, "openai", "gpt-5.6-luna", original, translated, upstream)
	if err != nil {
		t.Fatalf("translate response: %v", err)
	}
	data := gjson.ParseBytes(out)
	if got := data.Get("object").String(); got != "chat.completion" {
		t.Fatalf("object = %q; response=%s", got, out)
	}
	if got := data.Get("choices.0.message.content").String(); got != "pong" {
		t.Fatalf("content = %q; response=%s", got, out)
	}
	if got := data.Get("choices.0.message.reasoning_content").String(); got != "brief thought" {
		t.Fatalf("reasoning_content = %q; response=%s", got, out)
	}
	if got := data.Get("usage.total_tokens").Int(); got != 3 {
		t.Fatalf("total_tokens = %d; response=%s", got, out)
	}
	if got := data.Get("usage.prompt_tokens_details.cached_tokens").Int(); got != 1 {
		t.Fatalf("cached_tokens = %d; response=%s", got, out)
	}
}

func TestResponsesFunctionCallToOpenAIChat(t *testing.T) {
	t.Parallel()

	original := []byte(`{"model":"gpt-5.6-luna","messages":[{"role":"user","content":"look up x"}]}`)
	translated, err := RequestForEndpointFrom("openai", "gpt-5.6-luna", original, false, EndpointResponses)
	if err != nil {
		t.Fatalf("translate request: %v", err)
	}
	upstream := []byte(`{
		"id":"resp_tools","status":"completed","model":"gpt-5.6-luna",
		"output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"x\"}"}],
		"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}
	}`)
	out, err := ResponseFromEndpoint(context.Background(), EndpointResponses, "openai", "gpt-5.6-luna", original, translated, upstream)
	if err != nil {
		t.Fatalf("translate response: %v", err)
	}
	data := gjson.ParseBytes(out)
	if got := data.Get("choices.0.finish_reason").String(); got != "tool_calls" {
		t.Fatalf("finish_reason = %q; response=%s", got, out)
	}
	if got := data.Get("choices.0.message.tool_calls.0.id").String(); got != "call_1" {
		t.Fatalf("tool call id = %q; response=%s", got, out)
	}
	if got := data.Get("choices.0.message.tool_calls.0.function.name").String(); got != "lookup" {
		t.Fatalf("tool name = %q; response=%s", got, out)
	}
}

func TestResponsesNonStreamResponseToClaude(t *testing.T) {
	t.Parallel()

	original := []byte(`{"model":"gpt-5.6-sol","max_tokens":20,"messages":[{"role":"user","content":"ping"}]}`)
	translated, err := RequestForEndpointFrom("claude", "gpt-5.6-sol", original, false, EndpointResponses)
	if err != nil {
		t.Fatalf("translate request: %v", err)
	}
	upstream := []byte(`{
		"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"gpt-5.6-sol",
		"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"pong","annotations":[]}]}],
		"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}
	}`)
	out, err := ResponseFromEndpoint(context.Background(), EndpointResponses, "claude", "gpt-5.6-sol", original, translated, upstream)
	if err != nil {
		t.Fatalf("translate response: %v", err)
	}
	data := gjson.ParseBytes(out)
	if got := data.Get("type").String(); got != "message" {
		t.Fatalf("Claude response type = %q; response=%s", got, out)
	}
	if got := data.Get("content.0.text").String(); got != "pong" {
		t.Fatalf("Claude response text = %q; response=%s", got, out)
	}
	if got := data.Get("usage.input_tokens").Int(); got != 2 {
		t.Fatalf("Claude input tokens = %d; response=%s", got, out)
	}
}

func TestChatSSEToResponses(t *testing.T) {
	t.Parallel()

	original := []byte(`{"model":"gpt-test","input":[{"role":"user","content":[{"type":"input_text","text":"ping"}]}]}`)
	translated, err := RequestForEndpoint("gpt-test", original, true, EndpointChatCompletions)
	if err != nil {
		t.Fatalf("translate request: %v", err)
	}
	chunks := [][]byte{
		[]byte(`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","content":"po"},"finish_reason":null}]}`),
		[]byte(`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[{"index":0,"delta":{"content":"ng"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`),
		[]byte(`data: [DONE]`),
	}
	var state any
	var output strings.Builder
	for _, chunk := range chunks {
		frames, errTranslate := StreamToResponses(context.Background(), EndpointChatCompletions, "gpt-test", original, translated, chunk, &state)
		if errTranslate != nil {
			t.Fatalf("translate SSE: %v", errTranslate)
		}
		for _, frame := range frames {
			output.Write(frame)
		}
	}
	text := output.String()
	for _, needle := range []string{
		"event: response.created",
		"event: response.output_text.delta",
		`"delta":"po"`,
		`"delta":"ng"`,
		"event: response.completed",
		`"total_tokens":3`,
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("translated SSE omits %q:\n%s", needle, text)
		}
	}
}

func TestResponsesSSEToClaude(t *testing.T) {
	t.Parallel()

	original := []byte(`{"model":"gpt-5.6-sol","max_tokens":20,"messages":[{"role":"user","content":"ping"}]}`)
	translated, err := RequestForEndpointFrom("claude", "gpt-5.6-sol", original, true, EndpointResponses)
	if err != nil {
		t.Fatalf("translate request: %v", err)
	}
	chunks := [][]byte{
		[]byte(`event: response.created
data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress","model":"gpt-5.6-sol","output":[],"usage":{"input_tokens":2,"output_tokens":0}}}

`),
		[]byte(`event: response.content_part.added
data: {"type":"response.content_part.added","item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}

`),
		[]byte(`event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"one"}

`),
		[]byte(`event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":" two"}

`),
		[]byte(`event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":" three"}

`),
		[]byte(`event: response.content_part.done
data: {"type":"response.content_part.done","item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":"one two three"}}

`),
		[]byte(`event: response.completed
data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","model":"gpt-5.6-sol","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"one two three"}]}],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}

`),
	}
	var state any
	var output strings.Builder
	for _, chunk := range chunks {
		frames, errTranslate := StreamFromEndpoint(context.Background(), EndpointResponses, "claude", "gpt-5.6-sol", original, translated, chunk, &state)
		if errTranslate != nil {
			t.Fatalf("translate SSE: %v", errTranslate)
		}
		for _, frame := range frames {
			output.Write(frame)
		}
	}
	text := output.String()
	for _, needle := range []string{
		"event: message_start",
		"event: content_block_start",
		`"type":"text"`,
		`"text":"one"`,
		`"text":" two"`,
		`"text":" three"`,
		`"stop_reason":"end_turn"`,
		`"output_tokens":3`,
		"event: message_stop",
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("Claude SSE omits %q:\n%s", needle, text)
		}
	}
}

func TestResponsesSSEToOpenAIChat(t *testing.T) {
	t.Parallel()

	original := []byte(`{"model":"gpt-5.6-luna","messages":[{"role":"user","content":"ping"}],"stream":true}`)
	translated, err := RequestForEndpointFrom("openai", "gpt-5.6-luna", original, true, EndpointResponses)
	if err != nil {
		t.Fatalf("translate request: %v", err)
	}
	chunks := [][]byte{
		[]byte(`event: response.created
data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress","model":"gpt-5.6-luna","output":[],"usage":{"input_tokens":2,"output_tokens":0}}}

`),
		[]byte(`event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"po"}

`),
		[]byte(`event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"ng"}

`),
		[]byte(`event: response.completed
data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","model":"gpt-5.6-luna","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"pong"}]}],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}

`),
	}
	var state any
	var output strings.Builder
	for _, chunk := range chunks {
		frames, errTranslate := StreamFromEndpoint(context.Background(), EndpointResponses, "openai", "gpt-5.6-luna", original, translated, chunk, &state)
		if errTranslate != nil {
			t.Fatalf("translate SSE: %v", errTranslate)
		}
		for _, frame := range frames {
			output.Write(frame)
		}
	}
	text := output.String()
	for _, needle := range []string{
		`"object":"chat.completion.chunk"`,
		`"content":"po"`,
		`"content":"ng"`,
		`"finish_reason":"stop"`,
		`"total_tokens":3`,
		"[DONE]",
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("translated SSE omits %q:\n%s", needle, text)
		}
	}
}

func TestResponsesToolSSEToOpenAIChat(t *testing.T) {
	t.Parallel()

	original := []byte(`{"model":"gpt-5.6-luna","messages":[{"role":"user","content":"look up x"}],"stream":true}`)
	translated, err := RequestForEndpointFrom("openai", "gpt-5.6-luna", original, true, EndpointResponses)
	if err != nil {
		t.Fatalf("translate request: %v", err)
	}
	chunks := [][]byte{
		[]byte(`event: response.created
data: {"type":"response.created","response":{"id":"resp_tools","status":"in_progress","model":"gpt-5.6-luna","output":[]}}

`),
		[]byte(`event: response.output_item.added
data: {"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":""}}

`),
		[]byte(`event: response.function_call_arguments.delta
data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{\"q\":"}

`),
		[]byte(`event: response.function_call_arguments.delta
data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"\"x\"}"}

`),
		[]byte(`event: response.output_item.done
data: {"type":"response.output_item.done","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"x\"}"}}

`),
		[]byte(`event: response.completed
data: {"type":"response.completed","response":{"id":"resp_tools","status":"completed","model":"gpt-5.6-luna","output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"x\"}"}],"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}

`),
	}
	var state any
	var output strings.Builder
	for _, chunk := range chunks {
		frames, errTranslate := StreamFromEndpoint(context.Background(), EndpointResponses, "openai", "gpt-5.6-luna", original, translated, chunk, &state)
		if errTranslate != nil {
			t.Fatalf("translate SSE: %v", errTranslate)
		}
		for _, frame := range frames {
			output.Write(frame)
		}
	}
	text := output.String()
	for _, needle := range []string{
		`"id":"call_1"`,
		`"name":"lookup"`,
		`"arguments":"{\"q\":"`,
		`"arguments":"\"x\"}"`,
		`"finish_reason":"tool_calls"`,
		"[DONE]",
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("translated tool SSE omits %q:\n%s", needle, text)
		}
	}
	if got := strings.Count(text, `"id":"call_1"`); got != 1 {
		t.Fatalf("tool call start count = %d, want 1:\n%s", got, text)
	}
	if strings.Contains(text, "data: ") {
		t.Fatalf("chat stream payload contains an SSE envelope that CPA would wrap again:\n%s", text)
	}
}
