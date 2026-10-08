// Package reminder fires due reminders on a timer and fans them out to live
// subscribers and an optional webhook.
package reminder

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/liliang-cn/noted/internal/store"
)

// Hub delivers fired reminders to the streams currently watching a user.
// Delivery is best effort: a slow subscriber drops events rather than
// blocking the scheduler, and catches up through ListReminders.
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan store.Reminder]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[string]map[chan store.Reminder]struct{}{}} }

// Subscribe returns a channel of the user's reminders and a function that
// stops the subscription.
func (h *Hub) Subscribe(userID string) (<-chan store.Reminder, func()) {
	ch := make(chan store.Reminder, 16)
	h.mu.Lock()
	if h.subs[userID] == nil {
		h.subs[userID] = map[chan store.Reminder]struct{}{}
	}
	h.subs[userID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs[userID], ch)
		if len(h.subs[userID]) == 0 {
			delete(h.subs, userID)
		}
		h.mu.Unlock()
	}
}

// Subscribers reports how many streams are watching the user.
func (h *Hub) Subscribers(userID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[userID])
}

func (h *Hub) Publish(r store.Reminder) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[r.UserID] {
		select {
		case ch <- r:
		default:
			slog.Warn("reminder subscriber is behind; dropping", "user", r.UserID, "reminder", r.ID)
		}
	}
}

type Scheduler struct {
	Store      *store.Store
	Hub        *Hub
	WebhookURL string
	Interval   time.Duration // default 15s
	Retention  time.Duration // reminder history kept, default 30 days
	client     *http.Client
}

// Run blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	if s.Interval <= 0 {
		s.Interval = 15 * time.Second
	}
	if s.Retention <= 0 {
		s.Retention = 30 * 24 * time.Hour
	}
	s.client = &http.Client{Timeout: 10 * time.Second}
	t := time.NewTicker(s.Interval)
	defer t.Stop()
	lastPrune := time.Time{}
	for {
		s.Tick(ctx, time.Now())
		if time.Since(lastPrune) > 24*time.Hour {
			if err := s.Store.PruneReminders(ctx, time.Now().Add(-s.Retention)); err != nil {
				slog.Error("prune reminders", "err", err)
			}
			lastPrune = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick fires everything due at now. Exposed so tests need no real clock.
func (s *Scheduler) Tick(ctx context.Context, now time.Time) {
	due, err := s.Store.CollectDueReminders(ctx, now)
	if err != nil {
		slog.Error("collect reminders", "err", err)
		return
	}
	for _, r := range due {
		slog.Info("reminder", "user", r.UserID, "title", r.Title)
		s.Hub.Publish(r)
		if s.WebhookURL != "" {
			go s.post(r)
		}
	}
}

type webhookBody struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	RefID   string    `json:"ref_id"`
	Title   string    `json:"title"`
	DueTime time.Time `json:"due_time"`
	Fired   time.Time `json:"fire_time"`
	UserID  string    `json:"user_id"`
}

func (s *Scheduler) post(r store.Reminder) {
	kind := "event"
	if r.Kind == store.KindTask {
		kind = "task"
	}
	body, _ := json.Marshal(webhookBody{r.ID, kind, r.RefID, r.Title, r.Due, r.Fired, r.UserID})
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := s.client.Post(s.WebhookURL, "application/json", bytes.NewReader(body))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 300 {
				return
			}
			slog.Warn("webhook rejected reminder", "status", resp.StatusCode)
		} else {
			slog.Warn("webhook failed", "err", err)
		}
		time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
	}
}
