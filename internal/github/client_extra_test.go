package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	gogithub "github.com/google/go-github/v88/github"
)

// stubTransport redirects all outgoing HTTP(S) requests to the given target
// host (e.g. "127.0.0.1:PORT") over plain HTTP.  This lets us intercept calls
// made by NewClient (which builds its own *gogithub.Client internally) without
// modifying the production source.
type stubTransport struct {
	targetHost string
	inner      http.RoundTripper
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r2 := req.Clone(req.Context())
	r2.URL.Scheme = "http"
	r2.URL.Host = s.targetHost
	return s.inner.RoundTrip(r2)
}

// withDefaultTransport temporarily replaces http.DefaultTransport with t and
// restores the original when the returned cleanup function is called.
// NOT safe for parallel tests that share http.DefaultTransport.
func withDefaultTransport(t http.RoundTripper) (restore func()) {
	orig := http.DefaultTransport
	http.DefaultTransport = t
	return func() { http.DefaultTransport = orig }
}

// TestNewClientSuccess exercises the success path of NewClient by intercepting
// all outbound HTTP(S) requests via a custom RoundTripper and redirecting them
// to a local httptest server that returns a valid /user response.
func TestNewClientSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			writeJSON(w, map[string]string{"login": "testowner"})
		default:
			writeJSON(w, map[string]interface{}{})
		}
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	restore := withDefaultTransport(&stubTransport{
		targetHost: u.Host,
		inner:      http.DefaultTransport,
	})
	defer restore()

	c, err := NewClient("fake-token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c == nil {
		t.Fatal("NewClient returned nil client")
	}
}

// TestNewClientNoToken verifies that NewClient returns ErrNoToken when called
// with an empty token.
func TestNewClientNoToken(t *testing.T) {
	_, err := NewClient("")
	if err == nil {
		t.Fatal("expected error from NewClient with empty token")
	}
	if !errors.Is(err, ErrNoToken) {
		t.Errorf("expected ErrNoToken, got: %v", err)
	}
}

// TestNewClientResolveError exercises the error path when /user returns a
// non-2xx response.
func TestNewClientResolveError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	restore := withDefaultTransport(&stubTransport{
		targetHost: u.Host,
		inner:      http.DefaultTransport,
	})
	defer restore()

	_, err := NewClient("bad-token")
	if err == nil {
		t.Error("expected error from NewClient when /user returns 401")
	}
}

// TestNewClientEmptyLogin exercises the "empty owner login" error path in
// NewClient.
func TestNewClientEmptyLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			// Return a user with empty login field.
			writeJSON(w, map[string]string{"login": ""})
		default:
			writeJSON(w, map[string]interface{}{})
		}
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	restore := withDefaultTransport(&stubTransport{
		targetHost: u.Host,
		inner:      http.DefaultTransport,
	})
	defer restore()

	_, err := NewClient("some-token")
	if err == nil {
		t.Error("expected error from NewClient when /user returns empty login")
	}
}

// TestListPRsRateLimitRetry verifies that listPRs (via ListOpenPRs) retries
// once after receiving a 403 with X-RateLimit-Remaining: 0.
func TestListPRsRateLimitRetry(t *testing.T) {
	callCount := 0
	now := time.Now()

	// The go-github client interprets 403 + X-RateLimit-Remaining:0 as a
	// *RateLimitError.  We need to include the proper JSON body and headers
	// so go-github parses it correctly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: rate-limit 403.
			reset := now.Add(100 * time.Millisecond).Unix()
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-RateLimit-Limit", "60")
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", reset))
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"message":           "API rate limit exceeded",
				"documentation_url": "https://docs.github.com/rest/overview/rate-limits",
			})
			return
		}
		// Second call: success.
		prs := []*gogithub.PullRequest{
			{Number: intPtr(99), Title: strPtr("retry PR"), State: strPtr("open")},
		}
		writeJSON(w, prs)
	}))
	defer srv.Close()

	ghc := newGHClient(t, srv.URL)

	c := buildClientWithGHC(ghc, "testuser")
	got, err := c.ListOpenPRs(context.Background(), "testuser", "myrepo", "feat")
	if err != nil {
		t.Fatalf("ListOpenPRs (after rate-limit retry): %v", err)
	}
	if callCount < 2 {
		t.Errorf("expected at least 2 calls (rate-limit + retry), got %d", callCount)
	}
	if len(got) != 1 {
		t.Errorf("len(prs) = %d, want 1", len(got))
	}
}

// TestListPRsRateLimitRetryFails verifies that if the retry after a rate-limit
// also fails, the error is returned.
func TestListPRsRateLimitRetryFails(t *testing.T) {
	callCount := 0
	now := time.Now()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		reset := now.Add(100 * time.Millisecond).Unix()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Limit", "60")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", reset))
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"message":           "API rate limit exceeded",
			"documentation_url": "https://docs.github.com/rest/overview/rate-limits",
		})
	}))
	defer srv.Close()

	ghc := newGHClient(t, srv.URL)

	c := buildClientWithGHC(ghc, "testuser")
	_, err := c.ListOpenPRs(context.Background(), "testuser", "myrepo", "feat")
	if err == nil {
		t.Error("expected error when rate-limit retry also fails")
	}
}

// TestListReposRateLimitRetry verifies that ListRepos retries once after a
// rate-limit 403.
func TestListReposRateLimitRetry(t *testing.T) {
	callCount := 0
	now := time.Now()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			reset := now.Add(100 * time.Millisecond).Unix()
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-RateLimit-Limit", "60")
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", reset))
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"message":           "API rate limit exceeded",
				"documentation_url": "https://docs.github.com/rest/overview/rate-limits",
			})
			return
		}
		repos := []*gogithub.Repository{
			{Name: strPtr("after-retry-repo")},
		}
		writeJSON(w, repos)
	}))
	defer srv.Close()

	ghc := newGHClient(t, srv.URL)

	c := buildClientWithGHC(ghc, "testuser")
	got, err := c.ListRepos(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListRepos (after rate-limit retry): %v", err)
	}
	if callCount < 2 {
		t.Errorf("expected at least 2 calls, got %d", callCount)
	}
	if len(got) != 1 || got[0].GetName() != "after-retry-repo" {
		t.Errorf("repos = %v, want [after-retry-repo]", got)
	}
}

// TestListReposRateLimitRetryFails verifies that ListRepos returns an error
// when both the initial call and the rate-limit retry fail.
func TestListReposRateLimitRetryFails(t *testing.T) {
	now := time.Now()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reset := now.Add(100 * time.Millisecond).Unix()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Limit", "60")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", reset))
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"message":           "API rate limit exceeded",
			"documentation_url": "https://docs.github.com/rest/overview/rate-limits",
		})
	}))
	defer srv.Close()

	ghc := newGHClient(t, srv.URL)

	c := buildClientWithGHC(ghc, "testuser")
	_, err := c.ListRepos(context.Background(), 10)
	if err == nil {
		t.Error("expected error when rate-limit retry also fails")
	}
}

// TestListPRsContextCancelledDuringRateLimitWait verifies that a cancelled
// context is respected during the rate-limit retry sleep.
func TestListPRsContextCancelledDuringRateLimitWait(t *testing.T) {
	now := time.Now()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always respond with a rate-limit that resets far in the future.
		reset := now.Add(60 * time.Second).Unix()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Limit", "60")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", reset))
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"message":           "API rate limit exceeded",
			"documentation_url": "https://docs.github.com/rest/overview/rate-limits",
		})
	}))
	defer srv.Close()

	ghc := newGHClient(t, srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	c := buildClientWithGHC(ghc, "testuser")
	_, err := c.ListOpenPRs(ctx, "testuser", "myrepo", "feat")
	if err == nil {
		t.Error("expected error when context cancelled during rate-limit wait")
	}
}

// TestListReposContextCancelledDuringRateLimitWait verifies context cancellation
// during rate-limit wait in ListRepos.
func TestListReposContextCancelledDuringRateLimitWait(t *testing.T) {
	now := time.Now()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reset := now.Add(60 * time.Second).Unix()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Limit", "60")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", reset))
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"message":           "API rate limit exceeded",
			"documentation_url": "https://docs.github.com/rest/overview/rate-limits",
		})
	}))
	defer srv.Close()

	ghc := newGHClient(t, srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	c := buildClientWithGHC(ghc, "testuser")
	_, err := c.ListRepos(ctx, 10)
	if err == nil {
		t.Error("expected error when context cancelled during rate-limit wait")
	}
}

// TestListOpenPRsPagination verifies that listPRs follows pagination links
// and accumulates results across pages.
func TestListOpenPRsPagination(t *testing.T) {
	page1 := []*gogithub.PullRequest{
		{Number: intPtr(1), Title: strPtr("PR one"), State: strPtr("open")},
	}
	page2 := []*gogithub.PullRequest{
		{Number: intPtr(2), Title: strPtr("PR two"), State: strPtr("open")},
	}

	var srv *httptest.Server
	handler := func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "2" {
			writeJSON(w, page2)
			return
		}
		// First page: add Link header to indicate next page.
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/testuser/myrepo/pulls?page=2>; rel="next"`, srv.URL))
		writeJSON(w, page1)
	}
	ghc, srv2 := newTestServer(t, handler)
	srv = srv2
	defer srv.Close()

	c := buildClientWithGHC(ghc, "testuser")
	got, err := c.ListOpenPRs(context.Background(), "testuser", "myrepo", "feat")
	if err != nil {
		t.Fatalf("ListOpenPRs paginated: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len(prs) = %d, want 2 (paginated)", len(got))
	}
}
