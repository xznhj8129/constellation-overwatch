package logger

import (
	"fmt"
	"time"
)

// PrintStartupBanner prints the Constellation Overwatch ASCII banner in NATS style
func PrintStartupBanner(version, commit, date string) {
	lines := []string{
		"",
		"Starting Constellation Overwatch",
		"        _.--\"\"--._",
		"      .\"          \".",
		"     /   O      O   \\",
		"    |    \\  __  /    |      C4ISR Edge Server Mesh",
		"    |     \\/  \\/     |",
		"     \\     `--'     /",
		"      `.          .'",
		"        `-.____.-'",
		"",
		"          https://constellation-overwatch.dev",
		"",
		"--------- CONSTELLATION OVERWATCH ---------",
		fmt.Sprintf("  Version:       %s", version),
		fmt.Sprintf("  Commit:        %s", commit),
		fmt.Sprintf("  Built:         %s", date),
		"--------------------------------------------",
		"",
	}

	timestamp := time.Now().Format("2006/01/02 15:04:05.000000")
	for _, line := range lines {
		fmt.Printf("[%d] %s %s[INF]%s %s\n", pid, timestamp, colorGreen, colorReset, line)
	}
}
