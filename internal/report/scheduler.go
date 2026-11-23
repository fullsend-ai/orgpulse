package report

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/fullsend-ai/orgpulse/internal/database"
)

type SchedulerConfig struct {
	SubscriptionInterval time.Duration
	AlertInterval        time.Duration
	SubscriptionSpread   time.Duration
	AlertSpread          time.Duration
}

type Scheduler struct {
	svc *Service
	cfg SchedulerConfig
}

func NewScheduler(svc *Service, cfg SchedulerConfig) *Scheduler {
	return &Scheduler{svc: svc, cfg: cfg}
}

func (sc *Scheduler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); sc.runSubscriptions(ctx) }()
	go func() { defer wg.Done(); sc.runAlerts(ctx) }()
	wg.Wait()
}

func (sc *Scheduler) Tick(ctx context.Context) {
	sc.processSubscriptions(ctx)
	sc.processAlerts(ctx)
}

func (sc *Scheduler) runSubscriptions(ctx context.Context) {
	sc.processSubscriptions(ctx)
	ticker := time.NewTicker(sc.cfg.SubscriptionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sc.processSubscriptions(ctx)
		}
	}
}

func (sc *Scheduler) runAlerts(ctx context.Context) {
	sc.processAlerts(ctx)
	ticker := time.NewTicker(sc.cfg.AlertInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sc.processAlerts(ctx)
		}
	}
}

func (sc *Scheduler) processSubscriptions(ctx context.Context) {
	subs, err := sc.svc.store.ListSubscriptionRows(ctx)
	if err != nil {
		slog.Error("scheduler: failed to list subscriptions", "err", err)
		return
	}

	var due []int64
	for _, sub := range subs {
		if isDue(sub.LastRunAt, sub.IntervalDays) {
			due = append(due, sub.ID)
		}
	}

	if len(due) == 0 {
		return
	}

	sort.Slice(due, func(i, j int) bool {
		return spreadHash(due[i]) < spreadHash(due[j])
	})

	pace := paceDuration(sc.cfg.SubscriptionSpread, len(due))

	for i, id := range due {
		if ctx.Err() != nil {
			return
		}
		slog.Info("scheduler: sending report", "subscription_id", id)
		if err := sc.svc.SendReport(ctx, id); err != nil {
			slog.Error("scheduler: report failed", "subscription_id", id, "err", err)
		} else {
			slog.Info("scheduler: report sent", "subscription_id", id)
		}
		if pace > 0 && i < len(due)-1 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(pace):
			}
		}
	}
}

func (sc *Scheduler) processAlerts(ctx context.Context) {
	alerts, err := sc.svc.store.ListAlertRows(ctx)
	if err != nil {
		slog.Error("scheduler: failed to list alerts", "err", err)
		return
	}

	if len(alerts) == 0 {
		return
	}

	sort.Slice(alerts, func(i, j int) bool {
		return spreadHash(alerts[i].ID) < spreadHash(alerts[j].ID)
	})

	pace := paceDuration(sc.cfg.AlertSpread, len(alerts))

	for i, alert := range alerts {
		if ctx.Err() != nil {
			return
		}
		sc.checkAlert(ctx, alert)
		if pace > 0 && i < len(alerts)-1 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(pace):
			}
		}
	}
}

func (sc *Scheduler) checkAlert(ctx context.Context, alert database.AlertRow) {
	payload := alert.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}

	respJSON, err := sc.svc.postJSON(ctx, alert.URL, payload)
	if err != nil {
		slog.Error("scheduler: alert fetch failed", "alert_id", alert.ID, "err", err)
		return
	}

	triggered, err := evaluateRule(alert.Rule, respJSON)
	if err != nil {
		slog.Error("scheduler: rule evaluation failed", "alert_id", alert.ID, "err", err)
		return
	}

	if !triggered {
		return
	}

	slog.Info("scheduler: alert triggered", "alert_id", alert.ID, "email", alert.Email)

	analysisHTML, err := sc.svc.postForText(ctx, alert.AIAnalyticsURL, respJSON)
	if err != nil {
		slog.Error("scheduler: alert AI analytics failed", "alert_id", alert.ID, "err", err)
		return
	}

	title := "Alert: " + buildTitle(alert.ProductID, alert.TeamID)
	if err := sc.svc.sendEmail(alert.Email, title, []byte(sanitizeEmailHTML(analysisHTML))); err != nil {
		slog.Error("scheduler: alert email failed", "alert_id", alert.ID, "err", err)
	}
}

func spreadHash(id int64) uint64 {
	return uint64(id) * 2654435761 //nolint:gosec
}

func paceDuration(spread time.Duration, count int) time.Duration {
	if spread <= 0 || count <= 1 {
		return 0
	}
	return spread / time.Duration(count)
}

// ruleTimeout bounds one alert rule evaluation; alert rules are user-supplied.
var ruleTimeout = 10 * time.Second

func evaluateRule(rule string, data json.RawMessage) (bool, error) {
	vm := goja.New()
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return false, err
	}
	err := vm.Set("data", parsed)
	if err != nil {
		return false, err
	}
	timer := time.AfterFunc(ruleTimeout, func() { vm.Interrupt("rule exceeded " + ruleTimeout.String()) })
	defer timer.Stop()
	result, err := vm.RunString(rule)
	if err != nil {
		return false, err
	}
	return result.ToBoolean(), nil
}

func isDue(lastRunAt *string, intervalDays *int) bool {
	if intervalDays == nil || *intervalDays <= 0 {
		return false
	}
	if lastRunAt == nil {
		return true
	}
	t, err := time.Parse(time.RFC3339, *lastRunAt)
	if err != nil {
		return true
	}
	return time.Since(t) >= time.Duration(*intervalDays)*24*time.Hour
}
