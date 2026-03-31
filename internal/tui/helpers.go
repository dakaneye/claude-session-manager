package tui

import "strings"

// padToHeight appends newlines until content reaches the target height in lines.
func padToHeight(content string, height int) string {
	rendered := strings.Count(content, "\n") + 1
	for rendered < height {
		content += "\n"
		rendered++
	}
	return content
}
