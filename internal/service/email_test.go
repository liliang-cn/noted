package service

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestPasswordHashDefaults(t *testing.T) {
	h, err := hashPassword("correct horse", (&Account{}).cost())
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := bcrypt.Cost([]byte(h)); c != 12 {
		t.Fatalf("bcrypt cost %d, want 12", c)
	}
	if !passwordMatches(h, "correct horse") || passwordMatches(h, "correct horsE") {
		t.Fatal("password check")
	}
}

func TestNormalizeEmail(t *testing.T) {
	for in, want := range map[string]string{" A@B.co ": "a@b.co", "x.y+z@Mail.Example.org": "x.y+z@mail.example.org"} {
		if got, err := NormalizeEmail(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "a", "a@b", "@b.co", "a@.co", "a@b.", "Name <a@b.co>", "a b@c.co"} {
		if _, err := NormalizeEmail(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestTrustedClientIP(t *testing.T) {
	cases := []struct {
		remote string
		xff    []string
		want   string
	}{
		{"203.0.113.5:1234", []string{"1.2.3.4"}, "203.0.113.5"},      // public peer: header ignored
		{"100.104.1.2:5555", []string{"9.9.9.9, 8.8.8.8"}, "8.8.8.8"}, // proxy on the tailnet
		{"127.0.0.1:80", nil, "127.0.0.1"},                            // no header
		{"192.168.1.1:80", []string{"garbage"}, "192.168.1.1"},        // unparsable header
		{"[::1]:80", []string{"2001:db8::1"}, "2001:db8::1"},          // loopback IPv6
	}
	for _, c := range cases {
		if got := TrustedClientIP(c.remote, c.xff); got != c.want {
			t.Errorf("%s %v: %s, want %s", c.remote, c.xff, got, c.want)
		}
	}
}
