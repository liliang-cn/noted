package server_test

import (
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestHoldingPositionUsesAverageCost(t *testing.T) {
	e := start(t, nil, false)
	ctx := as(e.alice)
	h := pb.NewHoldingServiceClient(e.conn(t))

	v, err := h.CreateHolding(ctx, &pb.CreateHoldingRequest{Holding: &pb.Holding{Symbol: " voo ", Name: "Vanguard S&P 500"}})
	if err != nil || v.Symbol != "VOO" || v.Currency != "USD" || v.Position.Shares != 0 || v.Position.Dca.Active {
		t.Fatalf("%+v %v", v, err)
	}
	day := func(d int) *timestamppb.Timestamp {
		return timestamppb.New(time.Date(2026, 1, d, 12, 0, 0, 0, time.UTC))
	}
	buy := func(d int, shares, price, fee float64) (*pb.Holding, error) {
		return h.RecordTrade(ctx, &pb.RecordTradeRequest{HoldingId: v.Id, Trade: &pb.Trade{Side: "buy", Shares: shares, Price: price, Fee: fee, Time: day(d)}})
	}
	if _, err := buy(2, 10, 100, 1); err != nil {
		t.Fatal(err)
	}
	got, err := buy(3, 10, 120, 0)
	if err != nil {
		t.Fatal(err)
	}
	p := got.Position
	// 10*100+1 + 10*120 = 2201 over 20 shares.
	if !near(p.Shares, 20) || !near(p.CostBasis, 2201) || !near(p.AvgCost, 110.05) || !near(p.TotalBought, 2201) || p.HasPrice {
		t.Fatalf("after two buys: %+v", p)
	}

	// Selling 5 at 130 realises 5*(130-110.05) and keeps the average of the rest.
	got, err = h.RecordTrade(ctx, &pb.RecordTradeRequest{HoldingId: v.Id, Trade: &pb.Trade{Side: "sell", Shares: 5, Price: 130, Time: day(4)}})
	if err != nil {
		t.Fatal(err)
	}
	p = got.Position
	if !near(p.Shares, 15) || !near(p.AvgCost, 110.05) || !near(p.RealizedPnl, 99.75) || !near(p.CostBasis, 1650.75) {
		t.Fatalf("after a sell: %+v", p)
	}

	// A price the user types gives market value and unrealised gain; clearing it removes them.
	got, err = h.UpdateHolding(ctx, &pb.UpdateHoldingRequest{Holding: &pb.Holding{Id: v.Id, LastPrice: ptr(125.0)}, UpdateMask: mask("last_price")})
	if err != nil {
		t.Fatal(err)
	}
	p = got.Position
	if !p.HasPrice || !near(p.MarketValue, 1875) || !near(p.UnrealizedPnl, 224.25) || !near(p.UnrealizedPercent, 224.25/1650.75) {
		t.Fatalf("with a price: %+v", p)
	}
	got, _ = h.UpdateHolding(ctx, &pb.UpdateHoldingRequest{Holding: &pb.Holding{Id: v.Id}, UpdateMask: mask("last_price")})
	if got.Position.HasPrice || got.LastPrice != nil {
		t.Fatalf("price not cleared: %+v", got)
	}

	// You cannot sell what you did not hold at the time, even backdated or after deleting a buy.
	if _, err := h.RecordTrade(ctx, &pb.RecordTradeRequest{HoldingId: v.Id, Trade: &pb.Trade{Side: "sell", Shares: 16, Price: 1}}); code(err) != codes.InvalidArgument {
		t.Fatalf("selling more than held: %v", err)
	}
	if _, err := h.RecordTrade(ctx, &pb.RecordTradeRequest{HoldingId: v.Id, Trade: &pb.Trade{Side: "sell", Shares: 1, Price: 1, Time: day(1)}}); code(err) != codes.InvalidArgument {
		t.Fatalf("selling before the first buy: %v", err)
	}
	ts, _ := h.ListTrades(ctx, &pb.ListTradesRequest{HoldingId: v.Id})
	if len(ts.Trades) != 3 || ts.Trades[0].Side != "buy" || ts.Trades[2].Side != "sell" {
		t.Fatalf("trades after the refusals: %+v", ts.Trades)
	}
	if _, err := h.DeleteTrade(ctx, &pb.DeleteTradeRequest{Id: ts.Trades[1].Id}); err != nil {
		t.Fatalf("deleting the second buy leaves 10 held, enough for the sell of 5: %v", err)
	}
	if _, err := h.DeleteTrade(ctx, &pb.DeleteTradeRequest{Id: ts.Trades[0].Id}); code(err) != codes.InvalidArgument {
		t.Fatalf("deleting the only buy under a sell: %v", err)
	}

	for name, tr := range map[string]*pb.Trade{
		"bad side":     {Side: "hold", Shares: 1, Price: 1},
		"no shares":    {Side: "buy", Shares: 0, Price: 1},
		"neg price":    {Side: "buy", Shares: 1, Price: -1},
		"negative fee": {Side: "buy", Shares: 1, Price: 1, Fee: -1},
	} {
		if _, err := h.RecordTrade(ctx, &pb.RecordTradeRequest{HoldingId: v.Id, Trade: tr}); code(err) != codes.InvalidArgument {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, in := range map[string]*pb.Holding{
		"duplicate":    {Symbol: "voo"},
		"bad symbol":   {Symbol: "V O O"},
		"empty":        {Symbol: ""},
		"day 31":       {Symbol: "X1", DcaDay: 31},
		"bad time":     {Symbol: "X2", DcaDay: 5, DcaTime: "25:00"},
		"bad currency": {Symbol: "X3", Currency: "D"},
	} {
		if _, err := h.CreateHolding(ctx, &pb.CreateHoldingRequest{Holding: in}); code(err) != codes.InvalidArgument {
			t.Errorf("%s: %v", name, err)
		}
	}

	// Isolation.
	if _, err := h.GetHolding(as(e.bob), &pb.GetHoldingRequest{Id: v.Id}); code(err) != codes.NotFound {
		t.Fatalf("bob get: %v", err)
	}
	if _, err := h.RecordTrade(as(e.bob), &pb.RecordTradeRequest{HoldingId: v.Id, Trade: &pb.Trade{Side: "buy", Shares: 1, Price: 1}}); code(err) != codes.NotFound {
		t.Fatalf("bob trade: %v", err)
	}
	if b, _ := h.CreateHolding(as(e.bob), &pb.CreateHoldingRequest{Holding: &pb.Holding{Symbol: "VOO"}}); b == nil {
		t.Fatal("another user may track the same ticker")
	}
}

func ptr[T any](v T) *T { return &v }

func TestMonthlyPlanOnThe15thRemindsAndTracksTheMonth(t *testing.T) {
	e := start(t, nil, false)
	ctx := as(e.alice)
	conn := e.conn(t)
	h, cal := pb.NewHoldingServiceClient(conn), pb.NewCalendarServiceClient(conn)

	v, err := h.CreateHolding(ctx, &pb.CreateHoldingRequest{Holding: &pb.Holding{Symbol: "QQQM", DcaDay: 15, DcaAmount: 500, TimeZone: "Asia/Shanghai"}})
	if err != nil {
		t.Fatal(err)
	}
	d := v.Position.Dca
	if !d.Active || d.DoneThisMonth || d.StreakMonths != 0 || v.DcaTime != "09:00" || v.DcaEventId == "" {
		t.Fatalf("a new plan: %+v event %q", d, v.DcaEventId)
	}
	next := d.NextTime.AsTime().In(time.FixedZone("CST", 8*3600))
	if next.Day() != 15 || next.Hour() != 9 || next.Before(time.Now()) {
		t.Fatalf("next plan day: %v", next)
	}

	// The calendar carries a monthly reminder on the 15th at 09:00, so reminders and the
	// watch pick it up without knowing about holdings.
	ev, err := cal.GetEvent(ctx, &pb.GetEventRequest{Id: v.DcaEventId})
	if err != nil || ev.Title != "定投 QQQM" || ev.Rrule != "FREQ=MONTHLY" || ev.RemindBeforeMinutes == nil || *ev.RemindBeforeMinutes != 0 {
		t.Fatalf("%+v %v", ev, err)
	}
	occ, err := cal.ListEvents(ctx, &pb.ListEventsRequest{From: timestamppb.New(time.Now()), To: timestamppb.New(time.Now().AddDate(0, 0, 100))})
	if err != nil || len(occ.Occurrences) < 3 {
		t.Fatalf("%v %v", occ, err)
	}
	for _, o := range occ.Occurrences {
		if l := o.StartTime.AsTime().In(time.FixedZone("CST", 8*3600)); l.Day() != 15 || l.Hour() != 9 {
			t.Fatalf("occurrence on %v", l)
		}
	}

	// Changing the day or amount moves the same event rather than adding another.
	v, err = h.UpdateHolding(ctx, &pb.UpdateHoldingRequest{Holding: &pb.Holding{Id: v.Id, DcaDay: 20, DcaAmount: 800}, UpdateMask: mask("dca_day", "dca_amount")})
	if err != nil {
		t.Fatal(err)
	}
	ev2, _ := cal.GetEvent(ctx, &pb.GetEventRequest{Id: v.DcaEventId})
	if ev2.Id != ev.Id || ev2.StartTime.AsTime().In(time.FixedZone("CST", 8*3600)).Day() != 20 || ev2.Description != "计划 800 USD" {
		t.Fatalf("event after the change: %+v", ev2)
	}
	all, _ := cal.ListEvents(ctx, &pb.ListEventsRequest{From: timestamppb.New(time.Now()), To: timestamppb.New(time.Now().AddDate(0, 0, 40))})
	n := 0
	for _, o := range all.Occurrences {
		if o.Event.Title == "定投 QQQM" {
			n++
		}
	}
	if n < 1 || n > 2 {
		t.Fatalf("%d QQQM reminders in 40 days", n)
	}

	// Buys this month and the two before make a 3-month streak; skipping a month breaks it.
	shanghai, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Now().In(shanghai)
	inMonth := func(back int) *timestamppb.Timestamp {
		return timestamppb.New(time.Date(now.Year(), now.Month()-time.Month(back), 10, 12, 0, 0, 0, shanghai))
	}
	buy := func(back int) *pb.Holding {
		r, err := h.RecordTrade(ctx, &pb.RecordTradeRequest{HoldingId: v.Id, Trade: &pb.Trade{Side: "buy", Shares: 3, Price: 160, Time: inMonth(back)}})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	buy(3)
	if got := buy(1); got.Position.Dca.StreakMonths != 1 || got.Position.Dca.DoneThisMonth {
		t.Fatalf("a buy last month only: %+v", got.Position.Dca)
	}
	buy(2)
	if got := buy(0); !got.Position.Dca.DoneThisMonth || got.Position.Dca.StreakMonths != 4 || !near(got.Position.Dca.InvestedThisMonth, 480) {
		t.Fatalf("buys in four months running: %+v", got.Position.Dca)
	}

	// Archiving or ending the plan removes the reminder; deleting the holding removes it too.
	if _, err := h.UpdateHolding(ctx, &pb.UpdateHoldingRequest{Holding: &pb.Holding{Id: v.Id}, UpdateMask: mask("dca_day")}); err != nil {
		t.Fatal(err)
	}
	if _, err := cal.GetEvent(ctx, &pb.GetEventRequest{Id: v.DcaEventId}); code(err) != codes.NotFound {
		t.Fatalf("event after ending the plan: %v", err)
	}
	v2, _ := h.CreateHolding(ctx, &pb.CreateHoldingRequest{Holding: &pb.Holding{Symbol: "OKLO", DcaDay: 1}})
	if _, err := h.UpdateHolding(ctx, &pb.UpdateHoldingRequest{Holding: &pb.Holding{Id: v2.Id, Archived: true}, UpdateMask: mask("archived")}); err != nil {
		t.Fatal(err)
	}
	if _, err := cal.GetEvent(ctx, &pb.GetEventRequest{Id: v2.DcaEventId}); code(err) != codes.NotFound {
		t.Fatalf("event after archiving: %v", err)
	}
	v3, _ := h.CreateHolding(ctx, &pb.CreateHoldingRequest{Holding: &pb.Holding{Symbol: "GLD", DcaDay: 5}})
	if _, err := h.DeleteHolding(ctx, &pb.DeleteHoldingRequest{Id: v3.Id}); err != nil {
		t.Fatal(err)
	}
	if _, err := cal.GetEvent(ctx, &pb.GetEventRequest{Id: v3.DcaEventId}); code(err) != codes.NotFound {
		t.Fatalf("event after deleting the holding: %v", err)
	}

	// The list puts holdings whose plan is still open this month first.
	h.CreateHolding(ctx, &pb.CreateHoldingRequest{Holding: &pb.Holding{Symbol: "AAA", DcaDay: 9}})
	l, _ := h.ListHoldings(ctx, &pb.ListHoldingsRequest{})
	if len(l.Holdings) != 2 || l.Holdings[0].Symbol != "AAA" {
		t.Fatalf("order: %v", l.Holdings)
	}
	arch, _ := h.ListHoldings(ctx, &pb.ListHoldingsRequest{IncludeArchived: true})
	if len(arch.Holdings) != len(l.Holdings)+1 {
		t.Fatalf("archived listing: %d vs %d", len(arch.Holdings), len(l.Holdings))
	}
}
