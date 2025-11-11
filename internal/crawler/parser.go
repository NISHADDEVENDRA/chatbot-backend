package crawler

import (
	"encoding/json"
	"strings"
	"time"

	"saas-chatbot-platform/models"

	"github.com/PuerkitoBio/goquery"
)

// ExtractProductData extracts structured product data from a page
func ExtractProductData(doc *goquery.Document, pageURL string) *models.Product {
	product := &models.Product{
		URL:         pageURL,
		Attributes:  make(map[string]interface{}),
		ExtractedAt: getCurrentTime(),
	}

	// Extract title/name (common selectors)
	nameSelectors := []string{
		"h1.product-title",
		"h1.product-name",
		"h1[itemprop='name']",
		".product-title",
		".product-name",
		"h1",
	}

	for _, selector := range nameSelectors {
		name := strings.TrimSpace(doc.Find(selector).First().Text())
		if name != "" {
			product.Name = name
			break
		}
	}

	// Extract price (common selectors)
	priceSelectors := []string{
		".price",
		".product-price",
		"[itemprop='price']",
		".current-price",
		".sale-price",
	}

	for _, selector := range priceSelectors {
		price := strings.TrimSpace(doc.Find(selector).First().Text())
		if price != "" {
			product.Price = cleanPrice(price)
			break
		}
	}

	// Extract SKU from JSON-LD structured data
	doc.Find("script[type='application/ld+json']").Each(func(_ int, s *goquery.Selection) {
		jsonText := s.Text()
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(jsonText), &data); err == nil {
			// Try to extract SKU
			if sku, ok := extractSKUFromJSON(data); ok {
				product.SKU = sku
			}

			// Try to extract more product info
			if name, ok := data["name"].(string); ok && product.Name == "" {
				product.Name = name
			}
			if price, ok := extractPriceFromJSON(data); ok && product.Price == "" {
				product.Price = price
			}
			if desc, ok := data["description"].(string); ok {
				product.Description = desc
			}
		}
	})

	// Extract description
	descSelectors := []string{
		".product-description",
		"[itemprop='description']",
		".description",
		"meta[name='description']",
	}

	for _, selector := range descSelectors {
		desc := ""
		if strings.HasPrefix(selector, "meta") {
			desc, _ = doc.Find(selector).Attr("content")
		} else {
			desc = strings.TrimSpace(doc.Find(selector).First().Text())
		}
		if desc != "" && product.Description == "" {
			product.Description = desc
			break
		}
	}

	// Extract image
	imgSelectors := []string{
		".product-image img",
		"[itemprop='image']",
		"img[itemprop='image']",
		".main-image img",
	}

	for _, selector := range imgSelectors {
		imgURL, exists := doc.Find(selector).First().Attr("src")
		if !exists {
			imgURL, exists = doc.Find(selector).First().Attr("data-src")
		}
		if exists && imgURL != "" {
			product.ImageURL = imgURL
			break
		}
	}

	// Extract stock status
	if doc.Find(".in-stock, .available, [data-in-stock='true']").Length() > 0 {
		product.InStock = true
	} else if doc.Find(".out-of-stock, .unavailable, [data-in-stock='false']").Length() > 0 {
		product.InStock = false
	}

	// Clean up extracted data
	product.Name = strings.TrimSpace(product.Name)
	product.Description = strings.TrimSpace(product.Description)

	return product
}

// ExtractProductsFromPage extracts all products from a listing page
func ExtractProductsFromPage(doc *goquery.Document, baseURL string) []models.Product {
	var products []models.Product

	// Common product listing selectors
	productSelectors := []string{
		".product-item",
		".product-card",
		"[data-product-id]",
		".product",
	}

	var productElements *goquery.Selection

	for _, selector := range productSelectors {
		productElements = doc.Find(selector)
		if productElements.Length() > 0 {
			break
		}
	}

	if productElements == nil || productElements.Length() == 0 {
		return products
	}

	productElements.Each(func(_ int, s *goquery.Selection) {
		product := &models.Product{
			URL:         baseURL,
			Attributes:  make(map[string]interface{}),
			ExtractedAt: getCurrentTime(),
		}

		// Extract product link
		link := s.Find("a").First()
		if href, exists := link.Attr("href"); exists {
			product.URL = resolveURL(baseURL, href)
		}

		// Extract name
		nameSelectors := []string{"h2", "h3", ".product-name", ".title", "[itemprop='name']"}
		for _, sel := range nameSelectors {
			name := strings.TrimSpace(s.Find(sel).First().Text())
			if name != "" {
				product.Name = name
				break
			}
		}

		// Extract price
		priceSelectors := []string{".price", ".product-price", "[itemprop='price']"}
		for _, sel := range priceSelectors {
			price := strings.TrimSpace(s.Find(sel).First().Text())
			if price != "" {
				product.Price = cleanPrice(price)
				break
			}
		}

		if product.Name != "" {
			products = append(products, *product)
		}
	})

	return products
}

// Helper functions

func cleanPrice(price string) string {
	// Remove currency symbols and clean up
	price = strings.TrimSpace(price)
	price = strings.ReplaceAll(price, "$", "")
	price = strings.ReplaceAll(price, "€", "")
	price = strings.ReplaceAll(price, "£", "")
	price = strings.ReplaceAll(price, "₹", "")
	price = strings.ReplaceAll(price, ",", "")
	return strings.TrimSpace(price)
}

func extractSKUFromJSON(data map[string]interface{}) (string, bool) {
	if sku, ok := data["sku"].(string); ok {
		return sku, true
	}
	if mpn, ok := data["mpn"].(string); ok {
		return mpn, true
	}
	return "", false
}

func extractPriceFromJSON(data map[string]interface{}) (string, bool) {
	if price, ok := data["price"].(string); ok {
		return price, true
	}
	if offers, ok := data["offers"].(map[string]interface{}); ok {
		if price, ok := offers["price"].(string); ok {
			return price, true
		}
	}
	return "", false
}

func resolveURL(baseURL, relativeURL string) string {
	if strings.HasPrefix(relativeURL, "http://") || strings.HasPrefix(relativeURL, "https://") {
		return relativeURL
	}
	if strings.HasPrefix(relativeURL, "/") {
		return baseURL + relativeURL
	}
	return baseURL + "/" + relativeURL
}

func getCurrentTime() time.Time {
	return time.Now()
}
