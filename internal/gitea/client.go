package gitea

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client calls Gitea HTTP API.
type Client struct {
	baseURL    *url.URL
	token      string
	httpClient *http.Client
	retries    int
	retryDelay time.Duration
	log        *slog.Logger
}

// NewClient parses baseURL and returns a configured client.
func NewClient(rawBase, token string, timeout time.Duration, retries int, retryDelay time.Duration, log *slog.Logger) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(rawBase, "/"))
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid gitea.base_url: %q", rawBase)
	}
	if log == nil {
		log = slog.Default()
	}
	return &Client{
		baseURL: u,
		token:   token,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		retries:    retries,
		retryDelay: retryDelay,
		log:        log,
	}, nil
}

// Commit is a minimal PR commit payload from Gitea API.
type Commit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
	} `json:"commit"`
}

// ListPullCommits returns all commits for a pull request (paginated).
func (c *Client) ListPullCommits(ctx context.Context, owner, repo string, prIndex int) ([]Commit, error) {
	var all []Commit
	page := 1
	for {
		rel := fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%d/commits?page=%d&limit=50",
			url.PathEscape(owner), url.PathEscape(repo), prIndex, page)
		var batch []Commit
		if err := c.getJSON(ctx, rel, &batch); err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		all = append(all, batch...)
		if len(batch) < 50 {
			break
		}
		page++
	}
	return all, nil
}

// StatusState is a Gitea commit status state.
type StatusState string

const (
	StatePending StatusState = "pending"
	StateSuccess StatusState = "success"
	StateFailure StatusState = "failure"
	StateError   StatusState = "error"
)

// CreateStatusRequest is the JSON body for POST .../statuses/{sha}.
type CreateStatusRequest struct {
	State       StatusState `json:"state"`
	TargetURL   string      `json:"target_url,omitempty"`
	Description string      `json:"description"`
	Context     string      `json:"context"`
}

// CreateStatus sets commit status for sha.
func (c *Client) CreateStatus(ctx context.Context, owner, repo, sha string, req CreateStatusRequest) error {
	rel := fmt.Sprintf("/api/v1/repos/%s/%s/statuses/%s", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(sha))
	return c.postJSON(ctx, rel, req, nil)
}

// CreateIssueCommentRequest is the JSON body for POST .../issues/{id}/comments.
// Pull requests use the same issue index as the PR number.
type CreateIssueCommentRequest struct {
	Body string `json:"body"`
}

// CreateIssueComment adds a comment to an issue or pull request.
func (c *Client) CreateIssueComment(ctx context.Context, owner, repo string, issueIndex int, body string) error {
	rel := fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d/comments",
		url.PathEscape(owner), url.PathEscape(repo), issueIndex)
	return c.postJSON(ctx, rel, CreateIssueCommentRequest{Body: body}, nil)
}

func (c *Client) getJSON(ctx context.Context, relativePath string, out any) error {
	return c.doJSON(ctx, http.MethodGet, relativePath, nil, out)
}

func (c *Client) postJSON(ctx context.Context, relativePath string, body any, out any) error {
	return c.doJSON(ctx, http.MethodPost, relativePath, body, out)
}

func (c *Client) doJSON(ctx context.Context, method, relativePath string, body any, out any) error {
	attempts := c.retries
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			backoff := c.retryDelay * time.Duration(1<<(attempt-1))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		full := joinURL(c.baseURL, relativePath)
		var rdr io.Reader
		var reqBody []byte
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				return err
			}
			reqBody = b
			rdr = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(ctx, method, full, rdr)
		if err != nil {
			return err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if c.token != "" {
			req.Header.Set("Authorization", "token "+c.token)
		}

		if c.log.Enabled(ctx, slog.LevelDebug) {
			c.log.DebugContext(ctx, "gitea http request",
				"method", method,
				"url", full,
				"headers", headersForLog(req.Header),
				"body", string(reqBody),
			)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if shouldRetryAfterError(err, 0) {
				continue
			}
			return err
		}

		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			return readErr
		}

		if c.log.Enabled(ctx, slog.LevelDebug) {
			c.log.DebugContext(ctx, "gitea http response",
				"method", method,
				"url", full,
				"status", resp.StatusCode,
				"headers", headersForLog(resp.Header),
				"body", string(respBody),
			)
		}

		if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			lastErr = fmt.Errorf("gitea %s %s: %s", method, full, strings.TrimSpace(string(respBody)))
			continue
		}
		if resp.StatusCode >= 400 {
			return &HTTPError{Status: resp.StatusCode, Body: string(respBody)}
		}

		if out != nil && len(respBody) > 0 && method != http.MethodDelete {
			if err := json.Unmarshal(respBody, out); err != nil {
				return err
			}
		}
		return nil
	}
	if lastErr == nil {
		lastErr = errors.New("gitea: exhausted retries")
	}
	return lastErr
}

func joinURL(base *url.URL, rel string) string {
	b := strings.TrimRight(base.String(), "/")
	return b + rel
}

// headersForLog returns a copy of h with Authorization values redacted.
func headersForLog(h http.Header) map[string][]string {
	if h == nil {
		return nil
	}
	out := make(map[string][]string, len(h))
	for k, vv := range h {
		cp := append([]string(nil), vv...)
		if strings.EqualFold(k, "Authorization") {
			for i := range cp {
				cp[i] = "***redacted***"
			}
		}
		out[k] = cp
	}
	return out
}

func shouldRetryAfterError(err error, status int) bool {
	if status == 429 || status >= 500 {
		return true
	}
	if err == nil {
		return false
	}
	// transient network failures
	var _ = err
	return true
}

// HTTPError is a non-2xx API response (4xx included).
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return "gitea api: " + strconv.Itoa(e.Status) + ": " + strings.TrimSpace(e.Body)
}
