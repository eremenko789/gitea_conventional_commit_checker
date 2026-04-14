package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root service configuration loaded from YAML.
type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Gitea        GiteaConfig        `yaml:"gitea"`
	Check        CheckConfig        `yaml:"check"`
	Repositories []RepositoryConfig `yaml:"repositories"`

	repoIndex map[string]int // lowercased full name -> index
}

// ServerConfig controls HTTP and worker pool.
type ServerConfig struct {
	Listen        string        `yaml:"listen"`
	WebhookSecret string        `yaml:"webhook_secret"`
	QueueSize     int           `yaml:"queue_size"`
	Workers       int           `yaml:"workers"`
	GiteaTimeout  time.Duration `yaml:"gitea_timeout"`
}

// GiteaConfig is API client settings.
type GiteaConfig struct {
	BaseURL string `yaml:"base_url"`
	Token   string `yaml:"token"`
}

// CheckConfig drives validation and commit status text.
type CheckConfig struct {
	Context             string        `yaml:"context"`
	TargetURLTemplate   string        `yaml:"target_url_template"`
	DescriptionSuccess  string        `yaml:"description_success"`
	DescriptionFailure  string        `yaml:"description_failure"`
	DescriptionPending  string        `yaml:"description_pending"`
	PRCommentEnabled    *bool         `yaml:"pr_comment_enabled,omitempty"`
	PRCommentSuccess    string        `yaml:"pr_comment_success"`
	PRCommentFailure    string        `yaml:"pr_comment_failure"`
	PRCommentMaxRunes   int           `yaml:"pr_comment_max_runes"`
	StatusOnEachCommit  *bool         `yaml:"status_on_each_commit,omitempty"`
	AllowedTypes        []string      `yaml:"allowed_types"`
	SkipMergeCommits    *bool         `yaml:"skip_merge_commits,omitempty"` // nil = default true (TZ)
	DescriptionMaxRunes int           `yaml:"description_max_runes"`
	HTTPRetries         int           `yaml:"http_retries"`
	HTTPRetryBaseDelay  time.Duration `yaml:"http_retry_base_delay"`
}

// RepositoryConfig whitelists a repo and optional per-repo check overrides.
type RepositoryConfig struct {
	Name  string       `yaml:"name"`
	Check *CheckConfig `yaml:"check,omitempty"`
}

// SkipMerge returns whether merge/revert-style commits are excluded from validation.
func (c CheckConfig) SkipMerge() bool {
	if c.SkipMergeCommits == nil {
		return true
	}
	return *c.SkipMergeCommits
}

// StatusEachCommit reports whether status should be posted on every commit SHA.
func (c CheckConfig) StatusEachCommit() bool {
	if c.StatusOnEachCommit == nil {
		return false
	}
	return *c.StatusOnEachCommit
}

// PRCommentsOn reports whether a rendered check summary is posted as a PR comment.
func (c CheckConfig) PRCommentsOn() bool {
	if c.PRCommentEnabled == nil {
		return true
	}
	return *c.PRCommentEnabled
}

// Load reads YAML from path and validates it.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Server.Listen == "" {
		c.Server.Listen = ":8080"
	}
	if c.Server.QueueSize <= 0 {
		c.Server.QueueSize = 100
	}
	if c.Server.Workers <= 0 {
		c.Server.Workers = 4
	}
	if c.Server.GiteaTimeout == 0 {
		c.Server.GiteaTimeout = 30 * time.Second
	}
	if c.Check.DescriptionMaxRunes <= 0 {
		c.Check.DescriptionMaxRunes = 4096
	}
	if c.Check.HTTPRetries <= 0 {
		c.Check.HTTPRetries = 3
	}
	if c.Check.HTTPRetryBaseDelay == 0 {
		c.Check.HTTPRetryBaseDelay = 300 * time.Millisecond
	}
	if c.Check.PRCommentMaxRunes <= 0 {
		c.Check.PRCommentMaxRunes = 65535
	}
	if c.Check.PRCommentsOn() {
		if strings.TrimSpace(c.Check.PRCommentSuccess) == "" {
			c.Check.PRCommentSuccess = defaultPRCommentSuccess
		}
		if strings.TrimSpace(c.Check.PRCommentFailure) == "" {
			c.Check.PRCommentFailure = defaultPRCommentFailure
		}
	}
}

func (c *Config) validate() error {
	if c.Gitea.BaseURL == "" {
		return errors.New("gitea.base_url is required")
	}
	if c.Gitea.Token == "" {
		return errors.New("gitea.token is required")
	}
	if strings.TrimSpace(c.Check.Context) == "" {
		return errors.New("check.context is required")
	}
	if len(c.Repositories) == 0 {
		return errors.New("at least one repositories entry is required")
	}
	c.repoIndex = make(map[string]int)
	for i, r := range c.Repositories {
		name := strings.TrimSpace(strings.ToLower(r.Name))
		if name == "" {
			return fmt.Errorf("repositories[%d].name is empty", i)
		}
		if _, dup := c.repoIndex[name]; dup {
			return fmt.Errorf("duplicate repository: %s", r.Name)
		}
		c.repoIndex[name] = i
	}
	if err := validateDescriptionTemplates(c.Check); err != nil {
		return err
	}
	if err := validatePRCommentTemplates(c.Check); err != nil {
		return err
	}
	for _, r := range c.Repositories {
		eff := c.EffectiveCheck(r.Name)
		if err := validateDescriptionTemplates(eff); err != nil {
			return fmt.Errorf("effective check for %q: %w", r.Name, err)
		}
		if err := validatePRCommentTemplates(eff); err != nil {
			return fmt.Errorf("effective check for %q: %w", r.Name, err)
		}
	}
	return nil
}

// EffectiveCheck merges global check with optional per-repo overrides (non-empty / non-nil fields).
func (c *Config) EffectiveCheck(repoFullName string) CheckConfig {
	base := c.Check
	key := strings.TrimSpace(strings.ToLower(repoFullName))
	idx, ok := c.repoIndex[key]
	if !ok || c.Repositories[idx].Check == nil {
		return normalizeCheck(base)
	}
	ov := c.Repositories[idx].Check
	out := base
	if strings.TrimSpace(ov.Context) != "" {
		out.Context = ov.Context
	}
	if strings.TrimSpace(ov.TargetURLTemplate) != "" {
		out.TargetURLTemplate = ov.TargetURLTemplate
	}
	if strings.TrimSpace(ov.DescriptionSuccess) != "" {
		out.DescriptionSuccess = ov.DescriptionSuccess
	}
	if strings.TrimSpace(ov.DescriptionFailure) != "" {
		out.DescriptionFailure = ov.DescriptionFailure
	}
	if strings.TrimSpace(ov.DescriptionPending) != "" {
		out.DescriptionPending = ov.DescriptionPending
	}
	if ov.PRCommentEnabled != nil {
		out.PRCommentEnabled = ov.PRCommentEnabled
	}
	if strings.TrimSpace(ov.PRCommentSuccess) != "" {
		out.PRCommentSuccess = ov.PRCommentSuccess
	}
	if strings.TrimSpace(ov.PRCommentFailure) != "" {
		out.PRCommentFailure = ov.PRCommentFailure
	}
	if ov.PRCommentMaxRunes > 0 {
		out.PRCommentMaxRunes = ov.PRCommentMaxRunes
	}
	if ov.DescriptionMaxRunes > 0 {
		out.DescriptionMaxRunes = ov.DescriptionMaxRunes
	}
	if ov.HTTPRetries > 0 {
		out.HTTPRetries = ov.HTTPRetries
	}
	if ov.HTTPRetryBaseDelay > 0 {
		out.HTTPRetryBaseDelay = ov.HTTPRetryBaseDelay
	}
	if ov.SkipMergeCommits != nil {
		out.SkipMergeCommits = ov.SkipMergeCommits
	}
	if ov.StatusOnEachCommit != nil {
		out.StatusOnEachCommit = ov.StatusOnEachCommit
	}
	if len(ov.AllowedTypes) > 0 {
		out.AllowedTypes = append([]string(nil), ov.AllowedTypes...)
	}
	return normalizeCheck(out)
}

func normalizeCheck(ch CheckConfig) CheckConfig {
	if len(ch.AllowedTypes) == 0 {
		ch.AllowedTypes = append([]string(nil), defaultAllowedTypes...)
	}
	return ch
}

func validateDescriptionTemplates(ch CheckConfig) error {
	if strings.TrimSpace(ch.DescriptionSuccess) == "" {
		return errors.New("check.description_success must be non-empty")
	}
	if strings.TrimSpace(ch.DescriptionFailure) == "" {
		return errors.New("check.description_failure must be non-empty")
	}
	for _, tc := range []struct {
		name string
		tmpl string
		skip bool
	}{
		{"description_success", ch.DescriptionSuccess, false},
		{"description_failure", ch.DescriptionFailure, false},
		{"description_pending", ch.DescriptionPending, strings.TrimSpace(ch.DescriptionPending) == ""},
	} {
		if tc.skip {
			continue
		}
		t, err := template.New(tc.name).Parse(tc.tmpl)
		if err != nil {
			return fmt.Errorf("%s: %w", tc.name, err)
		}
		var buf bytes.Buffer
		data := sampleTemplateData()
		if err := t.Execute(&buf, data); err != nil {
			return fmt.Errorf("%s execute: %w", tc.name, err)
		}
		if strings.TrimSpace(buf.String()) == "" {
			return fmt.Errorf("%s renders to an empty string", tc.name)
		}
	}
	if ch.TargetURLTemplate != "" {
		t, err := template.New("target_url_template").Parse(ch.TargetURLTemplate)
		if err != nil {
			return fmt.Errorf("target_url_template: %w", err)
		}
		var buf bytes.Buffer
		if err := t.Execute(&buf, sampleTemplateData()); err != nil {
			return fmt.Errorf("target_url_template execute: %w", err)
		}
	}
	return nil
}

func validatePRCommentTemplates(ch CheckConfig) error {
	if !ch.PRCommentsOn() {
		return nil
	}
	if strings.TrimSpace(ch.PRCommentSuccess) == "" {
		return errors.New("check.pr_comment_success must be non-empty when pr_comment_enabled is true")
	}
	if strings.TrimSpace(ch.PRCommentFailure) == "" {
		return errors.New("check.pr_comment_failure must be non-empty when pr_comment_enabled is true")
	}
	for _, tc := range []struct {
		name string
		tmpl string
	}{
		{"pr_comment_success", ch.PRCommentSuccess},
		{"pr_comment_failure", ch.PRCommentFailure},
	} {
		t, err := template.New(tc.name).Parse(tc.tmpl)
		if err != nil {
			return fmt.Errorf("%s: %w", tc.name, err)
		}
		var buf bytes.Buffer
		data := sampleTemplateData()
		if err := t.Execute(&buf, data); err != nil {
			return fmt.Errorf("%s execute: %w", tc.name, err)
		}
		if strings.TrimSpace(buf.String()) == "" {
			return fmt.Errorf("%s renders to an empty string", tc.name)
		}
	}
	return nil
}

// TemplateData is passed to text/template for descriptions and target URLs.
type TemplateData struct {
	PRNumber               int
	PRTitle                string
	RepoFullName           string
	Owner                  string
	Repo                   string
	HeadSHA                string
	HeadShortSHA           string
	InvalidCommits         string
	InvalidCommitsMarkdown string
	InvalidCommitsList     []InvalidCommitEntry
	BadCount               int
	GoodCount              int
	TotalChecked           int
}

// InvalidCommitEntry is one failing commit for templates.
type InvalidCommitEntry struct {
	ShortSHA string
	FullSHA  string
	Subject  string
}

func sampleTemplateData() TemplateData {
	return TemplateData{
		PRNumber:               1,
		PRTitle:                "sample",
		RepoFullName:           "org/repo",
		Owner:                  "org",
		Repo:                   "repo",
		HeadSHA:                "deadbeefcafebabe000000000000000000000000",
		HeadShortSHA:           "deadbee",
		InvalidCommits:         "abc1234: bad subject\ndef5678: another bad",
		InvalidCommitsMarkdown: "- `abc1234` — bad subject\n- `def5678` — another bad",
		InvalidCommitsList: []InvalidCommitEntry{
			{ShortSHA: "abc1234", FullSHA: "abc1234deadbeef", Subject: "bad subject"},
			{ShortSHA: "def5678", FullSHA: "def5678cafebabe", Subject: "another bad"},
		},
		BadCount:     2,
		GoodCount:    1,
		TotalChecked: 3,
	}
}

var defaultAllowedTypes = []string{
	"feat", "fix", "docs", "style", "refactor", "perf", "test", "build", "ci", "chore", "revert",
}

const defaultPRCommentSuccess = "**Conventional Commits:** все коммиты в PR соответствуют конвенции.\n\n" +
	"Последний проверенный коммит: `{{ .HeadShortSHA }}` (`{{ .HeadSHA }}`)"

const defaultPRCommentFailure = "**Conventional Commits:** есть коммиты вне конвенции.\n\n" +
	"Последний проверенный коммит: `{{ .HeadShortSHA }}` (`{{ .HeadSHA }}`)\n\n" +
	"Проблемные коммиты:\n{{ .InvalidCommitsMarkdown }}"

// AllowedTypeSet builds a set from check config (after normalization).
func (c CheckConfig) AllowedTypeSet() map[string]struct{} {
	m := make(map[string]struct{}, len(c.AllowedTypes))
	for _, t := range c.AllowedTypes {
		m[strings.ToLower(strings.TrimSpace(t))] = struct{}{}
	}
	return m
}
