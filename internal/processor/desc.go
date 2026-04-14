package processor

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"unicode/utf8"

	"github.com/eremenko789/gitea_conventional_commit_checker/internal/config"
)

func renderDescription(tmplStr string, data config.TemplateData) (string, error) {
	t, err := template.New("desc").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	s := strings.TrimSpace(buf.String())
	if s == "" {
		return "", fmt.Errorf("template rendered empty description")
	}
	return s, nil
}

func renderTargetURL(tmplStr string, data config.TemplateData) (string, error) {
	tmplStr = strings.TrimSpace(tmplStr)
	if tmplStr == "" {
		return "", nil
	}
	t, err := template.New("target").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

// truncateRunes shortens s to at most max runes, adding an ellipsis if truncated.
func truncateRunes(s string, max int) string {
	if max <= 3 {
		return "…"
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	ellipsis := []rune(" …")
	out := max - len(ellipsis)
	if out < 1 {
		return string(ellipsis)
	}
	return string(r[:out]) + string(ellipsis)
}

func formatInvalidLines(entries []config.InvalidCommitEntry) string {
	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		sub := strings.TrimSpace(e.Subject)
		sub = strings.Split(sub, "\n")[0]
		fmt.Fprintf(&b, "%s: %s", shortSHA(e.FullSHA), sub)
	}
	return b.String()
}

// formatInvalidMarkdown renders a markdown bullet list: "- `short` — subject".
func formatInvalidMarkdown(entries []config.InvalidCommitEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		sub := strings.TrimSpace(e.Subject)
		sub = strings.Split(sub, "\n")[0]
		fmt.Fprintf(&b, "- `%s` — %s", shortSHA(e.FullSHA), sub)
	}
	return b.String()
}

func shortSHA(full string) string {
	full = strings.TrimSpace(full)
	if len(full) <= 7 {
		return full
	}
	return full[:7]
}
