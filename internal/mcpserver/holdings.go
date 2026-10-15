package mcpserver

import (
	"context"
	"fmt"
	"strings"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type listHoldingsIn struct {
	IncludeArchived bool `json:"include_archived,omitempty"`
}
type createHoldingIn struct {
	Symbol    string   `json:"symbol" jsonschema:"ticker, e.g. VOO"`
	Name      string   `json:"name,omitempty"`
	Currency  string   `json:"currency,omitempty" jsonschema:"default USD"`
	Notes     string   `json:"notes,omitempty"`
	LastPrice *float64 `json:"last_price,omitempty" jsonschema:"the latest price the user knows; noted does not fetch prices"`
	DCAAmount float64  `json:"dca_amount,omitempty" jsonschema:"monthly buying plan: amount per month, in currency"`
	DCADay    int32    `json:"dca_day,omitempty" jsonschema:"monthly buying plan: day of the month, 1-28; adds a monthly reminder event"`
	DCATime   string   `json:"dca_time,omitempty" jsonschema:"monthly buying plan: HH:MM, default 09:00"`
	TimeZone  string   `json:"time_zone,omitempty"`
}
type updateHoldingIn struct {
	ID         string   `json:"id"`
	Name       *string  `json:"name,omitempty"`
	Currency   *string  `json:"currency,omitempty"`
	Notes      *string  `json:"notes,omitempty"`
	LastPrice  *float64 `json:"last_price,omitempty"`
	ClearPrice bool     `json:"clear_price,omitempty"`
	DCAAmount  *float64 `json:"dca_amount,omitempty"`
	DCADay     *int32   `json:"dca_day,omitempty" jsonschema:"1-28; 0 ends the monthly plan"`
	DCATime    *string  `json:"dca_time,omitempty" jsonschema:"HH:MM"`
	Archived   *bool    `json:"archived,omitempty"`
}
type tradeIn struct {
	HoldingID string  `json:"holding_id,omitempty" jsonschema:"from list_holdings; or give symbol instead"`
	Symbol    string  `json:"symbol,omitempty" jsonschema:"ticker such as VOO, used when holding_id is not given; for a buy, a holding is created if the user has none"`
	Side      string  `json:"side" jsonschema:"buy or sell"`
	Shares    float64 `json:"shares" jsonschema:"number of shares (units), not money"`
	Price     float64 `json:"price" jsonschema:"price per share, in the holding's currency"`
	Fee       float64 `json:"fee,omitempty"`
	Time      string  `json:"time,omitempty" jsonschema:"RFC 3339; default now"`
	Note      string  `json:"note,omitempty"`
}
type holdingIDIn struct {
	HoldingID string `json:"holding_id"`
}

// registerHoldings adds investments: holdings, the trades made in them, and the
// monthly buying plan. noted records and reports; it never fetches prices.
func (t *tools) registerHoldings(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "list_holdings", Description: "The user's investments (e.g. VOO) with their position: shares, average cost, realized and unrealized gain at the last entered price, and the monthly buying plan's status.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listHoldingsIn) (*mcp.CallToolResult, *listOut[*holdingOut], error) {
			r, err := t.holds.ListHoldings(ctx, &pb.ListHoldingsRequest{IncludeArchived: in.IncludeArchived})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, &listOut[*holdingOut]{Items: items(r.Holdings, t.holding)})
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get_holding", Description: "One investment with its position.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *holdingOut, error) {
			r, err := t.holds.GetHolding(ctx, &pb.GetHoldingRequest{Id: in.ID})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.holding(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "create_holding", Description: "Start tracking an investment, optionally with a monthly buying plan (dca_amount on dca_day). To record a purchase, use record_trade, which also creates the holding when needed.", Annotations: additive},
		func(ctx context.Context, _ *mcp.CallToolRequest, in createHoldingIn) (*mcp.CallToolResult, *holdingOut, error) {
			tz := in.TimeZone
			if tz == "" {
				tz = t.loc.String()
			}
			r, err := t.holds.CreateHolding(ctx, &pb.CreateHoldingRequest{Holding: &pb.Holding{
				Symbol: in.Symbol, Name: in.Name, Currency: in.Currency, Notes: in.Notes, LastPrice: in.LastPrice,
				DcaAmount: in.DCAAmount, DcaDay: in.DCADay, DcaTime: in.DCATime, TimeZone: tz}})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.holding(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "update_holding", Description: "Change an investment: enter its latest price, change or end the monthly plan, archive it; omitted fields stay as they are.", Annotations: editing},
		func(ctx context.Context, _ *mcp.CallToolRequest, in updateHoldingIn) (*mcp.CallToolResult, *holdingOut, error) {
			h, mask := &pb.Holding{Id: in.ID}, []string{}
			if in.Name != nil {
				h.Name, mask = *in.Name, append(mask, "name")
			}
			if in.Currency != nil {
				h.Currency, mask = *in.Currency, append(mask, "currency")
			}
			if in.Notes != nil {
				h.Notes, mask = *in.Notes, append(mask, "notes")
			}
			if in.LastPrice != nil || in.ClearPrice {
				if !in.ClearPrice {
					h.LastPrice = in.LastPrice
				}
				mask = append(mask, "last_price")
			}
			if in.DCAAmount != nil {
				h.DcaAmount, mask = *in.DCAAmount, append(mask, "dca_amount")
			}
			if in.DCADay != nil {
				h.DcaDay, mask = *in.DCADay, append(mask, "dca_day")
			}
			if in.DCATime != nil {
				h.DcaTime, mask = *in.DCATime, append(mask, "dca_time")
			}
			if in.Archived != nil {
				h.Archived, mask = *in.Archived, append(mask, "archived")
			}
			if len(mask) == 0 {
				return nil, nil, fmt.Errorf("nothing to update: pass at least one field")
			}
			r, err := t.holds.UpdateHolding(ctx, &pb.UpdateHoldingRequest{Holding: h, UpdateMask: &fieldmaskpb.FieldMask{Paths: mask}})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.holding(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "delete_holding", Description: "Permanently delete an investment with all its trades and its monthly reminder.", Annotations: deleting},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *deletedOut, error) {
			if _, err := t.holds.DeleteHolding(ctx, &pb.DeleteHoldingRequest{Id: in.ID}); err != nil {
				return nil, nil, err
			}
			return deleted("holding", "holding", in.ID)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "record_trade", Description: "Record a buy or sell: \"bought 10 VOO at 520\" is side buy, shares 10, price 520. Give holding_id, or just the symbol (a buy of a new symbol creates its holding). Returns the holding with its new position.", Annotations: additive},
		func(ctx context.Context, _ *mcp.CallToolRequest, in tradeIn) (*mcp.CallToolResult, *holdingOut, error) {
			at, err := t.optWhen(in.Time)
			if err != nil {
				return nil, nil, err
			}
			side := strings.ToLower(strings.TrimSpace(in.Side))
			if side != "buy" && side != "sell" {
				return nil, nil, fmt.Errorf("side must be buy or sell")
			}
			if in.Shares <= 0 || in.Price < 0 {
				return nil, nil, fmt.Errorf("shares must be above 0 and price not negative")
			}
			id := in.HoldingID
			if id == "" {
				if id, err = t.holdingFor(ctx, in.Symbol, side == "buy"); err != nil {
					return nil, nil, err
				}
			}
			r, err := t.holds.RecordTrade(ctx, &pb.RecordTradeRequest{HoldingId: id, Trade: &pb.Trade{
				Time: at, Side: side, Shares: in.Shares, Price: in.Price, Fee: in.Fee, Note: in.Note}})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.holding(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list_trades", Description: "An investment's buys and sells, oldest first.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in holdingIDIn) (*mcp.CallToolResult, *listOut[*tradeOut], error) {
			r, err := t.holds.ListTrades(ctx, &pb.ListTradesRequest{HoldingId: in.HoldingID})
			if err != nil {
				return nil, nil, err
			}
			h, err := t.holds.GetHolding(ctx, &pb.GetHoldingRequest{Id: in.HoldingID})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, &listOut[*tradeOut]{Items: items(r.Trades, func(tr *pb.Trade) *tradeOut { return t.trade(tr, h.Currency) })})
		})
	mcp.AddTool(s, &mcp.Tool{Name: "delete_trade", Description: "Remove a recorded trade (id from list_trades).", Annotations: deleting},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *deletedOut, error) {
			if _, err := t.holds.DeleteTrade(ctx, &pb.DeleteTradeRequest{Id: in.ID}); err != nil {
				return nil, nil, err
			}
			return deleted("trade", "trade", in.ID)
		})
}

// holdingFor finds the user's holding of a symbol. For a buy it creates one when
// there is none, so "I bought 10 VOO" needs one call; nothing can be sold from
// a holding that does not exist yet.
func (t *tools) holdingFor(ctx context.Context, symbol string, create bool) (string, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return "", fmt.Errorf("give holding_id or symbol")
	}
	l, err := t.holds.ListHoldings(ctx, &pb.ListHoldingsRequest{IncludeArchived: true})
	if err != nil {
		return "", err
	}
	for _, h := range l.Holdings {
		if strings.EqualFold(h.Symbol, symbol) {
			return h.Id, nil
		}
	}
	if !create {
		return "", fmt.Errorf("no holding of %s", symbol)
	}
	h, err := t.holds.CreateHolding(ctx, &pb.CreateHoldingRequest{Holding: &pb.Holding{Symbol: symbol, TimeZone: t.loc.String()}})
	if err != nil {
		return "", err
	}
	return h.Id, nil
}
