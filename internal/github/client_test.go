package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gogithub "github.com/google/go-github/v88/github"
)

// newTestServer creates an httptest.Server that serves the given handler and
// returns a *gogithub.Client wired to it plus the server (caller must Close).
func newTestServer(t *testing.T, handler http.HandlerFunc) (*gogithub.Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	return newGHClient(t, srv.URL), srv
}

// newGHClient builds a go-github client for tests pointed at baseURL. Since
// v88, NewClient returns an error and the base/upload URLs are set via options
// rather than struct fields; this centralizes both.
func newGHClient(t *testing.T, baseURL string) *gogithub.Client {
	t.Helper()
	ghc, err := gogithub.NewClient(gogithub.WithURLs(&baseURL, &baseURL))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return ghc
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// buildClientWithGHC creates a github.Client (our interface) from an existing
// *gogithub.Client, bypassing NewClient (which calls the real /user endpoint).
func buildClientWithGHC(ghc *gogithub.Client, owner string) Client {
	return &client{gh: ghc, owner: owner}
}

// --- ListRepos ---

func TestListReposSinglePage(t *testing.T) {
	repos := []*gogithub.Repository{
		{Name: strPtr("repo-a"), DefaultBranch: strPtr("main")},
		{Name: strPtr("repo-b"), DefaultBranch: strPtr("main")},
	}

	ghc, srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user/repos":
			writeJSON(w, repos)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	c := buildClientWithGHC(ghc, "testuser")
	got, err := c.ListRepos(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len(repos) = %d, want 2", len(got))
	}
}

func TestListReposLimitRespected(t *testing.T) {
	repos := make([]*gogithub.Repository, 5)
	for i := range repos {
		name := fmt.Sprintf("repo-%d", i)
		repos[i] = &gogithub.Repository{Name: &name}
	}

	ghc, srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, repos)
	})
	defer srv.Close()

	c := buildClientWithGHC(ghc, "testuser")
	got, err := c.ListRepos(context.Background(), 3)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("len(repos) = %d, want 3 (limit)", len(got))
	}
}

func TestListReposError(t *testing.T) {
	ghc, srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})
	defer srv.Close()

	c := buildClientWithGHC(ghc, "testuser")
	_, err := c.ListRepos(context.Background(), 10)
	if err == nil {
		t.Error("expected error from ListRepos on 403")
	}
}

func TestListReposContextCancelled(t *testing.T) {
	ghc, srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Block until the request context is cancelled, then return so the
		// connection closes and httptest.Server.Close() can complete.
		<-r.Context().Done()
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	c := buildClientWithGHC(ghc, "testuser")
	_, err := c.ListRepos(ctx, 10)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

// --- ListOpenPRs / ListMergedPRs ---

func TestListOpenPRs(t *testing.T) {
	prs := []*gogithub.PullRequest{
		{Number: intPtr(1), Title: strPtr("open pr"), State: strPtr("open")},
	}

	ghc, srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, prs)
	})
	defer srv.Close()

	c := buildClientWithGHC(ghc, "testuser")
	got, err := c.ListOpenPRs(context.Background(), "testuser", "repo-a", "feat")
	if err != nil {
		t.Fatalf("ListOpenPRs: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len(prs) = %d, want 1", len(got))
	}
}

func TestListOpenPRsError(t *testing.T) {
	ghc, srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	})
	defer srv.Close()

	c := buildClientWithGHC(ghc, "testuser")
	_, err := c.ListOpenPRs(context.Background(), "testuser", "repo-a", "feat")
	if err == nil {
		t.Error("expected error from ListOpenPRs on 500")
	}
}

func TestListMergedPRs(t *testing.T) {
	now := gogithub.Timestamp{Time: time.Now()}
	prs := []*gogithub.PullRequest{
		{Number: intPtr(2), Title: strPtr("merged pr"), State: strPtr("closed"), MergedAt: &now},
		// closed but not merged (MergedAt is zero) — should be filtered out.
		{Number: intPtr(3), Title: strPtr("closed not merged"), State: strPtr("closed")},
	}

	ghc, srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, prs)
	})
	defer srv.Close()

	c := buildClientWithGHC(ghc, "testuser")
	got, err := c.ListMergedPRs(context.Background(), "testuser", "repo-a", "feat")
	if err != nil {
		t.Fatalf("ListMergedPRs: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len(prs) = %d, want 1 (only the one with MergedAt set)", len(got))
	}
	if got[0].GetNumber() != 2 {
		t.Errorf("pr number = %d, want 2", got[0].GetNumber())
	}
}

func TestListMergedPRsError(t *testing.T) {
	ghc, srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	})
	defer srv.Close()

	c := buildClientWithGHC(ghc, "testuser")
	_, err := c.ListMergedPRs(context.Background(), "testuser", "repo-a", "feat")
	if err == nil {
		t.Error("expected error from ListMergedPRs on 502")
	}
}

func TestListReposPagination(t *testing.T) {
	page1 := []*gogithub.Repository{{Name: strPtr("repo-p1")}}
	page2 := []*gogithub.Repository{{Name: strPtr("repo-p2")}}

	var srv *httptest.Server
	handler := func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "2" {
			writeJSON(w, page2)
			return
		}
		// First page: add Link header to indicate next page.
		w.Header().Set("Link", fmt.Sprintf(`<%s/user/repos?page=2>; rel="next"`, srv.URL))
		writeJSON(w, page1)
	}
	ghc, srv2 := newTestServer(t, handler)
	srv = srv2
	defer srv.Close()

	c := buildClientWithGHC(ghc, "testuser")
	got, err := c.ListRepos(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListRepos paginated: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len(repos) = %d, want 2 (paginated)", len(got))
	}
}

// helpers
func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
