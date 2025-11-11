package crawler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"saas-chatbot-platform/models"

	"github.com/PuerkitoBio/goquery"
	"github.com/andybalholm/brotli"
	"github.com/chromedp/chromedp"
	colly "github.com/gocolly/colly/v2"
	"golang.org/x/net/html/charset"
)

var (
	// Global HTTP transport with compression enabled
	httpTransport = &http.Transport{
		DisableCompression: false, // ✅ enables gzip/brotli decompression
	}
)

// CrawlConfig holds configuration for a crawl job
type CrawlConfig struct {
	URL            string
	MaxPages       int
	AllowedDomains []string
	AllowedPaths   []string
	FollowLinks    bool
	IncludeImages  bool
	RespectRobots  bool
	Timeout        time.Duration
	// Optional JS rendering for the initial page
	RenderJS         bool
	RenderTimeout    time.Duration
	WaitSelector     string
	NetworkIdleAfter time.Duration
}

// CrawlResult holds the result of a crawl operation
type CrawlResult struct {
	URL          string
	Title        string
	Content      string
	Pages        []models.CrawledPage
	Products     []models.Product
	Error        error
	PagesFound   int
	PagesCrawled int
}

// normalizeURL normalizes a URL to a canonical form for duplicate detection
func normalizeURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	// Remove fragment
	parsed.Fragment = ""

	// Normalize path - keep trailing slash as-is for root, remove for others
	// But be consistent: always remove trailing slash for non-root paths
	path := parsed.Path
	if path == "" {
		path = "/"
	} else if path != "/" {
		// Remove trailing slash for consistency
		path = strings.TrimSuffix(path, "/")
		if path == "" {
			path = "/"
		}
	}
	parsed.Path = path

	// Convert to lowercase scheme and host
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)

	// Remove default ports
	if parsed.Port() == "80" && parsed.Scheme == "http" {
		host, _, _ := strings.Cut(parsed.Host, ":")
		parsed.Host = host
	}
	if parsed.Port() == "443" && parsed.Scheme == "https" {
		host, _, _ := strings.Cut(parsed.Host, ":")
		parsed.Host = host
	}

	return parsed.String(), nil
}

// CrawlURL performs a production-grade crawl of a single URL
func CrawlURL(cfg CrawlConfig) (*CrawlResult, error) {
	result := &CrawlResult{
		URL:   cfg.URL,
		Pages: []models.CrawledPage{},
	}

	// Parse and validate the URL
	parsedURL, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	if parsedURL.Scheme == "" {
		parsedURL.Scheme = "https"
		cfg.URL = parsedURL.String()
	}

	// Normalize the starting URL - CRITICAL: normalize before everything
	normalizedStartURL, err := normalizeURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL format: %w", err)
	}

	// Determine allowed domains
	allowedDomains := cfg.AllowedDomains
	if len(allowedDomains) == 0 {
		hostname := parsedURL.Hostname()
		if hostname != "" {
			hostnameClean := strings.TrimPrefix(strings.ToLower(hostname), "www.")
			allowedDomains = []string{hostnameClean, "www." + hostnameClean, hostname}
			// Also add the hostname as-is (case variations)
			if !strings.Contains(strings.Join(allowedDomains, "|"), strings.ToLower(hostname)) {
				allowedDomains = append(allowedDomains, hostname)
			}
		}
	}

	// Create a FRESH collector for each crawl
	// This is critical - each crawl gets its own collector with fresh state
	options := []colly.CollectorOption{
		colly.Async(true),
		colly.MaxDepth(2),
	}

	// Add allowed domains
	if len(allowedDomains) > 0 {
		options = append(options, colly.AllowedDomains(allowedDomains...))
	}

	c := colly.NewCollector(options...)

	// ✅ Configure HTTP transport with compression enabled
	c.WithTransport(httpTransport)

	// Set timeout
	if cfg.Timeout > 0 {
		c.SetRequestTimeout(cfg.Timeout)
	} else {
		c.SetRequestTimeout(60 * time.Second)
	}

	// Set realistic browser User-Agent
	c.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

	// Configure rate limiting
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: 1,
		Delay:       2 * time.Second,
		RandomDelay: 1 * time.Second,
	})

	maxPages := cfg.MaxPages
	if maxPages <= 0 {
		maxPages = 50
	}

	// Thread-safe page storage
	var (
		pagesMu sync.Mutex
		pages   []models.CrawledPage
	)

	// Track which URLs we've successfully processed
	processed := sync.Map{}

	// Track URLs that have been queued for visiting (to avoid duplicate visits)
	queued := sync.Map{}
	var queuedMu sync.Mutex

	// Track if initial page was processed
	initialPageProcessed := false
	var initialPageMu sync.Mutex

	// On request - add proper browser-like headers to avoid 403 Forbidden
	c.OnRequest(func(r *colly.Request) {
		// Standard browser headers
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
		r.Headers.Set("Accept-Language", "en-US,en;q=0.9")
		r.Headers.Set("Accept-Encoding", "gzip, deflate, br, zstd")
		r.Headers.Set("Connection", "keep-alive")
		r.Headers.Set("Upgrade-Insecure-Requests", "1")
		r.Headers.Set("Sec-Fetch-Dest", "document")
		r.Headers.Set("Sec-Fetch-Mode", "navigate")
		r.Headers.Set("Sec-Fetch-Site", "none")
		r.Headers.Set("Sec-Fetch-User", "?1")
		r.Headers.Set("Sec-Ch-Ua", `"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"`)
		r.Headers.Set("Sec-Ch-Ua-Mobile", "?0")
		r.Headers.Set("Sec-Ch-Ua-Platform", `"Windows"`)

		// Set Referer to the same domain to appear more legitimate
		parsedURL, err := url.Parse(r.URL.String())
		if err == nil {
			referer := fmt.Sprintf("%s://%s/", parsedURL.Scheme, parsedURL.Host)
			r.Headers.Set("Referer", referer)
		}

		// Remove headers that might identify us as a bot
		r.Headers.Del("Cache-Control")
		r.Headers.Del("Pragma")
	})

	// On response - handle encoding and track successful responses
	c.OnResponse(func(r *colly.Response) {
		// ✅ Check content type - skip non-HTML content
		contentType := r.Headers.Get("Content-Type")
		if contentType != "" && !strings.Contains(contentType, "text/html") && !strings.Contains(contentType, "application/xhtml+xml") {
			// Skip binary files (PDFs, images, etc.)
			return
		}

		// ✅ Handle compression - Go's HTTP transport handles gzip automatically
		// But brotli (br) is NOT supported by standard transport, so handle it manually
		contentEncoding := r.Headers.Get("Content-Encoding")
		var bodyReader io.Reader = bytes.NewReader(r.Body)

		// Handle brotli compression manually (Go's standard transport doesn't support it)
		if strings.Contains(contentEncoding, "br") {
			brReader := brotli.NewReader(bodyReader)
			decompressed, err := io.ReadAll(brReader)
			if err == nil {
				r.Body = decompressed
				bodyReader = bytes.NewReader(decompressed)
			}
		}
		// Note: gzip is automatically handled by Go's HTTP transport,
		// so r.Body should already be decompressed for gzip responses

		// ✅ Properly decode the response body with charset handling
		// Detect and decode charset to UTF-8
		if len(r.Body) > 0 {
			utf8Reader, err := charset.NewReader(bodyReader, contentType)
			if err == nil {
				// Read the decoded body
				decodedBody, readErr := io.ReadAll(utf8Reader)
				if readErr == nil && len(decodedBody) > 0 {
					// Replace the body with properly decoded UTF-8 content
					r.Body = decodedBody
				}
			}
			// If charset detection fails, proceed with original body (may already be UTF-8)
		}

		// Mark response URL as processed (colly handled it)
		normalizedRespURL, _ := normalizeURL(r.Request.URL.String())
		if normalizedRespURL != "" {
			result.PagesFound++
		}
	})

	// On HTML - extract content
	c.OnHTML("html", func(e *colly.HTMLElement) {
		pagesMu.Lock()
		defer pagesMu.Unlock()

		// Check if we've reached max pages
		if len(pages) >= maxPages {
			return
		}

		// Normalize the URL
		rawURL := e.Request.URL.String()
		normalizedURL, err := normalizeURL(rawURL)
		if err != nil {
			return
		}

		// Check if we've already processed this exact normalized URL
		if _, exists := processed.LoadOrStore(normalizedURL, true); exists {
			// Already processed - skip
			return
		}

		// Process the page
		doc := e.DOM
		title := strings.TrimSpace(doc.Find("title").Text())
		content := extractMainContentFromSelection(e.DOM)

		// Try to get more content if initial extraction is minimal
		if len(content) < 50 {
			content = doc.Find("body").Text()
		}

		wordCount := len(strings.Fields(content))
		if wordCount < 10 {
			// Skip pages with too little content
			return
		}

		page := models.CrawledPage{
			URL:        normalizedURL,
			Title:      title,
			Content:    content,
			CrawledAt:  time.Now(),
			StatusCode: e.Response.StatusCode,
			Size:       int64(len(content)),
			WordCount:  wordCount,
		}

		pages = append(pages, page)

		// Set main title and content from first page
		if len(pages) == 1 {
			result.Title = title
			result.Content = content
			initialPageMu.Lock()
			initialPageProcessed = true
			initialPageMu.Unlock()
		}

		// Follow links if enabled
		if cfg.FollowLinks && len(pages) < maxPages {
			linkCount := 0
			doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
				if len(pages) >= maxPages {
					return
				}

				href, exists := s.Attr("href")
				if !exists || href == "" {
					return
				}

				// Skip anchors, javascript, mailto, tel links
				hrefLower := strings.ToLower(href)
				if strings.HasPrefix(href, "#") ||
					strings.HasPrefix(hrefLower, "javascript:") ||
					strings.HasPrefix(hrefLower, "mailto:") ||
					strings.HasPrefix(hrefLower, "tel:") {
					return
				}

				// Resolve relative URLs
				absoluteURL := e.Request.AbsoluteURL(href)
				if absoluteURL == "" {
					return
				}

				// Normalize the absolute URL
				normalized, err := normalizeURL(absoluteURL)
				if err != nil {
					return
				}

				// Check if already processed OR queued
				queuedMu.Lock()
				if _, queuedExists := queued.LoadOrStore(normalized, true); queuedExists {
					queuedMu.Unlock()
					return
				}
				queuedMu.Unlock()

				if _, processedExists := processed.Load(normalized); processedExists {
					return
				}

				// Check if URL matches allowed domains/paths
				if isURLAllowed(normalized, cfg, allowedDomains) {
					// Limit links per page
					if linkCount >= 20 {
						return
					}
					linkCount++

					// Visit using normalized URL - colly will handle duplicates
					c.Visit(normalized)
				}
			})
		}
	})

	// On error - handle gracefully
	c.OnError(func(r *colly.Response, err error) {
		errMsg := err.Error()
		requestURL := r.Request.URL.String()
		normalizedErrURL, _ := normalizeURL(requestURL)
		statusCode := r.StatusCode

		// ✅ Handle HTTP status code errors
		if statusCode == 403 {
			// Forbidden - server is blocking us
			fmt.Printf("⚠️ 403 Forbidden for URL: %s\n", requestURL)
			if normalizedErrURL == normalizedStartURL {
				result.Error = fmt.Errorf("access forbidden (403): the website blocked the crawler. This could be due to: bot protection, Cloudflare, rate limiting, or restricted access. Please check if the website allows web scraping or try a different URL")
			}
			return
		}

		if statusCode == 429 {
			// Too Many Requests
			fmt.Printf("⚠️ 429 Rate Limited for URL: %s\n", requestURL)
			if normalizedErrURL == normalizedStartURL {
				result.Error = fmt.Errorf("rate limited (429): too many requests. Please wait and try again later")
			}
			return
		}

		if statusCode >= 500 {
			// Server errors
			fmt.Printf("⚠️ Server Error %d for URL: %s\n", statusCode, requestURL)
			if normalizedErrURL == normalizedStartURL {
				result.Error = fmt.Errorf("server error (%d): the website server returned an error. Please try again later", statusCode)
			}
			return
		}

		// Handle "already visited" errors - colly's internal duplicate detection
		if strings.Contains(errMsg, "already visited") {
			// This is expected when following links - not a critical error
			// Check if we actually processed this URL
			if _, processed := processed.Load(normalizedErrURL); processed {
				// Already processed, error is fine
				return
			}

			// If it's the initial URL and we haven't processed anything, that's a problem
			if normalizedErrURL == normalizedStartURL {
				pagesMu.Lock()
				hasPages := len(pages) > 0
				pagesMu.Unlock()

				if !hasPages {
					// This shouldn't happen, but if it does, try visiting again
					// Remove from queued so we can retry
					queuedMu.Lock()
					queued.Delete(normalizedErrURL)
					queuedMu.Unlock()

					// Try visiting the original URL format
					c.Visit(cfg.URL)
				}
			}
			return
		}

		// Log other errors but don't fail unless it's the initial URL with no pages
		if normalizedErrURL == normalizedStartURL {
			pagesMu.Lock()
			hasPages := len(pages) > 0
			pagesMu.Unlock()

			if !hasPages && result.Error == nil {
				// Check if it's a network error vs HTTP error
				if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "no such host") {
					result.Error = fmt.Errorf("network error: %v. Please check the URL and your internet connection", err)
				} else if statusCode != 0 {
					result.Error = fmt.Errorf("HTTP error (%d): %v", statusCode, err)
				} else {
					result.Error = fmt.Errorf("failed to crawl initial URL %s: %w", normalizedStartURL, err)
				}
			}
		}
	})

	// CRITICAL: Mark starting URL as queued before visiting
	queuedMu.Lock()
	queued.Store(normalizedStartURL, true)
	queuedMu.Unlock()

	// Optionally prerender initial page for JS-heavy sites
	if cfg.RenderJS {
		renderTimeout := cfg.RenderTimeout
		if renderTimeout <= 0 {
			renderTimeout = 45 * time.Second
		}
		networkIdle := cfg.NetworkIdleAfter
		if networkIdle <= 0 {
			networkIdle = 1200 * time.Millisecond
		}
		html, renderErr := renderPageHTML(normalizedStartURL, renderTimeout, cfg.WaitSelector, networkIdle)
		if renderErr == nil && html != "" {
			doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(html))
			if parseErr == nil {
				title := strings.TrimSpace(doc.Find("title").Text())
				content := extractMainContentFromSelection(doc.Selection)
				wordCount := len(strings.Fields(content))
				if wordCount >= 10 {
					page := models.CrawledPage{
						URL:        normalizedStartURL,
						Title:      title,
						Content:    content,
						CrawledAt:  time.Now(),
						StatusCode: 200,
						Size:       int64(len(content)),
						WordCount:  wordCount,
					}
					pagesMu.Lock()
					pages = append(pages, page)
					pagesMu.Unlock()
					result.Title = title
					result.Content = content
					initialPageMu.Lock()
					initialPageProcessed = true
					initialPageMu.Unlock()
				}
			}
		} else if renderErr != nil {
			fmt.Printf("⚠️ JS render failed: %v\n", renderErr)
		}
	}

	// Visit the normalized start URL first (for links and as fallback)
	fmt.Printf("🚀 Starting crawl: %s\n", normalizedStartURL)
	err = c.Visit(normalizedStartURL)
	if err != nil {
		// If normalized visit fails, try original URL
		fmt.Printf("⚠️ Trying original URL: %s\n", cfg.URL)
		queuedMu.Lock()
		queued.Store(cfg.URL, true)
		queuedMu.Unlock()

		err = c.Visit(cfg.URL)
		if err != nil {
			// If both fail, check if it's "already visited" which might be OK
			if strings.Contains(err.Error(), "already visited") {
				// Wait a bit and check if we got pages anyway
				c.Wait()
				pagesMu.Lock()
				pagesCount := len(pages)
				pagesMu.Unlock()

				if pagesCount == 0 {
					return nil, fmt.Errorf("URL %s already visited with no pages processed", normalizedStartURL)
				}
				// If we have pages, it's OK
			} else {
				return nil, fmt.Errorf("failed to start crawl: %w", err)
			}
		}
	}

	// Wait for async crawl to complete
	c.Wait()

	// Final validation
	initialPageMu.Lock()
	wasProcessed := initialPageProcessed
	initialPageMu.Unlock()

	pagesMu.Lock()
	pagesCount := len(pages)
	pagesMu.Unlock()

	// If no pages were crawled
	if pagesCount == 0 {
		if result.Error != nil {
			return nil, result.Error
		}
		if !wasProcessed {
			return nil, fmt.Errorf("initial URL %s was not processed", normalizedStartURL)
		}
		return result, nil
	}

	result.Pages = pages
	result.PagesCrawled = len(pages)

	// Clear error if we got pages
	if result.Error != nil && len(pages) > 0 {
		result.Error = nil
	}

	return result, result.Error
}

// renderPageHTML launches a headless browser, waits for readiness and network idle, then returns HTML
func renderPageHTML(urlStr string, timeout time.Duration, waitSelector string, networkIdleAfter time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx,
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"),
	)
	defer allocCancel()

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	var html string

	// Step 1: Navigate
	if err := chromedp.Run(browserCtx, chromedp.Navigate(urlStr)); err != nil {
		return "", err
	}

	// Step 2: Quick ready check (soft-fail)
	if stepCtx, cancelStep := context.WithTimeout(browserCtx, 10*time.Second); true {
		defer cancelStep()
		_ = chromedp.Run(stepCtx, chromedp.WaitReady("body", chromedp.ByQuery))
	}

	// Step 3: Optional selector wait (soft-fail)
	if waitSelector != "" {
		if stepCtx, cancelStep := context.WithTimeout(browserCtx, 15*time.Second); true {
			defer cancelStep()
			_ = chromedp.Run(stepCtx, chromedp.WaitVisible(waitSelector, chromedp.ByQuery))
		}
	}

	// Step 4: Optional network idle (soft-fail, cap to 5s)
	if networkIdleAfter > 0 {
		idleCap := networkIdleAfter
		if idleCap > 5*time.Second {
			idleCap = 5 * time.Second
		}
		if stepCtx, cancelStep := context.WithTimeout(browserCtx, idleCap+1*time.Second); true {
			defer cancelStep()
			_ = chromedp.Run(stepCtx, waitForNetworkIdle(idleCap))
		}
	}

	// Step 5: Always attempt to read HTML
	if err := chromedp.Run(browserCtx, chromedp.OuterHTML("html", &html, chromedp.ByQuery)); err != nil {
		return "", err
	}
	return html, nil
}

// waitForNetworkIdle waits until no network requests are in flight for the given duration
func waitForNetworkIdle(d time.Duration) chromedp.ActionFunc {
	// Heuristic implemented in the page: track last network activity via PerformanceObserver
	js := `(function(waitMs){
      return new Promise((resolve)=>{
        if (!('PerformanceObserver' in window)) {
          setTimeout(resolve, waitMs);
          return;
        }
        let last = Date.now();
        const obs = new PerformanceObserver(()=>{ last = Date.now(); });
        try { obs.observe({entryTypes:['resource','navigation']}); } catch(e) {}
        const tick = () => {
          if (Date.now()-last >= waitMs) { try { obs.disconnect(); } catch(e){} resolve(); return; }
          setTimeout(tick, 100);
        };
        tick();
      });
    })(%d);`
	return func(ctx context.Context) error {
		return chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(js, int(d.Milliseconds())), nil))
	}
}

// extractMainContentFromSelection extracts main content from a goquery Selection
func extractMainContentFromSelection(selection *goquery.Selection) string {
	doc := selection.Clone()

	// Remove unwanted elements
	doc.Find("script, style, nav, footer, header, aside, .nav, .navbar, .footer, .header, .sidebar, .advertisement, .ads, .skip-link").Remove()

	// Try semantic HTML5 elements first
	contentSelectors := []string{
		"main",
		"article",
		"[role='main']",
		".main-content",
		".content",
		"#content",
		".post",
		".entry",
		"body",
	}

	var content strings.Builder
	contentFound := false

	for _, selector := range contentSelectors {
		doc.Find(selector).Each(func(_ int, s *goquery.Selection) {
			text := strings.TrimSpace(s.Text())
			if len(text) > 100 {
				content.WriteString(text)
				content.WriteString("\n\n")
				contentFound = true
			}
		})

		if contentFound {
			break
		}
	}

	if !contentFound {
		bodyText := doc.Find("body").Text()
		content.WriteString(bodyText)
	}

	text := strings.TrimSpace(content.String())

	// Clean up excessive whitespace
	lines := strings.Split(text, "\n")
	var cleanedLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleanedLines = append(cleanedLines, line)
		}
	}

	return strings.Join(cleanedLines, "\n")
}

// isURLAllowed checks if a URL is allowed based on configuration
func isURLAllowed(urlStr string, cfg CrawlConfig, allowedDomains []string) bool {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return false
	}

	// Only allow http/https
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}

	// Check domain
	if len(allowedDomains) > 0 {
		hostname := strings.ToLower(parsed.Hostname())
		domainAllowed := false
		for _, allowedDomain := range allowedDomains {
			allowedDomain = strings.ToLower(strings.TrimPrefix(allowedDomain, "www."))
			hostnameClean := strings.ToLower(strings.TrimPrefix(hostname, "www."))
			if hostnameClean == allowedDomain || strings.HasSuffix(hostnameClean, "."+allowedDomain) {
				domainAllowed = true
				break
			}
		}
		if !domainAllowed {
			return false
		}
	}

	// Check path patterns
	if len(cfg.AllowedPaths) > 0 {
		pathAllowed := false
		for _, allowedPath := range cfg.AllowedPaths {
			if strings.HasPrefix(parsed.Path, allowedPath) {
				pathAllowed = true
				break
			}
		}
		if !pathAllowed {
			return false
		}
	}

	// Filter out common non-content URLs
	excludedPatterns := []string{
		"/wp-json/",
		"/api/",
		"/ajax/",
		".pdf",
		".jpg",
		".jpeg",
		".png",
		".gif",
		".svg",
		".css",
		".js",
		".xml",
		"/feed/",
		"/rss/",
		"/atom/",
		"/search?",
		"/?s=",
		"/wp-admin/",
		"/wp-includes/",
	}

	pathLower := strings.ToLower(parsed.Path)
	queryLower := strings.ToLower(parsed.RawQuery)

	for _, pattern := range excludedPatterns {
		if strings.Contains(pathLower, pattern) || strings.Contains(queryLower, pattern) {
			return false
		}
	}

	return true
}
