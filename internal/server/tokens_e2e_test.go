package server_test

import (
	"strings"
	"testing"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"google.golang.org/grpc/codes"
)

func TestTokensCreateListRevoke(t *testing.T) {
	e := start(t, nil, false)
	acc := pb.NewAccountServiceClient(e.conn(t))
	notes := pb.NewNoteServiceClient(e.conn(t))

	// The label is trimmed; the new token works at once and is not the caller.
	cr, err := acc.CreateToken(as(e.alice), &pb.CreateTokenRequest{Label: "  Claude Code  "})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cr.Token, "noted_") || cr.Info.Label != "Claude Code" || len(cr.Info.Id) != 12 ||
		cr.Info.Current || cr.Info.CreateTime == nil || cr.Info.LastUsedTime != nil {
		t.Fatalf("create: %+v", cr.Info)
	}
	if _, err := notes.ListNotes(as(cr.Token), &pb.ListNotesRequest{}); err != nil {
		t.Fatalf("new token: %v", err)
	}
	// Default and over-long labels.
	def, err := acc.CreateToken(as(e.alice), &pb.CreateTokenRequest{Label: "   "})
	if err != nil || def.Info.Label != "AI 助手" {
		t.Fatalf("default label: %v %+v", err, def.GetInfo())
	}
	if _, err := acc.CreateToken(as(e.alice), &pb.CreateTokenRequest{Label: strings.Repeat("长", 60)}); err != nil {
		t.Fatalf("60 characters must fit: %v", err)
	}
	if _, err := acc.CreateToken(as(e.alice), &pb.CreateTokenRequest{Label: strings.Repeat("x", 61)}); code(err) != codes.InvalidArgument {
		t.Fatalf("61 characters: %v", err)
	}

	// Alice sees her own four tokens, the calling one marked; bob sees only his.
	l, err := acc.ListTokens(as(e.alice), &pb.ListTokensRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Tokens) != 4 {
		t.Fatalf("alice has 4 tokens, listed %d", len(l.Tokens))
	}
	var current, created *pb.TokenInfo
	for _, ti := range l.Tokens {
		if ti.Current {
			if current != nil {
				t.Fatal("two tokens marked current")
			}
			current = ti
		}
		if ti.Id == cr.Info.Id {
			created = ti
		}
	}
	if current == nil || current.Label != "test" || current.LastUsedTime == nil {
		t.Fatalf("current token: %+v", current)
	}
	if created == nil || created.LastUsedTime == nil {
		t.Fatalf("the used new token should show when it was used: %+v", created)
	}
	bl, err := acc.ListTokens(as(e.bob), &pb.ListTokensRequest{})
	if err != nil || len(bl.Tokens) != 1 || bl.Tokens[0].Id == current.Id || !bl.Tokens[0].Current {
		t.Fatalf("bob's list: %v %+v", err, bl.GetTokens())
	}

	// Bob cannot revoke alice's token; the token keeps working.
	if _, err := acc.RevokeToken(as(e.bob), &pb.RevokeTokenRequest{Id: cr.Info.Id}); code(err) != codes.NotFound {
		t.Fatalf("bob revoking alice's token: %v", err)
	}
	if _, err := notes.ListNotes(as(cr.Token), &pb.ListNotesRequest{}); err != nil {
		t.Fatalf("token after bob's attempt: %v", err)
	}
	if _, err := acc.RevokeToken(as(e.alice), &pb.RevokeTokenRequest{}); code(err) != codes.InvalidArgument {
		t.Fatalf("empty id: %v", err)
	}

	// Alice revokes it: it stops working, and a second revoke finds nothing.
	if _, err := acc.RevokeToken(as(e.alice), &pb.RevokeTokenRequest{Id: cr.Info.Id}); err != nil {
		t.Fatal(err)
	}
	if _, err := notes.ListNotes(as(cr.Token), &pb.ListNotesRequest{}); code(err) != codes.Unauthenticated {
		t.Fatalf("revoked token: %v", err)
	}
	if _, err := acc.RevokeToken(as(e.alice), &pb.RevokeTokenRequest{Id: cr.Info.Id}); code(err) != codes.NotFound {
		t.Fatalf("second revoke: %v", err)
	}

	// Revoking the token making the call is allowed and takes effect at once.
	if _, err := acc.RevokeToken(as(e.alice), &pb.RevokeTokenRequest{Id: current.Id}); err != nil {
		t.Fatalf("revoke own current token: %v", err)
	}
	if _, err := acc.ListTokens(as(e.alice), &pb.ListTokensRequest{}); code(err) != codes.Unauthenticated {
		t.Fatalf("after revoking itself: %v", err)
	}
	if l, err := acc.ListTokens(as(def.Token), &pb.ListTokensRequest{}); err != nil || len(l.Tokens) != 2 {
		t.Fatalf("alice's remaining tokens: %v %d", err, len(l.GetTokens()))
	}
	// Bob is untouched.
	if _, err := notes.ListNotes(as(e.bob), &pb.ListNotesRequest{}); err != nil {
		t.Fatalf("bob: %v", err)
	}
}

func TestTokensNeedAuth(t *testing.T) {
	e := start(t, nil, false)
	acc := pb.NewAccountServiceClient(e.conn(t))
	if _, err := acc.CreateToken(as("noted_bogus"), &pb.CreateTokenRequest{}); code(err) != codes.Unauthenticated {
		t.Fatalf("create with a bad token: %v", err)
	}
	if _, err := acc.ListTokens(as("noted_bogus"), &pb.ListTokensRequest{}); code(err) != codes.Unauthenticated {
		t.Fatalf("list with a bad token: %v", err)
	}
}
