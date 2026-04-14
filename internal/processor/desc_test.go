package processor

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/eremenko789/gitea_conventional_commit_checker/internal/config"
)

func TestRenderDescription_multipleInvalid(t *testing.T) {
	inv := []config.InvalidCommitEntry{
		{ShortSHA: "aaaaaaaa", FullSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Subject: "wip"},
		{ShortSHA: "bbbbbbbb", FullSHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Subject: "fix things"},
	}
	data := config.TemplateData{
		PRNumber:           42,
		PRTitle:            "My PR",
		RepoFullName:       "org/repo",
		Owner:              "org",
		Repo:               "repo",
		InvalidCommits:     formatInvalidLines(inv),
		InvalidCommitsList: inv,
		BadCount:           2,
		GoodCount:          1,
		TotalChecked:       3,
	}
	tmpl := "Invalid ({{ .BadCount }}): {{ .InvalidCommits }}"
	out, err := renderDescription(tmpl, data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "aaaaaaa:") {
		t.Fatalf("missing first sha: %q", out)
	}
	if !strings.Contains(out, "bbbbbbb:") {
		t.Fatalf("missing second sha: %q", out)
	}
}

func TestFormatInvalidMarkdown(t *testing.T) {
	inv := []config.InvalidCommitEntry{
		{FullSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Subject: "wip\n\nbody"},
		{FullSHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Subject: "  spaced "},
	}
	got := formatInvalidMarkdown(inv)
	if !strings.Contains(got, "- `aaaaaaa`") || !strings.Contains(got, "wip") {
		t.Fatalf("first line: %q", got)
	}
	if !strings.Contains(got, "- `bbbbbbb`") || !strings.Contains(got, "spaced") {
		t.Fatalf("second line: %q", got)
	}
}

func TestTruncateRunes_keepsShort(t *testing.T) {
	s := strings.Repeat("а", 10) // 10 runes
	got := truncateRunes(s, 20)
	if got != s {
		t.Fatalf("got %q", got)
	}
}

func TestTruncateRunes_unicode(t *testing.T) {
	s := strings.Repeat("аб", 600) // 1200 runes
	got := truncateRunes(s, 40)
	if utf8.RuneCountInString(got) > 40 {
		t.Fatalf("len %d", utf8.RuneCountInString(got))
	}
	if !strings.HasSuffix(got, " …") {
		t.Fatalf("suffix: %q", got)
	}
}
