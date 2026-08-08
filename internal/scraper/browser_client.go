package scraper

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
)

// BrowserResult holds the result from a browser fetch
type BrowserResult struct {
	Content string // Full HTML content
	Title   string // Page title
}

// findChrome finds the Chrome executable on the system
func findChrome() string {
	paths := []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		os.Getenv("LOCALAPPDATA") + `\Google\Chrome\Application\chrome.exe`,
		`/usr/bin/google-chrome-stable`,
		`/usr/bin/google-chrome`,
		`/usr/bin/chromium-browser`,
		`/usr/bin/chromium`,
		`/snap/bin/chromium`,
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "" // Let rod download its own
}

// FetchHTMLWithBrowser uses headless Chrome via rod + stealth to fetch HTML.
func FetchHTMLWithBrowser(ctx context.Context, targetURL string, timeout time.Duration) (*BrowserResult, error) {
	log.Printf("🌐 Launching stealth browser for: %s", targetURL)

	browserCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Find system Chrome to avoid downloading Chromium
	chromePath := findChrome()

	// Create launcher — disable leakless to avoid Windows antivirus false positive
	l := launcher.New().
		Context(browserCtx).
		Leakless(false). // Disable leakless (causes antivirus issues on Windows)
		Headless(true).
		Set("disable-web-security").
		Set("disable-setuid-sandbox").
		Set("disable-dev-shm-usage").
		Set("disable-accelerated-2d-canvas").
		Set("disable-gpu").
		Set("blink-settings", "imagesEnabled=false").
		Set("mute-audio").
		Set("no-sandbox")

	if chromePath != "" {
		log.Printf("🔍 Using system Chrome: %s", chromePath)
		l = l.Bin(chromePath)
	}

	u, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("failed to launch browser: %w", err)
	}

	browser := rod.New().Context(browserCtx).ControlURL(u)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to browser: %w", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer closeCancel()
		_ = browser.Context(closeCtx).Close()
	}()

	// Use stealth mode — patches all common bot detection methods
	page, err := stealth.Page(browser)
	if err != nil {
		return nil, fmt.Errorf("failed to create stealth page: %w", err)
	}
	defer func() { _ = page.Close() }()

	// Set viewport to match the working Puppeteer service
	if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:             1366,
		Height:            768,
		DeviceScaleFactor: 1,
		Mobile:            false,
	}); err != nil {
		return nil, fmt.Errorf("failed to set viewport: %w", err)
	}

	// Set User-Agent
	if err := page.SetUserAgent(&proto.NetworkSetUserAgentOverride{
		UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
	}); err != nil {
		return nil, fmt.Errorf("failed to set user agent: %w", err)
	}

	// Navigate to URL
	log.Printf("📡 Navigating to: %s", targetURL)
	err = page.Navigate(targetURL)
	if err != nil {
		return nil, fmt.Errorf("navigate failed: %w", err)
	}

	// Wait only for window.onload. WaitStable also waits for network and DOM
	// inactivity, which never occurs on ad/video pages and used to add 60s to
	// every MissAV request.
	if err := page.WaitLoad(); err != nil {
		return nil, fmt.Errorf("wait for page load failed: %w", err)
	}

	// Check for Cloudflare challenge
	title, err := pageTitle(page)
	if err != nil {
		return nil, fmt.Errorf("read page title failed: %w", err)
	}
	log.Printf("📄 Page title: %s", title)

	if isCloudflareChallenge(title) {
		log.Printf("⏳ Cloudflare challenge detected, waiting...")
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for isCloudflareChallenge(title) {
			select {
			case <-browserCtx.Done():
				return nil, fmt.Errorf("cloudflare challenge did not resolve: %w", browserCtx.Err())
			case <-ticker.C:
				nextTitle, titleErr := pageTitle(page)
				if titleErr != nil {
					// Evaluation contexts are briefly destroyed while Cloudflare
					// redirects from the challenge to the target page. Retry until
					// the overall browser deadline instead of failing that request.
					continue
				}
				title = nextTitle
			}
		}

		// The challenge normally redirects to the requested page. Wait for that
		// navigation's load event before collecting the HTML.
		if err := page.WaitLoad(); err != nil {
			return nil, fmt.Errorf("wait for post-challenge page load failed: %w", err)
		}
	}

	log.Printf("✅ Page loaded: %s", title)

	// Get full HTML content
	html, err := page.HTML()
	if err != nil {
		return nil, fmt.Errorf("read page HTML failed: %w", err)
	}

	log.Printf("📦 Browser fetched %d bytes from %s", len(html), targetURL)

	return &BrowserResult{
		Content: html,
		Title:   title,
	}, nil
}

func pageTitle(page *rod.Page) (string, error) {
	result, err := page.Eval(`() => document.title`)
	if err != nil {
		return "", err
	}
	return result.Value.Str(), nil
}

func isCloudflareChallenge(title string) bool {
	return strings.EqualFold(strings.TrimSpace(title), "Just a moment...")
}
