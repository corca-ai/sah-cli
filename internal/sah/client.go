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

type requestOptions struct {
	WorkerContract bool
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
	headers, err := client.doJSONWithHeaders(
		ctx,
		http.MethodGet,
		path,
		nil,
		&assignment,
		requestOptions{WorkerContract: true},
	)
	if err != nil {
		return nil, err
	}
	// Prefer explicit body links, but also accept HTTP Link headers so the
	// server can evolve the protocol without forcing a CLI release.
	mergeAssignmentLinks(&assignment, headers)
	return &assignment, nil
}

func (client *Client) SubmitContribution(
	ctx context.Context,
	request SubmitContributionRequest,
) (*SubmitContributionResponse, error) {
	var response SubmitContributionResponse
	if err := client.doWorkerJSON(ctx, http.MethodPost, "/s@h/contributions", request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (client *Client) SubmitAssignment(
	ctx context.Context,
	assignment Assignment,
	payload map[string]any,
) (*SubmitContributionResponse, error) {
	if href := strings.TrimSpace(assignment.Links.Submit.Href); href != "" {
		// Assignment-scoped submission already identifies the task on the server,
		// so newer protocol versions only need the payload object here.
		request := map[string]any{
			"payload": payload,
		}
		var response SubmitContributionResponse
		if err := client.doWorkerJSON(
			ctx,
			linkMethodOrDefault(assignment.Links.Submit.Method, http.MethodPost),
			href,
			request,
			&response,
		); err != nil {
			return nil, err
		}
		return &response, nil
	}
	return client.SubmitContribution(ctx, SubmitContributionRequest{
		AssignmentID: assignment.AssignmentID,
		TaskType:     assignment.TaskType,
		Payload:      payload,
	})
}

func (client *Client) ReleaseAssignment(ctx context.Context, assignmentID int64) error {
	path := fmt.Sprintf("/s@h/assignments/%d/release", assignmentID)
	return client.doWorkerJSON(ctx, http.MethodPost, path, nil, nil)
}

func (client *Client) ReleaseOpenAssignment(ctx context.Context, assignment Assignment) error {
	if href := strings.TrimSpace(assignment.Links.Release.Href); href != "" {
		return client.doWorkerJSON(
			ctx,
			linkMethodOrDefault(assignment.Links.Release.Method, http.MethodPost),
			href,
			nil,
			nil,
		)
	}
	return client.ReleaseAssignment(ctx, assignment.AssignmentID)
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

func (client *Client) GetClientRelease(ctx context.Context) (*ClientReleaseResponse, error) {
	var response ClientReleaseResponse
	if err := client.doJSON(ctx, http.MethodGet, "/s@h/client-release", nil, &response); err != nil {
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
	_, err := client.doJSONWithHeaders(ctx, method, path, body, out, requestOptions{})
	return err
}

func (client *Client) doWorkerJSON(
	ctx context.Context,
	method string,
	path string,
	body any,
	out any,
) error {
	_, err := client.doJSONWithHeaders(
		ctx,
		method,
		path,
		body,
		out,
		requestOptions{WorkerContract: true},
	)
	return err
}

func (client *Client) doJSONWithHeaders(
	ctx context.Context,
	method string,
	path string,
	body any,
	out any,
	options requestOptions,
) (http.Header, error) {
	endpoint, err := client.resolveEndpoint(path)
	if err != nil {
		return nil, fmt.Errorf("build request url: %w", err)
	}

	var requestBody io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		requestBody = bytes.NewReader(payload)
	}

	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), requestBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Accept", "application/json")
	if client.apiKey != "" {
		request.Header.Set("X-API-Key", client.apiKey)
	}
	if options.WorkerContract {
		request.Header.Set(TaskProtocolHeader, SupportedTaskProtocol)
		if capabilities := SupportedClientCapabilitiesHeaderValue(); capabilities != "" {
			request.Header.Set(ClientCapabilitiesHeader, capabilities)
		}
	}
	if version := CLIVersion(); version != "" {
		request.Header.Set("X-SAH-CLI-Version", version)
		request.Header.Set("User-Agent", "sah/"+version)
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("perform request: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	headers := response.Header.Clone()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, decodeStatusError(response)
	}
	if out == nil {
		return headers, nil
	}
	if response.StatusCode == http.StatusNoContent {
		return headers, &StatusError{
			StatusCode: response.StatusCode,
			Message:    http.StatusText(http.StatusNoContent),
		}
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return headers, nil
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

func (client *Client) resolveEndpoint(path string) (*url.URL, error) {
	baseURL, err := url.Parse(client.baseURL)
	if err != nil {
		return nil, err
	}
	ref, err := url.Parse(strings.TrimSpace(path))
	if err != nil {
		return nil, err
	}
	return baseURL.ResolveReference(ref), nil
}

func mergeAssignmentLinks(assignment *Assignment, headers http.Header) {
	if assignment == nil {
		return
	}

	// Header-derived links only fill gaps. Body links remain authoritative when
	// both representations are present.
	for _, raw := range headers.Values("Link") {
		for _, link := range parseLinkHeader(raw) {
			switch link.rel {
			case "submit":
				if strings.TrimSpace(assignment.Links.Submit.Href) == "" {
					assignment.Links.Submit.Href = link.href
				}
			case "release":
				if strings.TrimSpace(assignment.Links.Release.Href) == "" {
					assignment.Links.Release.Href = link.href
				}
			}
		}
	}
}

type parsedLink struct {
	rel  string
	href string
}

func parseLinkHeader(raw string) []parsedLink {
	parts := strings.Split(raw, ",")
	links := make([]parsedLink, 0, len(parts))
	for _, part := range parts {
		segment := strings.TrimSpace(part)
		if !strings.HasPrefix(segment, "<") {
			continue
		}
		end := strings.Index(segment, ">")
		if end <= 1 {
			continue
		}

		href := strings.TrimSpace(segment[1:end])
		if href == "" {
			continue
		}

		var rel string
		for _, param := range strings.Split(segment[end+1:], ";") {
			key, value, ok := strings.Cut(strings.TrimSpace(param), "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(key), "rel") {
				continue
			}
			for _, candidate := range strings.Fields(strings.Trim(value, `"'`)) {
				if candidate == "submit" || candidate == "release" {
					rel = candidate
					break
				}
			}
			if rel != "" {
				break
			}
		}
		if rel == "" {
			continue
		}
		links = append(links, parsedLink{rel: rel, href: href})
	}
	return links
}

func linkMethodOrDefault(method string, fallback string) string {
	normalized := strings.ToUpper(strings.TrimSpace(method))
	if normalized == "" {
		return fallback
	}
	return normalized
}

func statusMessage(err error) string {
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		return ""
	}
	return strings.TrimSpace(statusErr.Message)
}
