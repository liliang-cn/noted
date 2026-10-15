package mcpserver

import (
	"testing"
	"time"
)

func TestLocalZoneIsAnIANAName(t *testing.T) {
	t.Setenv("TZ", "Asia/Tokyo")
	if got := LocalZone(); got != "Asia/Tokyo" {
		t.Fatalf("TZ=Asia/Tokyo: %q", got)
	}
	t.Setenv("TZ", ":Europe/Berlin")
	if got := LocalZone(); got != "Europe/Berlin" {
		t.Fatalf("TZ=:Europe/Berlin: %q", got)
	}
	t.Setenv("TZ", "")
	got := LocalZone()
	if got == "Local" || got == "" {
		t.Fatalf("the system zone must have a real name, got %q", got)
	}
	if _, err := time.LoadLocation(got); err != nil {
		t.Fatalf("%q does not load: %v", got, err)
	}
}
