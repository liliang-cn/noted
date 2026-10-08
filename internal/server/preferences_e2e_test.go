package server_test

import (
	"strings"
	"sync"
	"testing"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"google.golang.org/grpc/codes"
)

func TestPreferencesRoundTripAndVersions(t *testing.T) {
	e := start(t, nil, false)
	c := pb.NewPreferenceServiceClient(e.conn(t))
	ctx := as(e.alice)

	p, err := c.SetPreference(ctx, &pb.SetPreferenceRequest{Key: "ui.theme", Value: `{"id":"paper"}`})
	if err != nil || p.Version != 1 {
		t.Fatalf("%+v %v", p, err)
	}
	got, err := c.GetPreference(ctx, &pb.GetPreferenceRequest{Key: "ui.theme"})
	if err != nil || got.Value != `{"id":"paper"}` {
		t.Fatalf("%+v %v", got, err)
	}
	p, _ = c.SetPreference(ctx, &pb.SetPreferenceRequest{Key: "ui.theme", Value: `{"id":"moss"}`})
	if p.Version != 2 {
		t.Fatalf("version = %d", p.Version)
	}

	// Compare-and-swap: a device holding a stale version is refused.
	stale := int64(1)
	if _, err := c.SetPreference(ctx, &pb.SetPreferenceRequest{Key: "ui.theme", Value: `{}`, IfVersion: &stale}); code(err) != codes.Aborted {
		t.Fatalf("stale write: %v", err)
	}
	cur := int64(2)
	if p, err := c.SetPreference(ctx, &pb.SetPreferenceRequest{Key: "ui.theme", Value: `{"id":"daylight"}`, IfVersion: &cur}); err != nil || p.Version != 3 {
		t.Fatalf("matching write: %+v %v", p, err)
	}
	// if_version 0 means "must not exist yet".
	zero := int64(0)
	if _, err := c.SetPreference(ctx, &pb.SetPreferenceRequest{Key: "ui.theme", Value: `{}`, IfVersion: &zero}); code(err) != codes.Aborted {
		t.Fatalf("create over existing: %v", err)
	}
	if _, err := c.SetPreference(ctx, &pb.SetPreferenceRequest{Key: "ui.layout.work", Value: `{"modules":["tasks"]}`, IfVersion: &zero}); err != nil {
		t.Fatalf("create new: %v", err)
	}

	l, err := c.ListPreferences(ctx, &pb.ListPreferencesRequest{Prefix: "ui.layout"})
	if err != nil || len(l.Preferences) != 1 || l.Preferences[0].Key != "ui.layout.work" {
		t.Fatalf("%+v %v", l, err)
	}
	all, _ := c.ListPreferences(ctx, &pb.ListPreferencesRequest{})
	if len(all.Preferences) != 2 {
		t.Fatalf("%d", len(all.Preferences))
	}

	if _, err := c.DeletePreference(ctx, &pb.DeletePreferenceRequest{Key: "ui.theme"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetPreference(ctx, &pb.GetPreferenceRequest{Key: "ui.theme"}); code(err) != codes.NotFound {
		t.Fatalf("after delete: %v", err)
	}
}

func TestPreferencesValidationAndIsolation(t *testing.T) {
	e := start(t, nil, false)
	c := pb.NewPreferenceServiceClient(e.conn(t))
	ctx := as(e.alice)

	for name, req := range map[string]*pb.SetPreferenceRequest{
		"not JSON":     {Key: "ui.theme", Value: "paper"},
		"empty key":    {Key: "", Value: "{}"},
		"upper case":   {Key: "UI.Theme", Value: "{}"},
		"path-like":    {Key: "../etc", Value: "{}"},
		"too big":      {Key: "big", Value: `"` + strings.Repeat("x", 65<<10) + `"`},
		"key too long": {Key: strings.Repeat("a", 65), Value: "{}"},
	} {
		if _, err := c.SetPreference(ctx, req); code(err) != codes.InvalidArgument {
			t.Errorf("%s: %v", name, err)
		}
	}

	c.SetPreference(ctx, &pb.SetPreferenceRequest{Key: "ui.theme", Value: `"alice"`})
	if _, err := c.GetPreference(as(e.bob), &pb.GetPreferenceRequest{Key: "ui.theme"}); code(err) != codes.NotFound {
		t.Fatalf("bob read alice's preference: %v", err)
	}
	if l, _ := c.ListPreferences(as(e.bob), &pb.ListPreferencesRequest{}); len(l.Preferences) != 0 {
		t.Fatal("bob lists alice's preferences")
	}
	c.SetPreference(as(e.bob), &pb.SetPreferenceRequest{Key: "ui.theme", Value: `"bob"`})
	if got, _ := c.GetPreference(ctx, &pb.GetPreferenceRequest{Key: "ui.theme"}); got.Value != `"alice"` {
		t.Fatalf("bob overwrote alice's value: %s", got.Value)
	}
}

func TestPreferencesAreCappedPerUser(t *testing.T) {
	e := start(t, nil, false)
	c := pb.NewPreferenceServiceClient(e.conn(t))
	ctx := as(e.alice)
	for i := 0; i < 100; i++ {
		if _, err := c.SetPreference(ctx, &pb.SetPreferenceRequest{Key: "k" + strings.Repeat("a", i%30) + string(rune('a'+i/30)) + string(rune('a'+i%26)), Value: "1"}); err != nil {
			t.Fatalf("key %d: %v", i, err)
		}
	}
	if _, err := c.SetPreference(ctx, &pb.SetPreferenceRequest{Key: "one-too-many", Value: "1"}); code(err) != codes.InvalidArgument {
		t.Fatalf("101st key: %v", err)
	}
}

func TestConcurrentCompareAndSwapHasOneWinner(t *testing.T) {
	e := start(t, nil, false)
	c := pb.NewPreferenceServiceClient(e.conn(t))
	ctx := as(e.alice)
	c.SetPreference(ctx, &pb.SetPreferenceRequest{Key: "ui.layout", Value: `{"n":0}`})

	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := int64(1)
			if _, err := c.SetPreference(ctx, &pb.SetPreferenceRequest{Key: "ui.layout", Value: `{"n":1}`, IfVersion: &v}); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d writers won a compare-and-swap on the same version, want exactly 1", wins)
	}
}
