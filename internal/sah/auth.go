package sah

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const defaultOAuthScope = "user:api offline_access"

type LoginOptions struct {
	BaseURL     string
	Paths       Paths
	Output      io.Writer
	OpenBrowser bool
}

func Login(ctx context.Context, options LoginOptions) (*OAuthTokenResponse, error) {
	baseURL := resolveLoginBaseURL(options.BaseURL)
	clientID := DefaultOAuthClientID
	authorization, err := StartOAuthDeviceAuthorizationWithPaths(
		ctx,
		options.Paths,
		baseURL,
		clientID,
		defaultOAuthScope,
	)
	if err != nil {
		return nil, err
	}
	announceDeviceAuthorization(options.Output, *authorization)
	return awaitDeviceAuthorization(ctx, options.Paths, baseURL, clientID, *authorization)
}

func announceDeviceAuthorization(output io.Writer, authorization OAuthDeviceAuthorizationResponse) {
	if output == nil {
		return
	}
	verificationURL := strings.TrimSpace(authorization.VerificationURIComplete)
	if verificationURL == "" {
		verificationURL = strings.TrimSpace(authorization.VerificationURI)
	}
	_, _ = fmt.Fprintln(output, "SCIENCE@home sign-in")
	_, _ = fmt.Fprintln(output)
	_, _ = fmt.Fprintf(output, "Verification URL: %s\n", verificationURL)
	_, _ = fmt.Fprintf(output, "Code: %s\n\n", authorization.UserCode)
	_, _ = fmt.Fprintln(output, "Open the page above, sign in, and enter the code. The CLI will finish automatically.")
}

func awaitDeviceAuthorization(
	ctx context.Context,
	paths Paths,
	baseURL string,
	clientID string,
	authorization OAuthDeviceAuthorizationResponse,
) (*OAuthTokenResponse, error) {
	interval := time.Duration(authorization.Interval)
	if interval <= 0 {
		interval = 5
	}
	deadline := time.Now().Add(time.Duration(authorization.ExpiresIn) * time.Second)

	for {
		if !deadline.IsZero() && time.Now().After(deadline) {
			return nil, fmt.Errorf("device authorization expired; run `sah auth login` again")
		}

		response, err := PollOAuthDeviceTokenWithPaths(
			ctx,
			paths,
			baseURL,
			clientID,
			authorization.DeviceCode,
		)
		if err != nil {
			var statusErr *StatusError
			if errors.As(err, &statusErr) {
				switch strings.TrimSpace(statusErr.ErrorCode) {
				case "authorization_pending":
					if err := sleepWithContext(ctx, interval*time.Second); err != nil {
						return nil, err
					}
					continue
				case "slow_down":
					interval += 5
					if err := sleepWithContext(ctx, interval*time.Second); err != nil {
						return nil, err
					}
					continue
				case "access_denied":
					return nil, fmt.Errorf("device authorization denied")
				case "expired_token":
					return nil, fmt.Errorf("device authorization expired; run `sah auth login` again")
				}
			}
			return nil, err
		}
		if strings.TrimSpace(response.AccessToken) == "" {
			return nil, fmt.Errorf("device authorization returned no access token")
		}
		return response, nil
	}
}

func resolveLoginBaseURL(rawBaseURL string) string {
	baseURL := normalizeBaseURL(rawBaseURL)
	if baseURL == "" {
		return DefaultBaseURL
	}
	return baseURL
}
