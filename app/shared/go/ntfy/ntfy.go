// Package ntfy implements notification templates.
package ntfy

import (
	"regexp"
	"strings"
)

var templatePattern = regexp.MustCompile(`{{\s*([a-zA-Z0-9_]+)\s*}}`)

func Render(template, fallback string, value func(string) string) string {
	template = strings.TrimSpace(template)
	if template == "" {
		template = fallback
	}
	return templatePattern.ReplaceAllStringFunc(template, func(match string) string { return value(strings.TrimSpace(match[2 : len(match)-2])) })
}
