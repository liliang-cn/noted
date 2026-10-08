package ai

import "testing"

func TestPickRefsPrefersWhatTheAnswerNames(t *testing.T) {
	seen := []Ref{
		{Kind: "task", ID: "1", Title: "提交周报"},
		{Kind: "task", ID: "2", Title: "提交 Q4 预算"},
		{Kind: "task", ID: "3", Title: "招聘 JD 定稿"},
		{Kind: "task", ID: "1", Title: "提交周报"}, // read twice
	}
	got := pickRefs(seen, "还有两项:提交周报,以及提交 Q4 预算。")
	if len(got) != 2 || got[0].ID != "1" || got[1].ID != "2" {
		t.Fatalf("%+v", got)
	}
}

func TestPickRefsIsCaseInsensitiveAndFallsBackToTheLatest(t *testing.T) {
	seen := []Ref{{Kind: "note", ID: "a", Title: "Roadmap"}}
	if got := pickRefs(seen, "see the ROADMAP note"); len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	// The answer names nothing: show the last few things read rather than leave it bare.
	var many []Ref
	for i := 0; i < 9; i++ {
		many = append(many, Ref{Kind: "event", ID: string(rune('a' + i)), Title: "event " + string(rune('a'+i))})
	}
	got := pickRefs(many, "a summary that quotes nothing")
	if len(got) != 5 || got[4].ID != "i" {
		t.Fatalf("%+v", got)
	}
	if pickRefs(nil, "anything") != nil {
		t.Fatal("nothing read, nothing to show")
	}
}

func TestPickRefsIgnoresBlankTitlesAndCapsTheList(t *testing.T) {
	seen := []Ref{{Kind: "note", ID: "x", Title: "  "}}
	for i := 0; i < 20; i++ {
		seen = append(seen, Ref{Kind: "task", ID: string(rune('a' + i)), Title: "t" + string(rune('a'+i))})
	}
	reply := ""
	for _, r := range seen[1:] {
		reply += r.Title + " "
	}
	got := pickRefs(seen, reply)
	if len(got) != maxRefs {
		t.Fatalf("len = %d, want %d", len(got), maxRefs)
	}
	for _, r := range got {
		if r.ID == "x" {
			t.Fatal("a blank title must never match")
		}
	}
}
