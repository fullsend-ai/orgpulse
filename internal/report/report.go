package report

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/microcosm-cc/bluemonday"

	"github.com/fullsend-ai/orgpulse/internal/chart"
	"github.com/fullsend-ai/orgpulse/internal/database"
)

var ErrNotFound = errors.New("subscription not found")

// maxResponseBody caps how much of a metrics or AI response is read.
const maxResponseBody = 32 << 20

// maxErrorBody caps how much of an upstream error body is quoted in errors.
const maxErrorBody = 512

func errorBody(b []byte) string {
	if len(b) > maxErrorBody {
		return string(b[:maxErrorBody]) + "..."
	}
	return string(b)
}

type Config struct {
	AIAnalyticsURL string
	AISummaryURL   string
	SMTPHost       string
	SMTPPort       int
	SMTPFrom       string
	SendMailFunc   func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

type Service struct {
	store  database.ConfigStore
	client *http.Client
	cfg    Config
}

func NewService(store database.ConfigStore, client *http.Client, cfg Config) *Service {
	if cfg.SendMailFunc == nil {
		cfg.SendMailFunc = smtp.SendMail
	}
	return &Service{store: store, client: client, cfg: cfg}
}

type reportSection struct {
	ImageDataURI template.URL
	AnalysisHTML template.HTML
}

var reportTmpl = template.Must(template.New("report").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.Title}}</title>
<style>
body { max-width: 800px; margin: 0 auto; padding: 24px; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; background: #ffffff; }
.chart-img { display: block; margin: 16px auto; max-width: 100%; }
.section { margin-bottom: 8px; padding-bottom: 8px; border-bottom: 1px solid #e5e7eb; }
table { border: none !important; }
</style>
</head>
<body>
{{.Summary}}
{{range .Sections}}
<div class="section">
{{if .ImageDataURI}}<img class="chart-img" src="{{.ImageDataURI}}" />
{{end}}{{.AnalysisHTML}}
</div>
{{end}}
</body>
</html>
`))

func (s *Service) SendReport(ctx context.Context, subscriptionID int64) error {
	sub, err := s.store.GetSubscriptionRow(ctx, subscriptionID)
	if err != nil {
		return fmt.Errorf("looking up subscription: %w", err)
	}
	if sub == nil {
		return ErrNotFound
	}

	var sections []reportSection
	var rawJSONs []json.RawMessage

	for _, el := range sub.Elements {
		payload := el.Payload
		if len(payload) == 0 {
			payload = json.RawMessage(`{}`)
		}

		metricsJSON, err := s.postJSON(ctx, el.URL, payload)
		if err != nil {
			return fmt.Errorf("fetching metrics from %s: %w", el.URL, err)
		}
		rawJSONs = append(rawJSONs, metricsJSON)

		var sec reportSection
		var chartReq chart.Request
		if err := json.Unmarshal(metricsJSON, &chartReq); err == nil && chartReq.Type != "" {
			if png, err := chart.Render(chartReq); err == nil {
				sec.ImageDataURI = template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png)) // #nosec G203 -- data URI of our own PNG
			}
		}

		if sub.AIAnalyticsURL != "" {
			analysisHTML, err := s.postForText(ctx, sub.AIAnalyticsURL, metricsJSON)
			if err != nil {
				return fmt.Errorf("fetching analysis from AI endpoint: %w", err)
			}
			sec.AnalysisHTML = template.HTML(sanitizeEmailHTML(analysisHTML)) // #nosec G203 -- sanitized
		}
		sections = append(sections, sec)
	}

	title := buildTitle(sub.ProductID, sub.TeamID)

	partialHTML, err := assembleHTML(title, "", sections)
	if err != nil {
		return fmt.Errorf("assembling partial report: %w", err)
	}

	summaryPayload, err := json.Marshal(struct {
		ReportHTML string            `json:"report_html"`
		Data       []json.RawMessage `json:"data"`
	}{
		ReportHTML: string(partialHTML),
		Data:       rawJSONs,
	})
	if err != nil {
		return fmt.Errorf("marshaling summary request: %w", err)
	}

	summaryHTML, err := s.postForText(ctx, s.cfg.AISummaryURL, summaryPayload)
	if err != nil {
		return fmt.Errorf("fetching executive summary from AI endpoint: %w", err)
	}

	finalHTML, err := assembleHTML(title, summaryHTML, sections)
	if err != nil {
		return fmt.Errorf("assembling final report: %w", err)
	}

	if err := s.sendEmail(sub.Email, title, finalHTML); err != nil {
		return err
	}

	if err := s.store.UpdateSubscriptionLastRunAt(ctx, subscriptionID, time.Now()); err != nil {
		slog.Error("failed to update last_run_at after sending report", "subscription_id", subscriptionID, "err", err)
	}
	return nil
}

func assembleHTML(title, summary string, sections []reportSection) ([]byte, error) {
	data := struct {
		Title    string
		Summary  template.HTML
		Sections []reportSection
	}{
		Title:    title,
		Summary:  template.HTML(sanitizeEmailHTML(summary)), // #nosec G203 -- sanitized
		Sections: sections,
	}
	var buf bytes.Buffer
	if err := reportTmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("executing report template: %w", err)
	}
	return buf.Bytes(), nil
}

func buildTitle(productID, teamID string) string {
	switch {
	case productID != "" && teamID != "":
		return productID + " — " + teamID
	case productID != "":
		return productID
	case teamID != "":
		return teamID
	default:
		return "Metrics Report"
	}
}

func (s *Service) postJSON(ctx context.Context, url string, body json.RawMessage) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, errorBody(respBody))
	}
	return json.RawMessage(respBody), nil
}

func (s *Service) postForText(ctx context.Context, url string, body []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, errorBody(respBody))
	}
	return string(respBody), nil
}

// emailPolicy keeps the formatting AI endpoints produce (tables, inline
// styles, links, images) and drops scripts, event handlers, forms, frames and
// javascript: URLs. AI output can echo text from the metrics it analyses.
var emailPolicy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("style", "class", "align", "valign", "bgcolor", "width", "height").Globally()
	p.AllowAttrs("border", "cellpadding", "cellspacing").OnElements("table")
	p.AllowElements("center", "font")
	p.AllowAttrs("color", "face", "size").OnElements("font")
	return p
}()

func sanitizeEmailHTML(s string) string {
	return emailPolicy.Sanitize(s)
}

// headerValue makes s safe for a single mail header line: CR and LF would
// let a product or team ID inject extra headers.
func headerValue(s string) string {
	s = strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
	return mime.QEncoding.Encode("utf-8", s)
}

func (s *Service) sendEmail(to, subject string, htmlBody []byte) error {
	var msg bytes.Buffer
	fmt.Fprintf(&msg, "From: %s\r\n", s.cfg.SMTPFrom)
	fmt.Fprintf(&msg, "To: %s\r\n", to)
	fmt.Fprintf(&msg, "Subject: %s\r\n", headerValue(subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.Write(htmlBody)

	addr := fmt.Sprintf("%s:%d", s.cfg.SMTPHost, s.cfg.SMTPPort)
	return s.cfg.SendMailFunc(addr, nil, s.cfg.SMTPFrom, []string{to}, msg.Bytes())
}
