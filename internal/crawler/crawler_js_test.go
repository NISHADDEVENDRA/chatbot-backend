package crawler

import (
	"testing"
	"time"
)

// Network/GUI dependent; validate that the JS-rendering path can run and returns some content.
func TestRenderJSFirstPage_Shallow(t *testing.T) {
	cfg := CrawlConfig{
		URL:              "https://example.com/",
		RenderJS:         true,
		RenderTimeout:    10 * time.Second,
		WaitSelector:     "body",
		NetworkIdleAfter: 300 * time.Millisecond,
		MaxPages:         1,
		FollowLinks:      false,
	}

	res, err := CrawlURL(cfg)
	if err != nil {
		// In CI/containers without Chrome this may fail; mark as skipped with context
		t.Skipf("JS-render test skipped due to environment: %v", err)
		return
	}
	if res == nil || len(res.Pages) == 0 {
		t.Fatalf("expected at least one page from JS-render path")
	}
}
