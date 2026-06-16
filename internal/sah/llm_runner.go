package sah

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxLLMResponseBytes = 8 * 1024 * 1024

type llmRunOptions struct {
	BaseURL     string
	Model       string
	MaxTokens   int
	Temperature float64
	Timeout     time.Duration
}

type openAIChatCompletionRequest struct {
	Model          string              `json:"model"`
	Messages       []openAIChatMessage `json:"messages"`
	MaxTokens      int                 `json:"max_tokens,omitempty"`
	Temperature    *float64            `json:"temperature,omitempty"`
	Stream         bool                `json:"stream"`
	ResponseFormat any                 `json:"response_format,omitempty"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatCompletionResponse struct {
	Choices []openAIChatChoice `json:"choices"`
	Usage   openAIChatUsage    `json:"usage"`
	Error   *openAIError       `json:"error,omitempty"`
}

type openAIChatChoice struct {
	Message      openAIChatResponseMessage `json:"message"`
	FinishReason string                    `json:"finish_reason"`
}

type openAIChatResponseMessage struct {
	Content any `json:"content"`
	Parsed  any `json:"parsed,omitempty"`
	Refusal any `json:"refusal,omitempty"`
}

type openAIChatUsage struct {
	PromptTokens              int64          `json:"prompt_tokens"`
	CompletionTokens          int64          `json:"completion_tokens"`
	TotalTokens               int64          `json:"total_tokens"`
	PromptTokensDetails       map[string]any `json:"prompt_tokens_details"`
	CompletionTokensDetails   map[string]any `json:"completion_tokens_details"`
	PromptTokenDetails        map[string]any `json:"prompt_token_details"`
	CompletionTokenDetails    map[string]any `json:"completion_token_details"`
	PromptCacheHitTokens      int64          `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens     int64          `json:"prompt_cache_miss_tokens"`
	CompletionReasoningTokens int64          `json:"completion_reasoning_tokens"`
}

type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Code    any    `json:"code,omitempty"`
}

func solveAssignmentWithLLM(
	ctx context.Context,
	assignment Assignment,
	options AgentRunOptions,
) (*AgentResult, error) {
	request, err := ResolveAssignmentAgentRequest(assignment)
	if err != nil {
		return nil, err
	}

	llmOptions, err := normalizeLLMRunOptions(options)
	if err != nil {
		return nil, err
	}

	runCtx, cancel := context.WithTimeout(ctx, llmOptions.Timeout)
	defer cancel()

	startedAt := time.Now()
	output, rawOutput, err := executeLLM(runCtx, request, llmOptions)
	if err != nil {
		return nil, err
	}

	payload, err := ParseAgentPayload(output.Text)
	if err != nil {
		return nil, err
	}

	return &AgentResult{
		Agent:     LLMAgentSpec,
		Model:     llmOptions.Model,
		RawOutput: rawOutput,
		Payload:   payload,
		Usage:     output.Usage,
		Duration:  time.Since(startedAt),
	}, nil
}

func normalizeLLMRunOptions(options AgentRunOptions) (llmRunOptions, error) {
	baseURL := normalizeBaseURL(options.LLMBaseURL)
	if baseURL == "" {
		return llmRunOptions{}, fmt.Errorf("llm backend requires an LLM base URL")
	}
	if err := ValidateBaseURL(baseURL); err != nil {
		return llmRunOptions{}, fmt.Errorf("invalid llm base URL: %w", err)
	}

	model := strings.TrimSpace(options.LLMModel)
	if model == "" {
		return llmRunOptions{}, fmt.Errorf("llm backend requires an LLM model")
	}

	maxTokens := options.LLMMaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultLLMMaxTokens
	}
	temperature := options.LLMTemperature
	if temperature < 0 {
		return llmRunOptions{}, fmt.Errorf("llm temperature must be non-negative")
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultAgentTimeout
	}

	return llmRunOptions{
		BaseURL:     baseURL,
		Model:       model,
		MaxTokens:   maxTokens,
		Temperature: temperature,
		Timeout:     timeout,
	}, nil
}

func executeLLM(
	ctx context.Context,
	request *AssignmentAgentRequest,
	options llmRunOptions,
) (*structuredAgentOutput, string, error) {
	endpoint, err := openAIChatCompletionsURL(options.BaseURL)
	if err != nil {
		return nil, "", err
	}

	httpRequest, err := buildLLMHTTPRequest(ctx, endpoint, request, options)
	if err != nil {
		return nil, "", err
	}
	response, err := http.DefaultClient.Do(httpRequest)
	if err != nil {
		return nil, "", fmt.Errorf("call llm: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	raw, err := readLimitedLLMResponse(response.Body)
	rawOutput := string(raw)
	if err != nil {
		return nil, rawOutput, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, rawOutput, fmt.Errorf("llm returned %d: %s", response.StatusCode, strings.TrimSpace(rawOutput))
	}
	output, err := decodeLLMResponse(raw)
	return output, rawOutput, err
}

func buildLLMHTTPRequest(
	ctx context.Context,
	endpoint string,
	request *AssignmentAgentRequest,
	options llmRunOptions,
) (*http.Request, error) {
	temperature := options.Temperature
	body := openAIChatCompletionRequest{
		Model: options.Model,
		Messages: []openAIChatMessage{
			{
				Role:    "system",
				Content: "You solve one SCIENCE@home assignment. Return only the structured response requested by the API response format.",
			},
			{
				Role:    "user",
				Content: agentRequestPrompt(request),
			},
		},
		MaxTokens:      options.MaxTokens,
		Temperature:    &temperature,
		Stream:         false,
		ResponseFormat: llmResponseFormat(request),
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode llm request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("build llm request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	return httpRequest, nil
}

func decodeLLMResponse(raw []byte) (*structuredAgentOutput, error) {
	var decoded openAIChatCompletionResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode llm response: %w", err)
	}
	if decoded.Error != nil {
		return nil, fmt.Errorf("llm returned error: %s", decoded.Error.message())
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("llm response did not include any choices")
	}

	text := strings.TrimSpace(llmMessageText(decoded.Choices[0].Message))
	if text == "" {
		return nil, fmt.Errorf("llm response did not include message content")
	}

	return &structuredAgentOutput{
		Text:  text,
		Usage: parseOpenAIChatUsage(decoded.Usage),
	}, nil
}

func llmResponseFormat(request *AssignmentAgentRequest) any {
	if request != nil && request.ResponseSchema != nil {
		return map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "sah_assignment_payload",
				"strict": true,
				"schema": request.ResponseSchema,
			},
		}
	}
	return map[string]any{"type": "json_object"}
}

func openAIChatCompletionsURL(raw string) (string, error) {
	baseURL := normalizeBaseURL(raw)
	if err := ValidateBaseURL(baseURL); err != nil {
		return "", fmt.Errorf("invalid llm base URL: %w", err)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse llm base URL: %w", err)
	}

	path := strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(path, "/v1/chat/completions"):
		parsed.Path = path
	case strings.HasSuffix(path, "/v1"):
		parsed.Path = path + "/chat/completions"
	default:
		parsed.Path = path + "/v1/chat/completions"
	}
	return parsed.String(), nil
}

func readLimitedLLMResponse(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxLLMResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read llm response: %w", err)
	}
	if len(data) > maxLLMResponseBytes {
		return data[:maxLLMResponseBytes], fmt.Errorf("llm response exceeded %d bytes", maxLLMResponseBytes)
	}
	return data, nil
}

func llmMessageText(message openAIChatResponseMessage) string {
	if text := contentText(message.Content); strings.TrimSpace(text) != "" {
		return text
	}
	return contentText(message.Parsed)
}

func contentText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := contentText(item); strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		if text := strings.TrimSpace(stringValue(typed["text"])); text != "" {
			return text
		}
		if text := strings.TrimSpace(stringValue(typed["content"])); text != "" {
			return text
		}
		return compactJSONText(typed)
	default:
		return compactJSONText(typed)
	}
}

func parseOpenAIChatUsage(usage openAIChatUsage) TokenUsage {
	input := usage.PromptTokens
	output := usage.CompletionTokens
	total := usage.TotalTokens
	if total == 0 {
		total = input + output
	}

	promptDetails := usage.PromptTokensDetails
	if len(promptDetails) == 0 {
		promptDetails = usage.PromptTokenDetails
	}
	completionDetails := usage.CompletionTokensDetails
	if len(completionDetails) == 0 {
		completionDetails = usage.CompletionTokenDetails
	}
	cached := int64Value(promptDetails["cached_tokens"]) + usage.PromptCacheHitTokens
	internal := int64Value(completionDetails["reasoning_tokens"]) + usage.CompletionReasoningTokens

	return TokenUsage{
		Available:      input > 0 || output > 0 || total > 0 || cached > 0 || internal > 0,
		InputTokens:    input,
		OutputTokens:   output,
		CachedTokens:   cached,
		InternalTokens: internal,
		TotalTokens:    total,
	}
}

func (err openAIError) message() string {
	message := strings.TrimSpace(err.Message)
	if message != "" {
		return message
	}
	if err.Type != "" {
		return err.Type
	}
	if err.Code != nil {
		return fmt.Sprint(err.Code)
	}
	return "unknown error"
}
