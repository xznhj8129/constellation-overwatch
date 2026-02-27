package logger

import (
	"fmt"
	"time"
)

// PrintStartupBanner prints NATS-style version/commit/date info lines.
// The CH logo is now rendered by the splash screen in main.go.
func PrintStartupBanner(version, commit, date string) {
	lines := []string{
		"",
		"Starting Constellation Overwatch",
		"",
		fmt.Sprintf("  Version:  %s", version),
		fmt.Sprintf("  Commit:   %s", commit),
		fmt.Sprintf("  Built:    %s", date),
		"",
	}

	timestamp := time.Now().Format("2006/01/02 15:04:05.000000")
	for _, line := range lines {
		fmt.Printf("[%d] %s %s[INF]%s %s\n", pid, timestamp, colorGreen, colorReset, line)
	}
}
