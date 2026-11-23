package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/fullsend-ai/orgpulse/internal/logging"
)

func truncateForLog(b []byte) string {
	const limit = 2048
	if len(b) > limit {
		return string(b[:limit]) + "..."
	}
	return string(b)
}

type AIAssistConfig struct {
	APIKey  string
	Model   string
	BaseURL string
}

func (c AIAssistConfig) Enabled() bool { return c.APIKey != "" }

func aiAssistHandler(cfg AIAssistConfig) http.HandlerFunc {
	client := &http.Client{Timeout: 60 * time.Second}

	return func(w http.ResponseWriter, r *http.Request) {
		if !cfg.Enabled() {
			writeError(w, http.StatusServiceUnavailable, "AI assist not configured")
			return
		}

		var req struct {
			Prompt   string `json:"prompt"`
			Code     string `json:"code"`
			CodeType string `json:"code_type"`
			Workflow any    `json:"workflow"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.Prompt == "" {
			writeError(w, http.StatusBadRequest, "prompt is required")
			return
		}

		systemPrompt := buildAISystemPrompt(req.CodeType)

		wfJSON, _ := json.MarshalIndent(req.Workflow, "", "  ")

		userContent := fmt.Sprintf("## Current workflow definition\n```json\n%s\n```\n\n", string(wfJSON))
		if req.Code != "" {
			userContent += fmt.Sprintf("## Current %s code in this editor\n```\n%s\n```\n\n", req.CodeType, req.Code)
		}
		userContent += "## Request\n" + req.Prompt

		anthropicReq := map[string]any{
			"model":      cfg.Model,
			"max_tokens": 4096,
			"system":     systemPrompt,
			"messages": []map[string]any{
				{"role": "user", "content": userContent},
			},
		}

		body, _ := json.Marshal(anthropicReq)
		apiReq, _ := http.NewRequestWithContext(r.Context(), "POST", cfg.BaseURL+"/v1/messages", bytes.NewReader(body))
		apiReq.Header.Set("Content-Type", "application/json")
		apiReq.Header.Set("x-api-key", cfg.APIKey)
		apiReq.Header.Set("anthropic-version", "2023-06-01")

		logger := logging.FromContext(r.Context())
		resp, err := client.Do(apiReq)
		if err != nil {
			logger.Error("ai assist: request failed", "err", err)
			writeError(w, http.StatusBadGateway, "AI API request failed")
			return
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamBody))
		if resp.StatusCode != 200 {
			logger.Error("ai assist: upstream error", "status", resp.StatusCode, "body", truncateForLog(respBody))
			writeError(w, http.StatusBadGateway, fmt.Sprintf("AI API error %d", resp.StatusCode))
			return
		}

		var anthropicResp struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(respBody, &anthropicResp); err != nil || len(anthropicResp.Content) == 0 {
			writeError(w, http.StatusBadGateway, "unexpected AI API response")
			return
		}

		text := anthropicResp.Content[0].Text
		code := extractCodeBlock(text)

		writeJSON(w, http.StatusOK, map[string]string{"code": code})
	}
}

func aiAnalyticsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			DiagramData json.RawMessage `json:"diagram_data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}

		html := `<div style="padding:12px;color:#6a737d;font-size:13px;line-height:1.5;">` +
			`<p style="margin:0 0 8px;font-weight:600;color:#24292e;">AI Analytics</p>` +
			`<p style="margin:0;font-style:italic;">Not available yet. AI-powered insights for this metric will appear here once configured.</p>` +
			`</div>`

		writeJSON(w, http.StatusOK, map[string]string{"html": html})
	}
}

func buildAISystemPrompt(codeType string) string {
	base := `You are a code assistant for the OrgPulse workflow editor. You generate code that replaces the current editor contents.

IMPORTANT RULES:
- Return ONLY the code. No explanations, no markdown formatting, no code fences.
- If you must include a code fence, use a single fenced block — the system will extract it.
- The code must be complete and ready to use — not a fragment or diff.
`

	switch codeType {
	case "js_input":
		return base + `
You are writing an INPUT TRANSFORM script (JavaScript, executed server-side via goja).
- The global variable "input" contains the request parameters (query params for GET, JSON body for POST).
- The script must return an object whose keys become available as SQL query parameter bindings.
- Example: ({team_id: input.team_id || '', min_count: parseInt(input.min_count) || 0})
- Use console.log/warn/error for debugging output.
- Throw an Error to abort the pipeline with a message.`

	case "js_output":
		return base + `
You are writing an OUTPUT TRANSFORM script (JavaScript, executed server-side via goja).
- The global variable "input" contains the output of the input transform.
- The global variable "results" is an object mapping query labels to arrays of row objects.
- The script must return the final API response object.
- Example: ({team: (results['team_info'] || [])[0], sources: results['sources'] || []})
- Use console.log/warn/error for debugging output.`

	case "sql":
		return base + `
You are writing a SQL query for a MySQL database.
- Use :variable_name for parameter placeholders (e.g. :team_id, :min_count).
- Named variables are resolved from the input transform output and prior query results.
- You can call stored procedures with CALL procedure_name(:param).
- Each query has a label that is used to reference its results in the output transform.
- Keep queries focused — use multiple query blocks for independent data fetches.`

	case "js_assert":
		return base + `
You are writing a TEST ASSERTION script (JavaScript, executed server-side via goja).
- The global variable "output" contains the workflow's output (null if the pipeline errored).
- The global variable "error" contains the pipeline error message (null if successful).
- Throw an Error to fail the test with a message.
- Example: if (!output.team) throw new Error('missing team');
- The script should validate the structure and values of the output.`

	default:
		return base
	}
}

func extractCodeBlock(text string) string {
	// Look for a fenced code block and extract its contents
	start := -1
	end := -1
	lines := splitLines(text)
	for i, line := range lines {
		if len(line) >= 3 && line[:3] == "```" {
			if start == -1 {
				start = i + 1
			} else {
				end = i
				break
			}
		}
	}
	if start > 0 && end > start {
		result := ""
		for i := start; i < end; i++ {
			if i > start {
				result += "\n"
			}
			result += lines[i]
		}
		return result
	}
	return text
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
