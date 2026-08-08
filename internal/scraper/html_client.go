package scraper

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"api-node/internal/config"
)

// HTMLClient handles HTML fetching with browser-like headers
type HTMLClient struct {
	httpClient *http.Client
}

// Reuse TCP/TLS connections between scrape requests. A new HTMLClient still
// gets its own cookie jar, but it does not have to create a new transport and
// connection pool for every request.
var sharedHTMLTransport = func() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // Preserve existing scraper behavior.
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 10
	transport.IdleConnTimeout = 90 * time.Second
	return transport
}()

// NewHTMLClient creates a new HTML client
func NewHTMLClient() *HTMLClient {
	// Create cookie jar for session handling
	jar, _ := cookiejar.New(nil)

	timeout := time.Duration(config.AppConfig.HTTPTimeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &HTMLClient{
		httpClient: &http.Client{
			Timeout:   timeout,
			Jar:       jar,
			Transport: sharedHTMLTransport,
		},
	}
}

// FetchHTML fetches HTML content from the given URL
func (c *HTMLClient) FetchHTML(ctx context.Context, targetURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Parse URL to extract domain for Referer
	parsedURL, _ := url.Parse(targetURL)
	referer := fmt.Sprintf("%s://%s/", parsedURL.Scheme, parsedURL.Host)

	// Set headers to mimic a real Chrome browser
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,th;q=0.8")
	req.Header.Set("Cache-Control", "max-age=0")
	req.Header.Set("Sec-Ch-Ua", `"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", `"Windows"`)
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Referer", referer)

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusForbidden {
			return "", fmt.Errorf("403 Forbidden: Access denied (anti-bot protection)")
		}
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Handle gzip encoding if server sends it
	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzReader.Close()
		reader = gzReader
	}

	// Read response body
	body, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	log.Printf("📦 Fetched %d bytes from %s", len(body), targetURL)

	return string(body), nil
}

// FetchHTMLWithRetry fetches HTML with retry logic.
// If all retries fail with 403, it falls back to headless Chrome.
func (c *HTMLClient) FetchHTMLWithRetry(ctx context.Context, targetURL string, maxRetries int) (string, error) {
	var lastErr error
	got403 := false

	for i := 0; i < maxRetries; i++ {
		html, err := c.FetchHTML(ctx, targetURL)
		if err == nil {
			return html, nil
		}

		lastErr = err

		// Track if we got a 403 (anti-bot protection)
		if strings.Contains(err.Error(), "403 Forbidden") {
			got403 = true
			break // No point retrying with HTTP — need browser
		}

		if i < maxRetries-1 {
			waitTime := time.Duration(1<<uint(i)) * time.Second
			timer := time.NewTimer(waitTime)
			select {
			case <-timer.C:
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return "", fmt.Errorf("fetch canceled while waiting to retry: %w", ctx.Err())
			}
		}
	}

	// Fallback to headless Chrome if we got 403
	if got403 {
		log.Printf("🔄 Got 403, falling back to headless Chrome for: %s", targetURL)
		browserTimeout := c.httpClient.Timeout
		if browserTimeout <= 0 {
			browserTimeout = 30 * time.Second
		}
		result, err := FetchHTMLWithBrowser(ctx, targetURL, browserTimeout)
		if err != nil {
			return "", fmt.Errorf("browser fallback also failed: %w (original: %v)", err, lastErr)
		}
		return result.Content, nil
	}

	return "", fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}
