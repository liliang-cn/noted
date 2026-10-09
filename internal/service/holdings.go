package service

import (
	"context"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Holdings struct {
	pb.UnimplementedHoldingServiceServer
	Store    *store.Store
	Location *time.Location // zone a holding's plan day follows unless it names another
}

func (s *Holdings) zone(in string) string {
	if in != "" {
		return in
	}
	if s.Location != nil {
		return s.Location.String()
	}
	return "UTC"
}

func holdingToPB(v store.HoldingView) *pb.Holding {
	h, p := v.Holding, v.Position
	out := &pb.Holding{
		Id: h.ID, Symbol: h.Symbol, Name: h.Name, Currency: h.Currency, Notes: h.Notes, LastPrice: h.LastPrice, LastPriceTime: tsPtr(h.PriceTime),
		DcaAmount: h.DCAAmount, DcaDay: int32(h.DCADay), DcaTime: h.DCATime, DcaEventId: h.DCAEvent, TimeZone: h.TimeZone, Archived: h.Archived,
		CreateTime: timestamppb.New(h.Created), UpdateTime: timestamppb.New(h.Updated),
		Position: &pb.Position{
			Shares: p.Shares, AvgCost: p.AvgCost, CostBasis: p.CostBasis, RealizedPnl: p.RealizedPnL, TotalBought: p.TotalBought, Trades: int32(p.Trades),
			HasPrice: p.HasPrice, MarketValue: p.MarketValue, UnrealizedPnl: p.UnrealizedPnL, UnrealizedPercent: p.UnrealizedPct,
			Dca: &pb.DcaStatus{Active: p.DCA.Active, DoneThisMonth: p.DCA.DoneThisMonth, InvestedThisMonth: p.DCA.InvestedMonth, StreakMonths: int32(p.DCA.StreakMonths)},
		},
	}
	if p.DCA.Active {
		out.Position.Dca.NextTime = timestamppb.New(p.DCA.Next)
	}
	return out
}

func (s *Holdings) view(ctx context.Context, userID, id string) (*pb.Holding, error) {
	v, err := s.Store.ViewHolding(ctx, userID, id, time.Now())
	if err != nil {
		return nil, toStatus(err)
	}
	return holdingToPB(v), nil
}

func (s *Holdings) CreateHolding(ctx context.Context, req *pb.CreateHoldingRequest) (*pb.Holding, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetHolding()
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "holding is required")
	}
	h, err := s.Store.CreateHolding(ctx, store.Holding{
		UserID: u, Symbol: in.Symbol, Name: in.Name, Currency: in.Currency, Notes: in.Notes, LastPrice: in.LastPrice,
		DCAAmount: in.DcaAmount, DCADay: int(in.DcaDay), DCATime: in.DcaTime, TimeZone: s.zone(in.TimeZone), Archived: in.Archived,
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return s.view(ctx, u, h.ID)
}

func (s *Holdings) GetHolding(ctx context.Context, req *pb.GetHoldingRequest) (*pb.Holding, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	return s.view(ctx, u, req.GetId())
}

func (s *Holdings) UpdateHolding(ctx context.Context, req *pb.UpdateHoldingRequest) (*pb.Holding, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetHolding()
	if in.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "holding.id is required")
	}
	paths, err := maskPaths(req.UpdateMask, "name", "currency", "notes", "last_price", "dca_amount", "dca_day", "dca_time", "time_zone", "archived")
	if err != nil {
		return nil, err
	}
	var p store.HoldingPatch
	if paths["name"] {
		p.Name = &in.Name
	}
	if paths["currency"] {
		p.Currency = &in.Currency
	}
	if paths["notes"] {
		p.Notes = &in.Notes
	}
	if paths["last_price"] {
		p.LastPrice = &in.LastPrice
	}
	if paths["dca_amount"] {
		p.DCAAmount = &in.DcaAmount
	}
	if paths["dca_day"] {
		d := int(in.DcaDay)
		p.DCADay = &d
	}
	if paths["dca_time"] {
		p.DCATime = &in.DcaTime
	}
	if paths["time_zone"] {
		z := s.zone(in.TimeZone)
		p.TimeZone = &z
	}
	if paths["archived"] {
		p.Archived = &in.Archived
	}
	if _, err := s.Store.UpdateHolding(ctx, u, in.Id, p); err != nil {
		return nil, toStatus(err)
	}
	return s.view(ctx, u, in.Id)
}

func (s *Holdings) DeleteHolding(ctx context.Context, req *pb.DeleteHoldingRequest) (*emptypb.Empty, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteHolding(ctx, u, req.GetId()); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *Holdings) ListHoldings(ctx context.Context, req *pb.ListHoldingsRequest) (*pb.ListHoldingsResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	vs, err := s.Store.ListHoldings(ctx, u, req.IncludeArchived, time.Now())
	if err != nil {
		return nil, toStatus(err)
	}
	out := &pb.ListHoldingsResponse{}
	for _, v := range vs {
		out.Holdings = append(out.Holdings, holdingToPB(v))
	}
	return out, nil
}

func (s *Holdings) RecordTrade(ctx context.Context, req *pb.RecordTradeRequest) (*pb.Holding, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetTrade()
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "trade is required")
	}
	t := store.Trade{Side: in.Side, Shares: in.Shares, Price: in.Price, Fee: in.Fee, Note: in.Note}
	if at := timePtr(in.Time); at != nil {
		t.Time = *at
	}
	if _, err := s.Store.RecordTrade(ctx, u, req.GetHoldingId(), t); err != nil {
		return nil, toStatus(err)
	}
	return s.view(ctx, u, req.GetHoldingId())
}

func (s *Holdings) DeleteTrade(ctx context.Context, req *pb.DeleteTradeRequest) (*emptypb.Empty, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteTrade(ctx, u, req.GetId()); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *Holdings) ListTrades(ctx context.Context, req *pb.ListTradesRequest) (*pb.ListTradesResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	ts, err := s.Store.ListTrades(ctx, u, req.GetHoldingId())
	if err != nil {
		return nil, toStatus(err)
	}
	out := &pb.ListTradesResponse{}
	for _, t := range ts {
		out.Trades = append(out.Trades, &pb.Trade{Id: t.ID, HoldingId: t.HoldingID, Time: timestamppb.New(t.Time), Side: t.Side, Shares: t.Shares, Price: t.Price, Fee: t.Fee, Note: t.Note})
	}
	return out, nil
}
