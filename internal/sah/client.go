package sah

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type StatusError struct {
	StatusCode int
	Message    string
	Body       string
}

func (err *StatusError) Error() string {
	if err.Message != "" {
		return fmt.Sprintf("api returned %d: %s", err.StatusCode, err.Message)
	}
	return fmt.Sprintf("api returned %d", err.StatusCode)
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: normalizeBaseURL(baseURL),
		apiKey:  strings.TrimSpace(apiKey),
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

func (client *Client) GetTask(ctx context.Context, taskType string) (*Assignment, error) {
	query := url.Values{}
	if strings.TrimSpace(taskType) != "" {
		query.Set("task_type", taskType)
	}

	path := "/s@h/tasks"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var assignment Assignment
	if err := client.doJSON(ctx, http.MethodGet, path, nil, &assignment); err != nil {
		return nil, err
	}
	return &assignment, nil
}

func (client *Client) SubmitContribution(
	ctx context.Context,
	request SubmitContributionRequest,
) (*SubmitContributionResponse, error) {
	var response SubmitContributionResponse
	if err := client.doJSON(ctx, http.MethodPost, "/s@h/contributions", request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (client *Client) GetMe(ctx context.Context) (*MeResponse, error) {
	var response MeResponse
	if err := client.doJSON(ctx, http.MethodGet, "/s@h/me", nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (client *Client) GetContributions(ctx context.Context, limit int) (*ContributionsResponse, error) {
	path := fmt.Sprintf("/s@h/contributions?limit=%d", limit)
	var response ContributionsResponse
	if err := client.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (client *Client) GetLeaderboard(ctx context.Context) (*LeaderboardResponse, error) {
	var response LeaderboardResponse
	if err := client.doJSON(ctx, http.MethodGet, "/s@h/leaderboard", nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func ExchangeCLIAuthCode(
	ctx context.Context,
	baseURL string,
	code string,
	verifier string,
) (*CLIExchangeResponse, error) {
	client := NewClient(baseURL, "")
	request := map[string]string{
		"code":     code,
		"verifier": verifier,
	}
	var response CLIExchangeResponse
	if err := client.doJSON(ctx, http.MethodPost, "/api/cli/exchange", request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (client *Client) doJSON(
	ctx context.Context,
	method string,
	path string,
	body any,
	out any,
) error {
	endpoint, err := url.Parse(client.baseURL + path)
	if err != nil {
		return fmt.Errorf("build request url: %w", err)
	}

	var requestBody io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
		requestBody = bytes.NewReader(payload)
	}

	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), requestBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if client.apiKey != "" {
		request.Header.Set("X-API-Key", client.apiKey)
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("perform request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeStatusError(response)
	}
	if out == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func decodeStatusError(response *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(response.Body, 16*1024))
	if err != nil {
		return fmt.Errorf("read error response: %w", err)
	}

	var detail struct {
		Detail  string `json:"detail"`
		Message string `json:"message"`
	}
	if len(body) > 0 && json.Unmarshal(body, &detail) == nil {
		message := strings.TrimSpace(detail.Detail)
		if message == "" {
			message = strings.TrimSpace(detail.Message)
		}
		return &StatusError{
			StatusCode: response.StatusCode,
			Message:    message,
			Body:       strings.TrimSpace(string(body)),
		}
	}
	return &StatusError{
		StatusCode: response.StatusCode,
		Message:    strings.TrimSpace(string(body)),
		Body:       strings.TrimSpace(string(body)),
	}
}

func IsStatus(err error, statusCode int) bool {
	var statusErr *StatusError
	return errors.As(err, &statusErr) && statusErr.StatusCode == statusCode
}
