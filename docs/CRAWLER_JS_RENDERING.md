# JS-rendered Crawling (Headless Chrome)

This system supports optional JavaScript-rendered crawling for the initial page of a crawl job. Use it for SPA/JS-heavy sites where static HTML does not contain content.

## How it works

- Backend adds `chromedp`-powered rendering for the first page only.
- After rendering, content is extracted with the same `goquery`-based extractor.
- Link following remains Colly-based for performance and resource control.

## Request fields

- `render_js` (bool): Enable JS rendering for the first page.
- `wait_selector` (string): CSS selector to wait for before extraction; recommended for SPA hydration (e.g., `#root .article`).
- `render_timeout_ms` (int): Overall render timeout; default 25000.

Bulk and single-page endpoints accept these fields and forward them to the crawler.

## Operational notes

- Requires Google Chrome/Chromium on the server. `chromedp` will use the system browser.
- Network idle heuristic waits until no requests are in-flight for ~800ms to stabilize dynamic content.
- Timeouts cap total rendering duration to avoid runaways.

## When to use

- Dynamic/SPAs (Next.js, React, Vue, Angular) where server HTML is minimal.
- Pages gated by client-side routing or hydration.

Avoid enabling on large bulk crawls unless necessary—rendering is heavier than direct HTTP.


