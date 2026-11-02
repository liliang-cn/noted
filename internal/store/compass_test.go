package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func sp(s string) *string { return &s }

func TestCompassRevisions(t *testing.T) {
	ctx := context.Background()
	s, u := newStore(t)
	now := time.Now()

	// Nothing saved: an empty compass, life by default.
	c, err := s.GetCompass(ctx, u, "")
	if err != nil || c.Version != 0 || c.Space != SpaceLife || c.Vision != "" {
		t.Fatalf("empty: %+v %v", c, err)
	}
	if _, err := s.SetCompass(ctx, u, "", CompassPatch{}, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("saving nothing: %v", err)
	}

	c, err = s.SetCompass(ctx, u, "", CompassPatch{Vision: sp(" build things that last "), AntiVision: sp("a life of meetings"),
		Constraints: &[]string{"health", " ", "family"}}, now)
	if err != nil || c.Version != 1 || c.Vision != "build things that last" || len(c.Constraints) != 2 {
		t.Fatalf("first: %+v %v", c, err)
	}

	// A merge keeps what was not named and appends a version.
	c, err = s.SetCompass(ctx, u, "life", CompassPatch{Identity: sp("我是那种每天动手的人")}, now.Add(time.Minute))
	if err != nil || c.Version != 2 || c.Vision != "build things that last" || c.AntiVision != "a life of meetings" || c.Identity != "我是那种每天动手的人" || len(c.Constraints) != 2 {
		t.Fatalf("merge: %+v %v", c, err)
	}
	// Saving what is already current is not a new version.
	c, err = s.SetCompass(ctx, u, "life", CompassPatch{Vision: sp("build things that last")}, now.Add(2*time.Minute))
	if err != nil || c.Version != 2 {
		t.Fatalf("same: %+v %v", c, err)
	}
	// [] clears the constraints.
	c, err = s.SetCompass(ctx, u, "life", CompassPatch{Constraints: &[]string{}}, now.Add(3*time.Minute))
	if err != nil || c.Version != 3 || len(c.Constraints) != 0 {
		t.Fatalf("clear: %+v %v", c, err)
	}

	// Work is its own compass.
	w, err := s.GetCompass(ctx, u, SpaceWork)
	if err != nil || w.Version != 0 {
		t.Fatalf("work: %+v %v", w, err)
	}

	revs, err := s.ListCompassRevisions(ctx, u, "", 0)
	if err != nil || len(revs) != 3 || revs[0].Version != 3 || revs[2].Version != 1 || len(revs[2].Constraints) != 2 || revs[2].Identity != "" {
		t.Fatalf("history: %+v %v", revs, err)
	}

	if _, err := s.SetCompass(ctx, u, "mars", CompassPatch{Vision: sp("x")}, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad space: %v", err)
	}
	long := make([]rune, maxCompassText+1)
	for i := range long {
		long[i] = '字'
	}
	if _, err := s.SetCompass(ctx, u, "", CompassPatch{Vision: sp(string(long))}, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("too long: %v", err)
	}
}
