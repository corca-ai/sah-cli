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
	"strconv"
	"strings"
	"sync"
	"time"
)

type Client struct {
	baseURL       string
	paths         Paths
	apiKey        string
	accessToken   string
	refreshToken  string
	tokenType     string
	tokenExpiry   time.Time
	oauthClientID string
	saveTokens    func(accessToken, refreshToken, tokenType string, expiry time.Time) error
	httpClient    *http.Client
	authMu        sync.Mutex
}

type requestOptions struct {
	WorkerContract bool
}

const maxRetryAfterDelay = 10 * time.Second

type StatusError struct {
	StatusCode int
	ErrorCode  string
	Message    string
	Body       string
}

func (err *StatusError) Error() string {
	if err.Message != "" {
		return fmt.Sprintf("api returned %d: %s", err.StatusCode, err.Message)
	}
	if err.ErrorCode != "" {
		return fmt.Sprintf("api returned %d: %s", err.StatusCode, err.ErrorCode)
	}
	return fmt.Sprintf("api returned %d", err.StatusCode)
}

func NewClient(baseURL, apiKey string) *Client {
	return newClientWithHTTPClient(Paths{}, baseURL, apiKey, newHTTPClient(nil))
}

func NewCachedClient(paths Paths, baseURL, apiKey string) *Client {
	return newClientWithHTTPClient(paths, baseURL, apiKey, newHTTPClient(buildCachedTransport(paths)))
}

func NewConfigClient(paths Paths, config *Config) *Client {
	return newConfigClient(paths, config, false)
}

func NewCachedConfigClient(paths Paths, config *Config) *Client {
	return newConfigClient(paths, config, true)
}

func newClientWithHTTPClient(paths Paths, baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = newHTTPClient(nil)
	}
	return &Client{
		baseURL:    normalizeBaseURL(baseURL),
		paths:      paths,
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: httpClient,
	}
}

func newConfigClient(paths Paths, config *Config, cached bool) *Client {
	if config == nil {
		if cached {
			return NewCachedClient(paths, DefaultBaseURL, "")
		}
		return NewClient(DefaultBaseURL, "")
	}

	var httpClient *http.Client
	if cached {
		httpClient = newHTTPClient(buildCachedTransport(paths))
	} else {
		httpClient = newHTTPClient(nil)
	}

	client := &Client{
		baseURL:       normalizeBaseURL(config.BaseURL),
		paths:         paths,
		apiKey:        strings.TrimSpace(config.APIKey),
		accessToken:   strings.TrimSpace(config.AccessToken),
		refreshToken:  strings.TrimSpace(config.RefreshToken),
		tokenType:     strings.TrimSpace(config.TokenType),
		tokenExpiry:   config.ParsedTokenExpiry(),
		oauthClientID: strings.TrimSpace(config.OAuthClientID),
		httpClient:    httpClient,
	}
	if client.oauthClientID == "" {
		client.oauthClientID = DefaultOAuthClientID
	}
	if client.tokenType == "" && client.accessToken != "" {
		client.tokenType = "Bearer"
	}
	if strings.TrimSpace(paths.ConfigFile) != "" {
		client.saveTokens = func(accessToken, refreshToken, tokenType string, expiry time.Time) error {
			config.AccessToken = strings.TrimSpace(accessToken)
			config.RefreshToken = strings.TrimSpace(refreshToken)
			config.TokenType = strings.TrimSpace(tokenType)
			if config.TokenType == "" && config.AccessToken != "" {
				config.TokenType = "Bearer"
			}
			if expiry.IsZero() {
				config.TokenExpiry = ""
			} else {
				config.TokenExpiry = expiry.UTC().Format(time.RFC3339)
			}
			return SaveConfig(paths, *config)
		}
	}
	return client
}

func (client *Client) GetTask(ctx context.Context, taskType string) (*Assignment, error) {
	return client.ClaimAssignment(ctx, taskType)
}

func (client *Client) ClaimAssignment(ctx context.Context, taskType string) (*Assignment, error) {
	path := "/s@h/assignments"
	body := map[string]any{}
	if encodedTaskType := strings.TrimSpace(taskType); encodedTaskType != "" {
		body["task_type"] = encodedTaskType
	}

	var assignment Assignment
	headers, err := client.doJSONWithHeaders(
		ctx,
		http.MethodPost,
		path,
		body,
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
	path := fmt.Sprintf("/s@h/assignments/%d", assignmentID)
	return client.doWorkerJSON(ctx, http.MethodDelete, path, nil, nil)
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

func (client *Client) GetServiceDocument(ctx context.Context) (*ServiceDocument, error) {
	var response ServiceDocument
	if err := client.doJSON(ctx, http.MethodGet, "/s@h", nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (client *Client) GetNavigation(ctx context.Context, request NavigationRequest) (*NavigationResponse, error) {
	var response NavigationResponse
	if err := client.doJSON(ctx, http.MethodPost, "/s@h/navigation", request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func GetOAuthAuthorizationServerMetadata(
	ctx context.Context,
	baseURL string,
) (*OAuthAuthorizationServerMetadata, error) {
	return NewClient(baseURL, "").getOAuthAuthorizationServerMetadata(ctx)
}

func StartOAuthDeviceAuthorization(
	ctx context.Context,
	baseURL string,
	clientID string,
	scope string,
) (*OAuthDeviceAuthorizationResponse, error) {
	return startOAuthDeviceAuthorizationWithClient(
		ctx,
		NewClient(baseURL, ""),
		clientID,
		scope,
	)
}

func StartOAuthDeviceAuthorizationWithPaths(
	ctx context.Context,
	paths Paths,
	baseURL string,
	clientID string,
	scope string,
) (*OAuthDeviceAuthorizationResponse, error) {
	return startOAuthDeviceAuthorizationWithClient(
		ctx,
		NewCachedClient(paths, baseURL, ""),
		clientID,
		scope,
	)
}

func startOAuthDeviceAuthorizationWithClient(
	ctx context.Context,
	client *Client,
	clientID string,
	scope string,
) (*OAuthDeviceAuthorizationResponse, error) {
	metadata, err := client.getOAuthAuthorizationServerMetadata(ctx)
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	form.Set("client_id", strings.TrimSpace(clientID))
	if strings.TrimSpace(scope) != "" {
		form.Set("scope", strings.TrimSpace(scope))
	}

	var response OAuthDeviceAuthorizationResponse
	if err := client.doForm(ctx, http.MethodPost, metadata.DeviceAuthorizationEndpoint, form, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func PollOAuthDeviceToken(
	ctx context.Context,
	baseURL string,
	clientID string,
	deviceCode string,
) (*OAuthTokenResponse, error) {
	return pollOAuthDeviceTokenWithClient(
		ctx,
		NewClient(baseURL, ""),
		clientID,
		deviceCode,
	)
}

func PollOAuthDeviceTokenWithPaths(
	ctx context.Context,
	paths Paths,
	baseURL string,
	clientID string,
	deviceCode string,
) (*OAuthTokenResponse, error) {
	return pollOAuthDeviceTokenWithClient(
		ctx,
		NewCachedClient(paths, baseURL, ""),
		clientID,
		deviceCode,
	)
}

func pollOAuthDeviceTokenWithClient(
	ctx context.Context,
	client *Client,
	clientID string,
	deviceCode string,
) (*OAuthTokenResponse, error) {
	metadata, err := client.getOAuthAuthorizationServerMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return postOAuthTokenWithClient(ctx, client, metadata.TokenEndpoint, url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {strings.TrimSpace(deviceCode)},
		"client_id":   {strings.TrimSpace(clientID)},
	})
}

func RefreshOAuthToken(
	ctx context.Context,
	baseURL string,
	clientID string,
	refreshToken string,
) (*OAuthTokenResponse, error) {
	return refreshOAuthTokenWithClient(
		ctx,
		NewClient(baseURL, ""),
		clientID,
		refreshToken,
	)
}

func refreshOAuthTokenWithClient(
	ctx context.Context,
	client *Client,
	clientID string,
	refreshToken string,
) (*OAuthTokenResponse, error) {
	metadata, err := client.getOAuthAuthorizationServerMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return postOAuthTokenWithClient(ctx, client, metadata.TokenEndpoint, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {strings.TrimSpace(refreshToken)},
		"client_id":     {strings.TrimSpace(clientID)},
	})
}

func postOAuthTokenWithClient(
	ctx context.Context,
	client *Client,
	tokenEndpoint string,
	form url.Values,
) (*OAuthTokenResponse, error) {
	var response OAuthTokenResponse
	if err := client.doForm(ctx, http.MethodPost, tokenEndpoint, form, &response); err != nil {
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

func StartDeviceAuthorization(
	ctx context.Context,
	baseURL string,
) (*DeviceAuthorizationResponse, error) {
	client := NewClient(baseURL, "")
	var response DeviceAuthorizationResponse
	if err := client.doJSON(ctx, http.MethodPost, "/api/cli/device-authorizations", map[string]any{}, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func PollDeviceAuthorization(
	ctx context.Context,
	baseURL string,
	deviceCode string,
) (*DeviceTokenResponse, error) {
	client := NewClient(baseURL, "")
	var response DeviceTokenResponse
	if err := client.doJSON(
		ctx,
		http.MethodPost,
		"/api/cli/device-token",
		map[string]string{"device_code": strings.TrimSpace(deviceCode)},
		&response,
	); err != nil {
		var statusErr *StatusError
		if errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusAccepted {
			var pending DeviceTokenResponse
			if strings.TrimSpace(statusErr.Body) != "" && json.Unmarshal([]byte(statusErr.Body), &pending) == nil {
				pending.Status = "pending"
				return &pending, nil
			}
		}
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
	if err := client.ensureAccessToken(ctx, false); err != nil {
		return nil, err
	}

	request, err := client.newJSONRequest(ctx, method, path, body, options)
	if err != nil {
		return nil, err
	}

	response, err := client.sendWithRetryAfter(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("perform request: %w", err)
	}
	if response.StatusCode == http.StatusUnauthorized && client.canRefreshAccessToken() {
		_ = response.Body.Close()
		if err := client.ensureAccessToken(ctx, true); err != nil {
			return nil, err
		}

		retryRequest, err := client.newJSONRequest(ctx, method, path, body, options)
		if err != nil {
			return nil, err
		}
		response, err = client.sendWithRetryAfter(ctx, retryRequest)
		if err != nil {
			return nil, fmt.Errorf("perform request: %w", err)
		}
	} else {
		retriedResponse, retried, err := client.retryWithAPIKeyFallback(
			ctx,
			request,
			response,
			method,
			path,
			body,
			options,
		)
		if err != nil {
			return nil, err
		}
		if retried {
			response = retriedResponse
		}
	}
	defer func() {
		_ = response.Body.Close()
	}()

	headers := response.Header.Clone()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, decodeStatusError(response)
	}
	return headers, decodeJSONResponse(response, out)
}

func (client *Client) retryWithAPIKeyFallback(
	ctx context.Context,
	request *http.Request,
	response *http.Response,
	method string,
	path string,
	body any,
	options requestOptions,
) (*http.Response, bool, error) {
	if response == nil || request == nil {
		return response, false, nil
	}
	if response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusForbidden {
		return response, false, nil
	}
	if request.Header.Get("Authorization") == "" || client.apiKey == "" {
		return response, false, nil
	}

	_ = response.Body.Close()
	client.clearOAuthTokens()
	if client.saveTokens != nil {
		if err := client.saveTokens("", "", "", time.Time{}); err != nil {
			return nil, false, err
		}
	}

	retryRequest, err := client.newJSONRequest(ctx, method, path, body, options)
	if err != nil {
		return nil, false, err
	}
	retriedResponse, err := client.sendWithRetryAfter(ctx, retryRequest)
	if err != nil {
		return nil, false, fmt.Errorf("perform request: %w", err)
	}
	return retriedResponse, true, nil
}

func shouldRetryAfter(response *http.Response, method string) bool {
	if response == nil {
		return false
	}
	if !isRetryableMethod(method) {
		return false
	}
	switch response.StatusCode {
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return strings.TrimSpace(response.Header.Get("Retry-After")) != ""
	default:
		return false
	}
}

func isRetryableMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func retryAfterDelay(raw string, now time.Time) (time.Duration, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, false
	}

	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}

	retryAt, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}

	delay := time.Until(retryAt)
	if !now.IsZero() {
		delay = retryAt.Sub(now)
	}
	if delay < 0 {
		return 0, true
	}
	return delay, true
}

func (client *Client) newJSONRequest(
	ctx context.Context,
	method string,
	path string,
	body any,
	options requestOptions,
) (*http.Request, error) {
	endpoint, err := client.resolveEndpoint(path)
	if err != nil {
		return nil, fmt.Errorf("build request url: %w", err)
	}

	requestBody, err := marshalJSONBody(body)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), requestBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	client.applyJSONRequestHeaders(request, body != nil, options)
	return request, nil
}

func (client *Client) doForm(
	ctx context.Context,
	method string,
	path string,
	form url.Values,
	out any,
) error {
	endpoint, err := client.resolveEndpoint(path)
	if err != nil {
		return fmt.Errorf("build request url: %w", err)
	}
	body := strings.NewReader(form.Encode())
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	if origin := httpURLOrigin(endpoint); origin != "" {
		request.Header.Set("Origin", origin)
	}
	if version := CLIVersion(); version != "" {
		request.Header.Set("X-SAH-CLI-Version", version)
		request.Header.Set("User-Agent", "sah/"+version)
	}

	response, err := client.sendWithRetryAfter(ctx, request)
	if err != nil {
		return fmt.Errorf("perform request: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeStatusError(response)
	}
	return decodeJSONResponse(response, out)
}

func httpURLOrigin(endpoint *url.URL) string {
	if endpoint == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(endpoint.Scheme)) {
	case "http", "https":
		return endpoint.Scheme + "://" + endpoint.Host
	default:
		return ""
	}
}

func marshalJSONBody(body any) (io.Reader, error) {
	if body == nil {
		return nil, nil
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode request body: %w", err)
	}
	return bytes.NewReader(payload), nil
}

func (client *Client) applyJSONRequestHeaders(request *http.Request, hasBody bool, options requestOptions) {
	if hasBody {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Accept", "application/json")
	if authHeader := client.authorizationHeader(); authHeader != "" {
		request.Header.Set("Authorization", authHeader)
	} else if client.apiKey != "" {
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
}

func (client *Client) authorizationHeader() string {
	accessToken := strings.TrimSpace(client.accessToken)
	if accessToken == "" {
		return ""
	}
	tokenType := strings.TrimSpace(client.tokenType)
	if tokenType == "" {
		tokenType = "Bearer"
	}
	return tokenType + " " + accessToken
}

func (client *Client) canRefreshAccessToken() bool {
	return strings.TrimSpace(client.refreshToken) != ""
}

func (client *Client) clearOAuthTokens() {
	client.accessToken = ""
	client.refreshToken = ""
	client.tokenType = ""
	client.tokenExpiry = time.Time{}
}

func (client *Client) getOAuthAuthorizationServerMetadata(
	ctx context.Context,
) (*OAuthAuthorizationServerMetadata, error) {
	metadataClient := NewClient(client.baseURL, "")
	if strings.TrimSpace(client.paths.HTTPCacheDir) != "" {
		metadataClient = NewCachedClient(client.paths, client.baseURL, "")
	}

	var response OAuthAuthorizationServerMetadata
	if err := metadataClient.doJSON(
		ctx,
		http.MethodGet,
		"/.well-known/oauth-authorization-server",
		nil,
		&response,
	); err != nil {
		return nil, err
	}
	return &response, nil
}

func (client *Client) ensureAccessToken(ctx context.Context, force bool) error {
	if !client.canRefreshAccessToken() {
		return nil
	}

	client.authMu.Lock()
	defer client.authMu.Unlock()

	if !force && strings.TrimSpace(client.accessToken) != "" {
		expiry := client.tokenExpiry
		if expiry.IsZero() || time.Until(expiry) > 30*time.Second {
			return nil
		}
	}

	clientID := strings.TrimSpace(client.oauthClientID)
	if clientID == "" {
		clientID = DefaultOAuthClientID
	}
	response, err := refreshOAuthTokenWithClient(ctx, client, clientID, client.refreshToken)
	if err != nil {
		if client.apiKey != "" && IsAuthenticationFailure(err) {
			client.clearOAuthTokens()
			if client.saveTokens != nil {
				if saveErr := client.saveTokens("", "", "", time.Time{}); saveErr != nil {
					return saveErr
				}
			}
			return nil
		}
		return err
	}
	client.updateTokens(*response)
	if client.saveTokens != nil {
		if err := client.saveTokens(client.accessToken, client.refreshToken, client.tokenType, client.tokenExpiry); err != nil {
			return err
		}
	}
	return nil
}

func (client *Client) updateTokens(response OAuthTokenResponse) {
	if accessToken := strings.TrimSpace(response.AccessToken); accessToken != "" {
		client.accessToken = accessToken
	}
	if refreshToken := strings.TrimSpace(response.RefreshToken); refreshToken != "" {
		client.refreshToken = refreshToken
	}
	if tokenType := strings.TrimSpace(response.TokenType); tokenType != "" {
		client.tokenType = tokenType
	} else if client.tokenType == "" && client.accessToken != "" {
		client.tokenType = "Bearer"
	}
	if response.ExpiresIn > 0 {
		client.tokenExpiry = time.Now().UTC().Add(time.Duration(response.ExpiresIn) * time.Second)
	}
}

func (client *Client) sendWithRetryAfter(ctx context.Context, request *http.Request) (*http.Response, error) {
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	if shouldRetryAfter(response, request.Method) {
		retryDelay, ok := retryAfterDelay(response.Header.Get("Retry-After"), time.Now())
		if ok && retryDelay <= maxRetryAfterDelay {
			_ = response.Body.Close()
			if retryDelay > 0 {
				if err := sleepWithContext(ctx, retryDelay); err != nil {
					return nil, err
				}
			}
			return client.httpClient.Do(request.Clone(ctx))
		}
	}
	return response, nil
}

func decodeJSONResponse(response *http.Response, out any) error {
	if out == nil {
		return nil
	}
	if response.StatusCode == http.StatusNoContent {
		return &StatusError{
			StatusCode: response.StatusCode,
			Message:    http.StatusText(http.StatusNoContent),
		}
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
		Detail           string `json:"detail"`
		Message          string `json:"message"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if len(body) > 0 && json.Unmarshal(body, &detail) == nil {
		message := strings.TrimSpace(detail.Detail)
		if message == "" {
			message = strings.TrimSpace(detail.Message)
		}
		if message == "" {
			message = strings.TrimSpace(detail.ErrorDescription)
		}
		return &StatusError{
			StatusCode: response.StatusCode,
			ErrorCode:  strings.TrimSpace(detail.Error),
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

func IsAuthenticationFailure(err error) bool {
	if err == nil {
		return false
	}

	var statusErr *StatusError
	if errors.As(err, &statusErr) {
		switch statusErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return true
		case http.StatusBadRequest:
			errorCode := strings.ToLower(strings.TrimSpace(statusErr.ErrorCode))
			if errorCode == "invalid_grant" || errorCode == "invalid_token" {
				return true
			}

			combined := strings.ToLower(strings.TrimSpace(statusErr.Message + " " + statusErr.Body))
			if strings.Contains(combined, "invalid refresh token") ||
				strings.Contains(combined, "refresh token") ||
				strings.Contains(combined, "invalid api key") {
				return true
			}
		}
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not authenticated") ||
		strings.Contains(message, "stored credential rejected") ||
		strings.Contains(message, "run `sah auth login` first") ||
		strings.Contains(message, "invalid api key")
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
