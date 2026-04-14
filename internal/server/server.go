package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/eremenko789/gitea_conventional_commit_checker/internal/config"
	"github.com/eremenko789/gitea_conventional_commit_checker/internal/processor"
	"github.com/eremenko789/gitea_conventional_commit_checker/pkg/webhook"
)

// Server is the HTTP entrypoint.
type Server struct {
	cfg       *config.Config
	proc      *processor.Processor
	log       *slog.Logger
	secret    []byte
	allowRepo func(full string) bool
}

// New creates an HTTP server wrapper.
func New(cfg *config.Config, proc *processor.Processor, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	allow := map[string]struct{}{}
	for _, r := range cfg.Repositories {
		k := strings.TrimSpace(strings.ToLower(r.Name))
		if k != "" {
			allow[k] = struct{}{}
		}
	}
	return &Server{
		cfg:    cfg,
		proc:   proc,
		log:    log,
		secret: []byte(cfg.Server.WebhookSecret),
		allowRepo: func(full string) bool {
			_, ok := allow[strings.TrimSpace(strings.ToLower(full))]
			return ok
		},
	}
}

// Handler returns the root mux with routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/webhook", s.handleWebhook)
	return mux
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()

	if s.log.Enabled(r.Context(), slog.LevelDebug) {
		s.log.Debug("webhook request",
			"remote_addr", r.RemoteAddr,
			"method", r.Method,
			"request_uri", r.RequestURI,
			"headers", r.Header.Clone(),
			"body", string(body),
		)
	}

	if len(s.secret) > 0 {
		sig := r.Header.Get("X-Gitea-Signature")
		hub := r.Header.Get("X-Hub-Signature-256")
		if !verifyGiteaSignature(s.secret, body, sig, hub) {
			s.log.Warn("webhook signature mismatch")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	event := r.Header.Get("X-Gitea-Event")
	if event == "" {
		event = r.Header.Get("X-Gogs-Event")
	}
	if !strings.EqualFold(event, webhook.EventPullRequest) {
		w.WriteHeader(http.StatusOK)
		return
	}

	payload, err := webhook.ParsePullRequest(body)
	if err != nil {
		s.log.Warn("webhook parse", "err", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if !shouldProcessAction(payload.Action) {
		w.WriteHeader(http.StatusOK)
		return
	}

	full := payload.OwnerRepo()
	if full == "" || !s.allowRepo(full) {
		w.WriteHeader(http.StatusOK)
		return
	}

	owner, repo, ok := splitOwnerRepo(full)
	if !ok {
		s.log.Warn("bad repo full name", "name", full)
		w.WriteHeader(http.StatusOK)
		return
	}
	idx := payload.PRIndex()
	if idx <= 0 {
		s.log.Warn("missing pr index")
		w.WriteHeader(http.StatusOK)
		return
	}
	head := strings.TrimSpace(payload.PullRequest.Head.Sha)
	if head == "" {
		s.log.Warn("missing head sha")
		w.WriteHeader(http.StatusOK)
		return
	}

	sender := ""
	if payload.Sender != nil {
		sender = payload.Sender.Login
	}

	job := processor.Job{
		Owner:        owner,
		Repo:         repo,
		RepoFullName: full,
		PRIndex:      idx,
		HeadSHA:      head,
		PRTitle:      payload.PullRequest.Title,
		Sender:       sender,
	}
	if !s.proc.Enqueue(job) {
		s.log.Warn("queue full", "repo", full, "pr", idx)
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func shouldProcessAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case webhook.ActionOpened, webhook.ActionReopened, webhook.ActionSynchronize:
		return true
	default:
		return false
	}
}

func splitOwnerRepo(full string) (owner, repo string, ok bool) {
	full = strings.TrimSpace(full)
	i := strings.Index(full, "/")
	if i <= 0 || i >= len(full)-1 {
		return "", "", false
	}
	return full[:i], full[i+1:], true
}

// Run serves HTTP until ctx is cancelled, then shuts down gracefully.
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.cfg.Server.Listen,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("http listen", "addr", s.cfg.Server.Listen)
		err := srv.ListenAndServe()
		if err == http.ErrServerClosed {
			err = nil
		}
		errCh <- err
	}()
	select {
	case <-ctx.Done():
		shCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		_ = srv.Shutdown(shCtx)
		err := <-errCh
		return err
	case err := <-errCh:
		return err
	}
}
