package sah

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
)

type LoginOptions struct {
	BaseURL     string
	Output      io.Writer
	OpenBrowser bool
}

func Login(ctx context.Context, options LoginOptions) (*CLIExchangeResponse, error) {
	baseURL := normalizeBaseURL(options.BaseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for auth callback: %w", err)
	}
	defer listener.Close()

	verifier, err := randomToken(32)
	if err != nil {
		return nil, fmt.Errorf("generate verifier: %w", err)
	}
	state, err := randomToken(32)
	if err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}

	redirectURI := fmt.Sprintf("http://%s/callback", listener.Addr().String())
	authURL, err := buildAuthorizeURL(baseURL, state, redirectURI, challengeForVerifier(verifier))
	if err != nil {
		return nil, err
	}

	fmt.Fprintf(options.Output, "Open this link to authenticate:\n%s\n\n", authURL)
	if options.OpenBrowser {
		if err := openBrowser(authURL); err != nil {
			fmt.Fprintf(options.Output, "Could not open a browser automatically: %v\n", err)
		} else {
			fmt.Fprintln(options.Output, "Opened your browser for SCIENCE@home login.")
		}
	}

	callbacks := make(chan callbackResult, 1)
	server := &http.Server{
		Handler: buildCallbackMux(state, callbacks),
	}
	defer server.Shutdown(context.Background())

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			select {
			case callbacks <- callbackResult{Err: err}:
			default:
			}
		}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-callbacks:
		if result.Err != nil {
			return nil, result.Err
		}
		return ExchangeCLIAuthCode(ctx, baseURL, result.Code, verifier)
	}
}

type callbackResult struct {
	Code string
	Err  error
}

func buildAuthorizeURL(baseURL, state, redirectURI, challenge string) (string, error) {
	endpoint, err := url.Parse(baseURL + "/cli/authorize")
	if err != nil {
		return "", fmt.Errorf("build authorize url: %w", err)
	}
	query := endpoint.Query()
	query.Set("state", state)
	query.Set("redirect_uri", redirectURI)
	query.Set("challenge", challenge)
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func buildCallbackMux(expectedState string, callbacks chan<- callbackResult) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(writer http.ResponseWriter, request *http.Request) {
		state := request.URL.Query().Get("state")
		code := request.URL.Query().Get("code")
		if state == "" || code == "" {
			writeCallbackPage(writer, "SCIENCE@home login failed", "Missing callback parameters.")
			callbacks <- callbackResult{Err: fmt.Errorf("callback was missing code or state")}
			return
		}
		if state != expectedState {
			writeCallbackPage(writer, "SCIENCE@home login failed", "State mismatch.")
			callbacks <- callbackResult{Err: fmt.Errorf("callback state mismatch")}
			return
		}

		writeCallbackPage(writer, "SCIENCE@home login complete", "You can close this tab and return to the terminal.")
		callbacks <- callbackResult{Code: code}
	})
	return mux
}

func writeCallbackPage(writer http.ResponseWriter, title string, body string) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(
		writer,
		"<!doctype html><html><head><meta charset=\"utf-8\"><title>%s</title></head><body><h1>%s</h1><p>%s</p></body></html>",
		html.EscapeString(title),
		html.EscapeString(title),
		html.EscapeString(body),
	)
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func challengeForVerifier(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func openBrowser(rawURL string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", rawURL)
	case "linux":
		command = exec.Command("xdg-open", rawURL)
	default:
		return fmt.Errorf("automatic browser open is unsupported on %s", runtime.GOOS)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
