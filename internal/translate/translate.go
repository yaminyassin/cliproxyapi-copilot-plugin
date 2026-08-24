package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/translator/builtin"
)

const (
	EndpointResponses       = "/responses"
	EndpointChatCompletions = "/chat/completions"
	EndpointMessages        = "/v1/messages"
)

var registry = builtin.Registry()

// The pinned SDK contains request converters with package-level scratch state.
// Serialize registry calls until the upstream converters become concurrency-safe.
var registryCallMu sync.Mutex

func ClaudeToResponses(model string, body []byte, stream bool) ([]byte, error) {
	return request(sdktranslator.FormatClaude, sdktranslator.FormatOpenAIResponse, model, body, stream)
}

func ResponsesToClaude(ctx context.Context, model string, original, translated, body []byte) ([]byte, error) {
	return response(ctx, sdktranslator.FormatOpenAIResponse, sdktranslator.FormatClaude, model, original, translated, body)
}

func RequestForEndpoint(model string, body []byte, stream bool, endpoint string) ([]byte, error) {
	return RequestForEndpointFrom(sdktranslator.FormatOpenAIResponse.String(), model, body, stream, endpoint)
}

func RequestForEndpointFrom(source, model string, body []byte, stream bool, endpoint string) ([]byte, error) {
	from := sdktranslator.FromString(source)
	to, err := endpointFormat(endpoint)
	if err != nil {
		return nil, err
	}
	var out []byte
	if from == to {
		out = append([]byte(nil), body...)
	} else {
		out, err = request(from, to, model, body, stream)
	}
	if err != nil {
		return nil, err
	}
	return setModelAndStream(out, model, stream)
}

func ResponseToResponses(ctx context.Context, endpoint, model string, original, translated, body []byte) ([]byte, error) {
	return ResponseFromEndpoint(ctx, endpoint, sdktranslator.FormatOpenAIResponse.String(), model, original, translated, body)
}

func ResponseFromEndpoint(ctx context.Context, endpoint, destination, model string, original, translated, body []byte) ([]byte, error) {
	from, err := endpointFormat(endpoint)
	if err != nil {
		return nil, err
	}
	to := sdktranslator.FromString(destination)
	if from == to {
		if !json.Valid(body) {
			return nil, fmt.Errorf("Copilot API returned invalid JSON")
		}
		return append([]byte(nil), body...), nil
	}
	return response(ctx, from, to, model, original, translated, body)
}

func StreamToResponses(ctx context.Context, endpoint, model string, original, translated, frame []byte, state *any) ([][]byte, error) {
	return StreamFromEndpoint(ctx, endpoint, sdktranslator.FormatOpenAIResponse.String(), model, original, translated, frame, state)
}

func StreamFromEndpoint(ctx context.Context, endpoint, destination, model string, original, translated, frame []byte, state *any) ([][]byte, error) {
	from, err := endpointFormat(endpoint)
	if err != nil {
		return nil, err
	}
	to := sdktranslator.FromString(destination)
	if from == to {
		return [][]byte{append([]byte(nil), frame...)}, nil
	}
	return stream(ctx, from, to, model, original, translated, frame, state)
}

func ResponsesSSEToClaude(ctx context.Context, model string, original, translated, frame []byte, state *any) ([][]byte, error) {
	return stream(ctx, sdktranslator.FormatOpenAIResponse, sdktranslator.FormatClaude, model, original, translated, frame, state)
}

func request(from, to sdktranslator.Format, model string, body []byte, stream bool) ([]byte, error) {
	if !json.Valid(body) {
		return nil, fmt.Errorf("request body is not valid JSON")
	}
	if from == sdktranslator.FormatOpenAI && to == sdktranslator.FormatOpenAIResponse {
		claude := registryTranslateRequest(from, sdktranslator.FormatClaude, model, body, stream)
		if len(claude) == 0 || !json.Valid(claude) {
			return nil, fmt.Errorf("official request translation from %s to %s failed", from, sdktranslator.FormatClaude)
		}
		return claudeRequestToResponses(model, claude, stream)
	}
	if from == sdktranslator.FormatClaude && to == sdktranslator.FormatOpenAIResponse {
		return claudeRequestToResponses(model, body, stream)
	}
	if from != to && !registry.HasRequestTransformer(from, to) {
		intermediate, ok := intermediateFormat(from, to)
		if !ok {
			return nil, fmt.Errorf("official translator has no request route from %s to %s", from, to)
		}
		first := registryTranslateRequest(from, intermediate, model, body, stream)
		out := registryTranslateRequest(intermediate, to, model, first, stream)
		if len(out) == 0 || !json.Valid(out) {
			return nil, fmt.Errorf("official two-hop request translation from %s to %s failed", from, to)
		}
		return out, nil
	}
	out := registryTranslateRequest(from, to, model, body, stream)
	if len(out) == 0 || !json.Valid(out) {
		return nil, fmt.Errorf("official request translation from %s to %s failed", from, to)
	}
	return out, nil
}

func response(ctx context.Context, from, to sdktranslator.Format, model string, original, translated, body []byte) ([]byte, error) {
	if !json.Valid(body) {
		return nil, fmt.Errorf("response body is not valid JSON")
	}
	if from == sdktranslator.FormatOpenAIResponse && to == sdktranslator.FormatOpenAI {
		return responsesResponseToOpenAI(model, body)
	}
	if from == sdktranslator.FormatOpenAIResponse && to == sdktranslator.FormatClaude {
		return responsesResponseToClaude(model, body)
	}
	if from != to && !registry.HasNonStreamResponseTransformer(to, from) {
		intermediate, ok := intermediateFormat(to, from)
		if !ok {
			return nil, fmt.Errorf("official translator has no response route from %s to %s", from, to)
		}
		intermediateRequest := registryTranslateRequest(to, intermediate, model, original, false)
		first := registryTranslateNonStream(ctx, from, intermediate, model, intermediateRequest, translated, body, nil)
		out := registryTranslateNonStream(ctx, intermediate, to, model, original, intermediateRequest, first, nil)
		if len(out) == 0 || !json.Valid(out) {
			return nil, fmt.Errorf("official two-hop response translation from %s to %s failed", from, to)
		}
		return out, nil
	}
	out := registryTranslateNonStream(ctx, from, to, model, original, translated, body, nil)
	if len(out) == 0 || !json.Valid(out) {
		return nil, fmt.Errorf("official response translation from %s to %s failed", from, to)
	}
	return out, nil
}

func stream(ctx context.Context, from, to sdktranslator.Format, model string, original, translated, frame []byte, state *any) ([][]byte, error) {
	if from == sdktranslator.FormatOpenAIResponse && to == sdktranslator.FormatOpenAI {
		return responsesStreamToOpenAI(ctx, model, original, frame, state)
	}
	if from == sdktranslator.FormatOpenAIResponse && to == sdktranslator.FormatClaude {
		return responsesStreamToClaude(model, frame, state)
	}
	if from != to && !registry.HasStreamResponseTransformer(to, from) {
		intermediate, ok := intermediateFormat(to, from)
		if !ok {
			return nil, fmt.Errorf("official translator has no stream route from %s to %s", from, to)
		}
		if state == nil {
			return nil, fmt.Errorf("two-hop stream translation requires state")
		}
		hopState, okState := (*state).(*twoHopStreamState)
		if !okState {
			hopState = &twoHopStreamState{}
			*state = hopState
		}
		intermediateRequest := registryTranslateRequest(to, intermediate, model, original, true)
		first := registryTranslateStream(ctx, from, intermediate, model, intermediateRequest, translated, frame, &hopState.First)
		var out [][]byte
		for _, firstFrame := range first {
			out = append(out, registryTranslateStream(ctx, intermediate, to, model, original, intermediateRequest, firstFrame, &hopState.Second)...)
		}
		return out, nil
	}
	out := registryTranslateStream(ctx, from, to, model, original, translated, frame, state)
	if len(out) == 1 && bytes.Equal(out[0], frame) && from != to {
		return nil, fmt.Errorf("official stream translation from %s to %s fell back unchanged", from, to)
	}
	return out, nil
}

type twoHopStreamState struct {
	First  any
	Second any
}

func registryTranslateRequest(from, to sdktranslator.Format, model string, body []byte, stream bool) []byte {
	registryCallMu.Lock()
	defer registryCallMu.Unlock()
	return registry.TranslateRequest(from, to, model, body, stream)
}

func registryTranslateNonStream(ctx context.Context, from, to sdktranslator.Format, model string, original, translated, body []byte, state *any) []byte {
	registryCallMu.Lock()
	defer registryCallMu.Unlock()
	return registry.TranslateNonStream(ctx, from, to, model, original, translated, body, state)
}

func registryTranslateStream(ctx context.Context, from, to sdktranslator.Format, model string, original, translated, frame []byte, state *any) [][]byte {
	registryCallMu.Lock()
	defer registryCallMu.Unlock()
	return registry.TranslateStream(ctx, from, to, model, original, translated, frame, state)
}

func intermediateFormat(from, to sdktranslator.Format) (sdktranslator.Format, bool) {
	for _, candidate := range []sdktranslator.Format{
		sdktranslator.FormatOpenAI,
		sdktranslator.FormatOpenAIResponse,
		sdktranslator.FormatClaude,
	} {
		if candidate != from && candidate != to &&
			registry.HasRequestTransformer(from, candidate) &&
			registry.HasRequestTransformer(candidate, to) {
			return candidate, true
		}
	}
	return "", false
}

func endpointFormat(endpoint string) (sdktranslator.Format, error) {
	switch endpoint {
	case EndpointResponses:
		return sdktranslator.FormatOpenAIResponse, nil
	case EndpointChatCompletions:
		return sdktranslator.FormatOpenAI, nil
	case EndpointMessages:
		return sdktranslator.FormatClaude, nil
	default:
		return "", fmt.Errorf("unsupported Copilot endpoint %q", endpoint)
	}
}

func setModelAndStream(body []byte, model string, streamEnabled bool) ([]byte, error) {
	var value map[string]any
	if errUnmarshal := json.Unmarshal(body, &value); errUnmarshal != nil {
		return nil, fmt.Errorf("decode translated request: %w", errUnmarshal)
	}
	value["model"] = model
	value["stream"] = streamEnabled
	out, errMarshal := json.Marshal(value)
	if errMarshal != nil {
		return nil, fmt.Errorf("encode translated request: %w", errMarshal)
	}
	return out, nil
}
