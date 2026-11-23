package server

import (
	"bytes"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/fullsend-ai/orgpulse/internal/logging"
)

var validWorkflowName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func workflowProductionProxyHandler(backendURL string, allow func(*http.Request, []byte) error) http.HandlerFunc {
	baseURL := strings.TrimRight(backendURL, "/")
	client := &http.Client{Timeout: 30 * time.Second}

	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if !validWorkflowName.MatchString(name) {
			writeError(w, http.StatusBadRequest, "invalid workflow name")
			return
		}
		if allow == nil {
			writeError(w, http.StatusForbidden, "access denied")
			return
		}

		body, err := readLimited(r.Body, maxMetricBody)
		if err != nil {
			writeMetricError(w, err)
			return
		}
		if err := allow(r, body); err != nil {
			writeMetricError(w, err)
			return
		}

		upstream := baseURL + "/workflows/" + name
		if r.URL.RawQuery != "" {
			upstream += "?" + r.URL.RawQuery
		}

		proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstream, bytes.NewReader(body)) // #nosec G704 -- upstream is a fixed base URL + validated name
		if err != nil {
			logger := logging.FromContext(r.Context())
			logger.Error("workflow proxy: failed to create request", "err", err)
			writeError(w, http.StatusInternalServerError, "proxy error")
			return
		}
		proxyReq.ContentLength = int64(len(body))
		proxyReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))

		reqID := logging.RequestIDFromContext(r.Context())
		if reqID == "" {
			reqID = logging.RequestID(r.Context())
		}
		if reqID != "" {
			proxyReq.Header.Set("X-Request-ID", reqID)
		}

		if auth := r.Header.Get("Authorization"); auth != "" {
			proxyReq.Header.Set("Authorization", auth)
		}

		resp, err := client.Do(proxyReq) // #nosec G704 -- see above
		if err != nil {
			logger := logging.FromContext(r.Context())
			logger.Error("workflow proxy: upstream error", "url", upstream, "err", err, "request_id", reqID)
			writeError(w, http.StatusBadGateway, "workflow service unavailable")
			return
		}
		defer resp.Body.Close()

		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		if _, err := io.Copy(w, resp.Body); err != nil {
			logger := logging.FromContext(r.Context())
			logger.Error("workflow proxy: response copy error", "err", err, "request_id", reqID)
		}
		logger := logging.FromContext(r.Context())
		logger.Info("upstream request completed", "status", resp.StatusCode, "request_id", reqID)
	}
}

func readLimited(body io.Reader, limit int) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	data, err := io.ReadAll(io.LimitReader(body, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errMetricBody
	}
	return data, nil
}
