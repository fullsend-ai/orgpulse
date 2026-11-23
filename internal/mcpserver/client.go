package mcpserver

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxResponseBody caps how much of an API response is read.
const maxResponseBody = 64 << 20

type apiClient struct {
	baseURL    string
	apiKey     string
	sessionID  string
	httpClient *http.Client
}

func newAPIClient(baseURL, apiKey string) *apiClient {
	b := make([]byte, 16)
	rand.Read(b)
	return &apiClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
		sessionID: "mcp-" + hex.EncodeToString(b),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *apiClient) do(method, path string, body any) (json.RawMessage, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		msg := string(respBody)
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, strings.TrimSpace(msg))
	}

	if len(respBody) == 0 {
		return json.RawMessage(`{"ok":true}`), nil
	}
	return json.RawMessage(respBody), nil
}

func (c *apiClient) get(path string) (json.RawMessage, error) {
	return c.do(http.MethodGet, path, nil)
}

func (c *apiClient) post(path string, body any) (json.RawMessage, error) {
	return c.do(http.MethodPost, path, body)
}

func (c *apiClient) put(path string, body any) (json.RawMessage, error) {
	return c.do(http.MethodPut, path, body)
}

func (c *apiClient) delete(path string) (json.RawMessage, error) {
	return c.do(http.MethodDelete, path, nil)
}
