package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIsCloudflareChallenge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		title string
		want  bool
	}{
		{name: "exact", title: "Just a moment...", want: true},
		{name: "case and whitespace", title: "  JUST A MOMENT...  ", want: true},
		{name: "real page", title: "FNS-243 - MissAV", want: false},
		{name: "empty", title: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isCloudflareChallenge(tt.title); got != tt.want {
				t.Fatalf("isCloudflareChallenge(%q) = %v, want %v", tt.title, got, tt.want)
			}
		})
	}
}

func TestFetchHTMLWithBrowserDoesNotWaitForDOMStability(t *testing.T) {
	if os.Getenv("RUN_BROWSER_INTEGRATION") != "1" {
		t.Skip("set RUN_BROWSER_INTEGRATION=1 to run the Chrome integration test")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
<head><title>dynamic test page</title></head>
<body><main id="content">browser-ready</main>
<script>setInterval(() => document.body.dataset.tick = Date.now(), 25)</script>
</body>
</html>`))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	started := time.Now()
	result, err := FetchHTMLWithBrowser(ctx, server.URL, 10*time.Second)
	if err != nil {
		t.Fatalf("FetchHTMLWithBrowser() error = %v", err)
	}
	if !strings.Contains(result.Content, "browser-ready") {
		t.Fatal("FetchHTMLWithBrowser() did not return the expected page content")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("FetchHTMLWithBrowser() took %s; it appears to be waiting for DOM stability", elapsed)
	}
}
