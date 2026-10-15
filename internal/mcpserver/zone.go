package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LocalZone names the system's time zone. Go calls time.Local "Local", a name
// that means nothing on the server: stored as an event's or goal's zone it would
// be read in whatever zone the server runs in, and other clients cannot read it
// at all. The IANA name comes from TZ or from where /etc/localtime points.
func LocalZone() string {
	if tz := strings.TrimPrefix(os.Getenv("TZ"), ":"); tz != "" && tz != "Local" {
		if _, err := time.LoadLocation(tz); err == nil {
			return tz
		}
	}
	if p, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if i := strings.LastIndex(p, "zoneinfo/"); i >= 0 {
			name := p[i+len("zoneinfo/"):]
			if _, err := time.LoadLocation(name); err == nil {
				return name
			}
		}
	}
	return "UTC" // what Go takes time.Local to be when it cannot find the zone either
}
