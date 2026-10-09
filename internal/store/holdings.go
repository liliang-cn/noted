package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// A Holding is something the user invests in (a ticker such as QQQM) with the
// buys and sells they made, and optionally a monthly plan ("the 15th, 500").
// noted records and reports; it does not fetch prices or move money. The last
// price is one the user enters.
type Holding struct {
	ID        string
	UserID    string
	Symbol    string
	Name      string
	Currency  string
	Notes     string
	LastPrice *float64
	PriceTime *time.Time
	DCAAmount float64 // planned amount per month; 0 with DCADay 0: no plan
	DCADay    int     // day of month 1-28; 0: no plan
	DCATime   string  // "HH:MM" in TimeZone
	DCAEvent  string  // the recurring calendar event that reminds about the plan
	TimeZone  string
	Archived  bool
	Created   time.Time
	Updated   time.Time
}

type HoldingPatch struct {
	Name, Currency, Notes *string
	LastPrice             **float64 // non-nil: set; *LastPrice == nil clears
	DCAAmount             *float64
	DCADay                *int
	DCATime, TimeZone     *string
	Archived              *bool
}

type Trade struct {
	ID        string
	HoldingID string
	UserID    string
	Time      time.Time
	Side      string // "buy" or "sell"
	Shares    float64
	Price     float64
	Fee       float64
	Note      string
}

type DCAStatus struct {
	Active        bool
	Next          time.Time // the next planned day
	DoneThisMonth bool      // a buy was recorded this calendar month
	InvestedMonth float64   // bought this calendar month, fees included
	StreakMonths  int       // consecutive calendar months with a buy, counting this one if done, else ending last month
}

type Position struct {
	Shares        float64
	AvgCost       float64 // per share, of the shares still held
	CostBasis     float64 // AvgCost * Shares
	RealizedPnL   float64
	TotalBought   float64 // everything ever spent on buys, fees included
	Trades        int
	HasPrice      bool
	MarketValue   float64
	UnrealizedPnL float64
	UnrealizedPct float64 // of cost basis; 0 without cost basis
	DCA           DCAStatus
}

type HoldingView struct {
	Holding  Holding
	Position Position
}

var symbolRE = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.\-]{0,14}$`)

const (
	maxShares   = 1e12
	maxPrice    = 1e9
	epsShares   = 1e-9
	maxHoldings = 500
)

func (h *Holding) normalize() error {
	h.Symbol = strings.ToUpper(strings.TrimSpace(h.Symbol))
	if !symbolRE.MatchString(h.Symbol) {
		return invalid("symbol must be 1-15 letters, digits, '.' or '-'")
	}
	h.Name = strings.TrimSpace(h.Name)
	if utf8.RuneCountInString(h.Name) > 100 || utf8.RuneCountInString(h.Notes) > 2000 {
		return invalid("name is at most 100 characters and notes at most 2000")
	}
	h.Currency = strings.ToUpper(strings.TrimSpace(h.Currency))
	if h.Currency == "" {
		h.Currency = "USD"
	}
	if len(h.Currency) < 3 || len(h.Currency) > 5 {
		return invalid("currency must be a code such as USD")
	}
	if h.LastPrice != nil && (math.IsNaN(*h.LastPrice) || *h.LastPrice < 0 || *h.LastPrice > maxPrice) {
		return invalid("last_price must be between 0 and %g", maxPrice)
	}
	if h.DCADay < 0 || h.DCADay > 28 {
		return invalid("dca_day must be 1-28 (or 0 for no plan)")
	}
	if h.DCAAmount < 0 || math.IsNaN(h.DCAAmount) || h.DCAAmount > maxPrice {
		return invalid("dca_amount must be between 0 and %g", maxPrice)
	}
	if h.DCADay == 0 {
		h.DCAAmount = 0
	}
	if h.DCATime == "" {
		h.DCATime = "09:00"
	}
	if _, err := time.Parse("15:04", h.DCATime); err != nil {
		return invalid("dca_time must be HH:MM")
	}
	if h.TimeZone == "" {
		h.TimeZone = "UTC"
	}
	if _, err := time.LoadLocation(h.TimeZone); err != nil {
		return invalid("unknown time_zone %q", h.TimeZone)
	}
	return nil
}

func (h Holding) location() *time.Location {
	loc, err := time.LoadLocation(h.TimeZone)
	if err != nil {
		return time.UTC
	}
	return loc
}

const holdingCols = `id, user_id, symbol, name, currency, notes, last_price, last_price_ms, dca_amount, dca_day, dca_time, dca_event_id, tz, archived, created_ms, updated_ms`

func scanHolding(r scanner) (Holding, error) {
	var h Holding
	var price sql.NullFloat64
	var priceMS sql.NullInt64
	var archived int
	var created, updated int64
	if err := r.Scan(&h.ID, &h.UserID, &h.Symbol, &h.Name, &h.Currency, &h.Notes, &price, &priceMS, &h.DCAAmount, &h.DCADay, &h.DCATime,
		&h.DCAEvent, &h.TimeZone, &archived, &created, &updated); err != nil {
		return Holding{}, err
	}
	if price.Valid {
		v := price.Float64
		h.LastPrice = &v
	}
	h.PriceTime = ptrTime(priceMS)
	h.Archived = archived != 0
	h.Created, h.Updated = fromMS(created), fromMS(updated)
	return h, nil
}

func nullFloat(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func (s *Store) CreateHolding(ctx context.Context, h Holding) (Holding, error) {
	if err := h.normalize(); err != nil {
		return Holding{}, err
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM holdings WHERE user_id = ?`, h.UserID).Scan(&n); err != nil {
		return Holding{}, err
	}
	if n >= maxHoldings {
		return Holding{}, invalid("at most %d holdings", maxHoldings)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	h.ID, h.Created, h.Updated = uuid.NewString(), now, now
	if h.LastPrice != nil {
		h.PriceTime = &now
	} else {
		h.PriceTime = nil
	}
	if err := s.syncDCAEvent(ctx, &h, now); err != nil {
		return Holding{}, err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO holdings(`+holdingCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.ID, h.UserID, h.Symbol, h.Name, h.Currency, h.Notes, nullFloat(h.LastPrice), nullMS(h.PriceTime), h.DCAAmount, h.DCADay, h.DCATime,
		h.DCAEvent, h.TimeZone, b2i(h.Archived), ms(h.Created), ms(h.Updated))
	if err != nil {
		if h.DCAEvent != "" {
			_ = s.DeleteEvent(ctx, h.UserID, h.DCAEvent)
		}
		if strings.Contains(err.Error(), "UNIQUE") {
			return Holding{}, invalid("%s is already tracked", h.Symbol)
		}
		return Holding{}, err
	}
	return h, nil
}

func (s *Store) GetHolding(ctx context.Context, userID, id string) (Holding, error) {
	h, err := scanHolding(s.db.QueryRowContext(ctx, `SELECT `+holdingCols+` FROM holdings WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Holding{}, ErrNotFound
	}
	return h, err
}

func (s *Store) UpdateHolding(ctx context.Context, userID, id string, p HoldingPatch) (Holding, error) {
	h, err := s.GetHolding(ctx, userID, id)
	if err != nil {
		return Holding{}, err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if p.Name != nil {
		h.Name = *p.Name
	}
	if p.Currency != nil {
		h.Currency = *p.Currency
	}
	if p.Notes != nil {
		h.Notes = *p.Notes
	}
	if p.LastPrice != nil {
		h.LastPrice = *p.LastPrice
		h.PriceTime = nil
		if h.LastPrice != nil {
			h.PriceTime = &now
		}
	}
	if p.DCAAmount != nil {
		h.DCAAmount = *p.DCAAmount
	}
	if p.DCADay != nil {
		h.DCADay = *p.DCADay
	}
	if p.DCATime != nil {
		h.DCATime = *p.DCATime
	}
	if p.TimeZone != nil {
		h.TimeZone = *p.TimeZone
	}
	if p.Archived != nil {
		h.Archived = *p.Archived
	}
	if err := h.normalize(); err != nil {
		return Holding{}, err
	}
	if h.Archived {
		// An archived holding stops reminding.
		h.DCADay = 0
		h.DCAAmount = 0
	}
	if err := s.syncDCAEvent(ctx, &h, now); err != nil {
		return Holding{}, err
	}
	h.Updated = now
	_, err = s.db.ExecContext(ctx, `UPDATE holdings SET name = ?, currency = ?, notes = ?, last_price = ?, last_price_ms = ?, dca_amount = ?, dca_day = ?, dca_time = ?,
		dca_event_id = ?, tz = ?, archived = ?, updated_ms = ? WHERE id = ? AND user_id = ?`,
		h.Name, h.Currency, h.Notes, nullFloat(h.LastPrice), nullMS(h.PriceTime), h.DCAAmount, h.DCADay, h.DCATime, h.DCAEvent, h.TimeZone, b2i(h.Archived), ms(h.Updated), id, userID)
	return h, err
}

func (s *Store) DeleteHolding(ctx context.Context, userID, id string) error {
	h, err := s.GetHolding(ctx, userID, id)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM holdings WHERE id = ? AND user_id = ?`, id, userID); err != nil {
		return err
	}
	if h.DCAEvent != "" {
		if err := s.DeleteEvent(ctx, userID, h.DCAEvent); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	return nil
}

// nextDCA is the first plan day at or after now.
func nextDCA(h Holding, now time.Time) time.Time {
	loc := h.location()
	t, _ := time.Parse("15:04", h.DCATime)
	n := now.In(loc)
	d := time.Date(n.Year(), n.Month(), h.DCADay, t.Hour(), t.Minute(), 0, 0, loc)
	if d.Before(n) {
		d = time.Date(n.Year(), n.Month()+1, h.DCADay, t.Hour(), t.Minute(), 0, 0, loc)
	}
	return d
}

// syncDCAEvent keeps one monthly calendar event in step with the plan: created
// when a plan appears, moved or retitled when it changes, removed when it ends.
func (s *Store) syncDCAEvent(ctx context.Context, h *Holding, now time.Time) error {
	if h.DCADay == 0 {
		if h.DCAEvent != "" {
			if err := s.DeleteEvent(ctx, h.UserID, h.DCAEvent); err != nil && !errors.Is(err, ErrNotFound) {
				return err
			}
			h.DCAEvent = ""
		}
		return nil
	}
	title := "定投 " + h.Symbol
	desc := ""
	if h.DCAAmount > 0 {
		desc = fmt.Sprintf("计划 %s %s", trimFloat(h.DCAAmount), h.Currency)
	}
	start := nextDCA(*h, now)
	zero := 0
	if h.DCAEvent != "" {
		rb := &zero
		end := start.Add(30 * time.Minute)
		rule, tz, allDay := "FREQ=MONTHLY", h.TimeZone, false
		_, err := s.UpdateEvent(ctx, h.UserID, h.DCAEvent, EventPatch{Title: &title, Description: &desc, Start: &start, End: &end, AllDay: &allDay, TimeZone: &tz, RRule: &rule, RemindBefore: &rb})
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrNotFound) { // the event was deleted by hand: make a new one
			return err
		}
	}
	e, err := s.CreateEvent(ctx, Event{UserID: h.UserID, Title: title, Description: desc, Start: start, End: start.Add(30 * time.Minute),
		TimeZone: h.TimeZone, RRule: "FREQ=MONTHLY", RemindBefore: &zero, Space: SpaceLife})
	if err != nil {
		return err
	}
	h.DCAEvent = e.ID
	return nil
}

func trimFloat(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}

// RecordTrade adds a buy or sell. Selling more than is held at that time is refused.
func (s *Store) RecordTrade(ctx context.Context, userID, holdingID string, t Trade) (Trade, error) {
	if _, err := s.GetHolding(ctx, userID, holdingID); err != nil {
		return Trade{}, err
	}
	if t.Side != "buy" && t.Side != "sell" {
		return Trade{}, invalid("side must be buy or sell")
	}
	if !(t.Shares > 0) || t.Shares > maxShares || math.IsNaN(t.Shares) {
		return Trade{}, invalid("shares must be greater than 0")
	}
	if t.Price < 0 || t.Price > maxPrice || math.IsNaN(t.Price) || t.Fee < 0 || t.Fee > maxPrice || math.IsNaN(t.Fee) {
		return Trade{}, invalid("price and fee must be between 0 and %g", maxPrice)
	}
	if utf8.RuneCountInString(t.Note) > 500 {
		return Trade{}, invalid("note is longer than 500 characters")
	}
	if t.Time.IsZero() {
		t.Time = time.Now()
	}
	t.ID, t.HoldingID, t.UserID, t.Time, t.Note = uuid.NewString(), holdingID, userID, t.Time.UTC().Truncate(time.Millisecond), strings.TrimSpace(t.Note)
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO holding_trades(id, holding_id, user_id, time_ms, side, shares, price, fee, note) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			t.ID, t.HoldingID, t.UserID, ms(t.Time), t.Side, t.Shares, t.Price, t.Fee, t.Note); err != nil {
			return err
		}
		all, err := tradesIn(ctx, tx, userID, holdingID)
		if err != nil {
			return err
		}
		if _, _, _, _, ok := replay(all); !ok {
			return invalid("that sells more shares than were held at the time")
		}
		return nil
	})
	return t, err
}

func (s *Store) DeleteTrade(ctx context.Context, userID, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var holdingID string
		err := tx.QueryRowContext(ctx, `SELECT holding_id FROM holding_trades WHERE id = ? AND user_id = ?`, id, userID).Scan(&holdingID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM holding_trades WHERE id = ?`, id); err != nil {
			return err
		}
		all, err := tradesIn(ctx, tx, userID, holdingID)
		if err != nil {
			return err
		}
		if _, _, _, _, ok := replay(all); !ok {
			return invalid("deleting this buy would leave a later sell without shares")
		}
		return nil
	})
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func tradesIn(ctx context.Context, q querier, userID, holdingID string) ([]Trade, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, holding_id, user_id, time_ms, side, shares, price, fee, note FROM holding_trades
		WHERE holding_id = ? AND user_id = ? ORDER BY time_ms, CASE side WHEN 'buy' THEN 0 ELSE 1 END, id`, holdingID, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Trade
	for rows.Next() {
		var t Trade
		var tm int64
		if err := rows.Scan(&t.ID, &t.HoldingID, &t.UserID, &tm, &t.Side, &t.Shares, &t.Price, &t.Fee, &t.Note); err != nil {
			return nil, err
		}
		t.Time = fromMS(tm)
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListTrades returns the holding's trades, oldest first.
func (s *Store) ListTrades(ctx context.Context, userID, holdingID string) ([]Trade, error) {
	if _, err := s.GetHolding(ctx, userID, holdingID); err != nil {
		return nil, err
	}
	return tradesIn(ctx, s.db, userID, holdingID)
}

// replay applies trades in time order with the average-cost method. ok is false
// when a sell exceeds what was held.
func replay(trades []Trade) (shares, cost, realized, bought float64, ok bool) {
	for _, t := range trades {
		switch t.Side {
		case "buy":
			shares += t.Shares
			cost += t.Shares*t.Price + t.Fee
			bought += t.Shares*t.Price + t.Fee
		case "sell":
			if t.Shares > shares+epsShares {
				return 0, 0, 0, 0, false
			}
			avg := 0.0
			if shares > 0 {
				avg = cost / shares
			}
			realized += t.Shares*(t.Price-avg) - t.Fee
			cost -= t.Shares * avg
			shares -= t.Shares
			if shares < epsShares {
				shares, cost = 0, 0
			}
		}
	}
	return shares, cost, realized, bought, true
}

func (s *Store) viewHolding(ctx context.Context, h Holding, now time.Time) (HoldingView, error) {
	trades, err := tradesIn(ctx, s.db, h.UserID, h.ID)
	if err != nil {
		return HoldingView{}, err
	}
	return holdingView(h, trades, now), nil
}

func holdingView(h Holding, trades []Trade, now time.Time) HoldingView {
	shares, cost, realized, bought, _ := replay(trades)
	p := Position{Shares: shares, CostBasis: cost, RealizedPnL: realized, TotalBought: bought, Trades: len(trades)}
	if shares > 0 {
		p.AvgCost = cost / shares
	}
	if h.LastPrice != nil {
		p.HasPrice = true
		p.MarketValue = shares * *h.LastPrice
		p.UnrealizedPnL = p.MarketValue - cost
		if cost > 0 {
			p.UnrealizedPct = p.UnrealizedPnL / cost
		}
	}
	loc := h.location()
	n := now.In(loc)
	months := map[[2]int]float64{}
	for _, t := range trades {
		if t.Side == "buy" {
			tl := t.Time.In(loc)
			months[[2]int{tl.Year(), int(tl.Month())}] += t.Shares*t.Price + t.Fee
		}
	}
	key := [2]int{n.Year(), int(n.Month())}
	p.DCA.InvestedMonth = months[key]
	_, p.DCA.DoneThisMonth = months[key]
	// Consecutive months with a buy; this month counts once it is done, and its
	// absence does not break a streak that ended last month.
	cur := time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, loc)
	if !p.DCA.DoneThisMonth {
		cur = cur.AddDate(0, -1, 0)
	}
	for {
		if _, ok := months[[2]int{cur.Year(), int(cur.Month())}]; !ok {
			break
		}
		p.DCA.StreakMonths++
		cur = cur.AddDate(0, -1, 0)
	}
	if h.DCADay > 0 {
		p.DCA.Active = true
		p.DCA.Next = nextDCA(h, now)
	}
	return HoldingView{Holding: h, Position: p}
}

func (s *Store) ViewHolding(ctx context.Context, userID, id string, now time.Time) (HoldingView, error) {
	h, err := s.GetHolding(ctx, userID, id)
	if err != nil {
		return HoldingView{}, err
	}
	return s.viewHolding(ctx, h, now)
}

// ListHoldings returns holdings with positions: ones with a plan still to do this
// month first, then by market value (or cost) descending.
func (s *Store) ListHoldings(ctx context.Context, userID string, includeArchived bool, now time.Time) ([]HoldingView, error) {
	q := `SELECT ` + holdingCols + ` FROM holdings WHERE user_id = ?`
	if !includeArchived {
		q += ` AND archived = 0`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY symbol`, userID)
	if err != nil {
		return nil, err
	}
	var hs []Holding
	for rows.Next() {
		h, err := scanHolding(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		hs = append(hs, h)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]HoldingView, 0, len(hs))
	for _, h := range hs {
		v, err := s.viewHolding(ctx, h, now)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	value := func(v HoldingView) float64 {
		if v.Position.HasPrice {
			return v.Position.MarketValue
		}
		return v.Position.CostBasis
	}
	sort.SliceStable(out, func(i, j int) bool {
		di := out[i].Position.DCA.Active && !out[i].Position.DCA.DoneThisMonth
		dj := out[j].Position.DCA.Active && !out[j].Position.DCA.DoneThisMonth
		if di != dj {
			return di
		}
		return value(out[i]) > value(out[j])
	})
	return out, nil
}
