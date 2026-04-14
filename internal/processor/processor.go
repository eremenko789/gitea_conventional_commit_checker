package processor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/eremenko789/gitea_conventional_commit_checker/internal/config"
	"github.com/eremenko789/gitea_conventional_commit_checker/internal/conventional"
	"github.com/eremenko789/gitea_conventional_commit_checker/internal/gitea"
)

// Job is one pull_request hook worth of work.
type Job struct {
	Owner        string
	Repo         string
	RepoFullName string
	PRIndex      int
	HeadSHA      string
	PRTitle      string
	Sender       string
}

// Processor runs a worker pool over a bounded queue.
type Processor struct {
	cfg    *config.Config
	client *gitea.Client
	log    *slog.Logger

	queue chan Job
	wg    sync.WaitGroup
}

// New constructs a processor. queueSize should match config server.queue_size.
func New(cfg *config.Config, client *gitea.Client, log *slog.Logger, queueSize int) *Processor {
	if log == nil {
		log = slog.Default()
	}
	return &Processor{
		cfg:    cfg,
		client: client,
		log:    log,
		queue:  make(chan Job, queueSize),
	}
}

// Start launches worker goroutines. Call once.
func (p *Processor) Start(workers int) {
	for i := 0; i < workers; i++ {
		p.wg.Add(1)
		go func(id int) {
			defer p.wg.Done()
			for job := range p.queue {
				p.handle(job, id)
			}
		}(i)
	}
}

// Shutdown closes the job queue and waits for workers to drain.
func (p *Processor) Shutdown() {
	close(p.queue)
	p.wg.Wait()
}

// Enqueue schedules a job; returns false if the queue is full.
func (p *Processor) Enqueue(job Job) bool {
	select {
	case p.queue <- job:
		return true
	default:
		return false
	}
}

func (p *Processor) handle(job Job, workerID int) {
	log := p.log.With(
		"worker", workerID,
		"repo", job.RepoFullName,
		"pr", job.PRIndex,
	)
	ch := p.cfg.EffectiveCheck(job.RepoFullName)
	allowed := ch.AllowedTypeSet()

	timeout := p.cfg.Server.GiteaTimeout * 3
	if timeout < 30*time.Second {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	targetBase, err := renderTargetURL(ch.TargetURLTemplate, config.TemplateData{
		PRNumber:     job.PRIndex,
		PRTitle:      job.PRTitle,
		RepoFullName: job.RepoFullName,
		Owner:        job.Owner,
		Repo:         job.Repo,
	})
	if err != nil {
		log.Error("target_url template", "err", err)
	}

	if strings.TrimSpace(ch.DescriptionPending) != "" {
		desc, err := renderDescription(ch.DescriptionPending, templateData(job, nil, nil, 0, 0, 0))
		if err != nil {
			log.Error("pending template", "err", err)
		} else {
			desc = truncateRunes(desc, ch.DescriptionMaxRunes)
			if err := p.client.CreateStatus(ctx, job.Owner, job.Repo, job.HeadSHA, gitea.CreateStatusRequest{
				State:       gitea.StatePending,
				TargetURL:   targetBase,
				Description: desc,
				Context:     ch.Context,
			}); err != nil {
				log.Warn("pending status", "err", err)
			}
		}
	}

	commits, err := p.client.ListPullCommits(ctx, job.Owner, job.Repo, job.PRIndex)
	if err != nil {
		log.Error("list pr commits", "err", err)
		p.postInfraError(ctx, job, ch, targetBase, err)
		return
	}

	var invalid []config.InvalidCommitEntry
	checked := 0
	for _, c := range commits {
		msg := strings.TrimSpace(c.Commit.Message)
		first := strings.Split(msg, "\n")[0]
		res := conventional.ValidateSubject(msg, allowed, ch.SkipMerge())
		if res.OK && res.Reason == "skipped merge/revert style" {
			continue
		}
		checked++
		if !res.OK {
			invalid = append(invalid, config.InvalidCommitEntry{
				ShortSHA: shortSHA(c.SHA),
				FullSHA:  c.SHA,
				Subject:  first,
			})
		}
	}

	total := len(commits)
	good := checked - len(invalid)
	data := templateData(job, commits, invalid, len(invalid), good, total)

	var desc string
	var state gitea.StatusState
	if len(invalid) == 0 {
		state = gitea.StateSuccess
		desc, err = renderDescription(ch.DescriptionSuccess, data)
	} else {
		state = gitea.StateFailure
		desc, err = renderDescription(ch.DescriptionFailure, data)
	}
	if err != nil {
		log.Error("result template", "err", err)
		p.postInfraError(ctx, job, ch, targetBase, err)
		return
	}
	desc = truncateRunes(desc, ch.DescriptionMaxRunes)

	shas := statusSHAs(job, commits, ch.StatusEachCommit())

	for _, sha := range shas {
		if err := p.client.CreateStatus(ctx, job.Owner, job.Repo, sha, gitea.CreateStatusRequest{
			State:       state,
			TargetURL:   targetBase,
			Description: desc,
			Context:     ch.Context,
		}); err != nil {
			log.Error("create status", "sha", sha, "err", err)
		}
	}

	if ch.PRCommentsOn() {
		tmplStr := ch.PRCommentSuccess
		if len(invalid) > 0 {
			tmplStr = ch.PRCommentFailure
		}
		commentBody, cerr := renderDescription(tmplStr, data)
		if cerr != nil {
			log.Error("pr_comment template", "err", cerr)
		} else {
			commentBody = truncateRunes(commentBody, ch.PRCommentMaxRunes)
			if err := p.client.CreateIssueComment(ctx, job.Owner, job.Repo, job.PRIndex, commentBody); err != nil {
				log.Warn("pr comment", "err", err)
			}
		}
	}
}

func statusSHAs(job Job, commits []gitea.Commit, each bool) []string {
	var shas []string
	seen := map[string]struct{}{}
	if job.HeadSHA != "" {
		shas = append(shas, job.HeadSHA)
		seen[job.HeadSHA] = struct{}{}
	}
	if !each {
		return shas
	}
	for _, c := range commits {
		if c.SHA == "" {
			continue
		}
		if _, ok := seen[c.SHA]; ok {
			continue
		}
		seen[c.SHA] = struct{}{}
		shas = append(shas, c.SHA)
	}
	return shas
}

func templateData(job Job, commits []gitea.Commit, invalid []config.InvalidCommitEntry, bad, good, total int) config.TemplateData {
	headFull, headShort := headSHAForReport(job, commits)
	lines := formatInvalidLines(invalid)
	md := formatInvalidMarkdown(invalid)
	return config.TemplateData{
		PRNumber:               job.PRIndex,
		PRTitle:                job.PRTitle,
		RepoFullName:           job.RepoFullName,
		Owner:                  job.Owner,
		Repo:                   job.Repo,
		HeadSHA:                headFull,
		HeadShortSHA:           headShort,
		InvalidCommits:         lines,
		InvalidCommitsMarkdown: md,
		InvalidCommitsList:     invalid,
		BadCount:               bad,
		GoodCount:              good,
		TotalChecked:           total,
	}
}

func headSHAForReport(job Job, commits []gitea.Commit) (full, short string) {
	full = strings.TrimSpace(job.HeadSHA)
	if full != "" {
		return full, shortSHA(full)
	}
	if len(commits) > 0 {
		full = strings.TrimSpace(commits[len(commits)-1].SHA)
		if full == "" {
			return "", ""
		}
		return full, shortSHA(full)
	}
	return "", ""
}

func (p *Processor) postInfraError(ctx context.Context, job Job, ch config.CheckConfig, targetURL string, cause error) {
	msg := "Could not verify commits for this pull request."
	var he *gitea.HTTPError
	if errors.As(cause, &he) {
		switch he.Status {
		case 401, 403:
			msg = "Gitea API rejected credentials or permissions for commit status."
		case 404:
			msg = "Gitea API returned not found for this pull request or repository."
		default:
			msg = fmt.Sprintf("Gitea API error (HTTP %d).", he.Status)
		}
	} else if cause != nil {
		msg = "Temporary error talking to Gitea while verifying commits."
	}
	desc := truncateRunes(msg, ch.DescriptionMaxRunes)
	_ = p.client.CreateStatus(ctx, job.Owner, job.Repo, job.HeadSHA, gitea.CreateStatusRequest{
		State:       gitea.StateError,
		TargetURL:   targetURL,
		Description: desc,
		Context:     ch.Context,
	})
}
