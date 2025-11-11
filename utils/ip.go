package utils

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// GetClientIP extracts the real client IP address from HTTP request
func GetClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxies/load balancers)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			ip := strings.TrimSpace(ips[0])
			if isValidIP(ip) {
				return ip
			}
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		if isValidIP(xri) {
			return xri
		}
	}

	// Check X-Forwarded header
	if xf := r.Header.Get("X-Forwarded"); xf != "" {
		if isValidIP(xf) {
			return xf
		}
	}

	// Check CF-Connecting-IP header (Cloudflare)
	if cfip := r.Header.Get("CF-Connecting-IP"); cfip != "" {
		if isValidIP(cfip) {
			return cfip
		}
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	if isValidIP(ip) {
		return ip
	}

	return r.RemoteAddr
}

// isValidIP checks if the given string is a valid IP address
func isValidIP(ip string) bool {
	return net.ParseIP(ip) != nil
}

// GetUserAgent extracts user agent from request
func GetUserAgent(r *http.Request) string {
	return r.Header.Get("User-Agent")
}

// GetReferrer extracts referrer from request
func GetReferrer(r *http.Request) string {
	referrer := r.Header.Get("Referer")
	if referrer == "" {
		referrer = r.Header.Get("Referrer")
	}
	return referrer
}

// GeolocationData represents comprehensive IP geolocation information
type GeolocationData struct {
	IP            string  `json:"ip"`
	Country       string  `json:"country"`
	CountryCode   string  `json:"country_code"`
	Region        string  `json:"region"`
	RegionName    string  `json:"region_name"`
	City          string  `json:"city"`
	Latitude      float64 `json:"latitude"`
	Longitude     float64 `json:"longitude"`
	Timezone      string  `json:"timezone"`
	ISP           string  `json:"isp"`
	Organization  string  `json:"org"`
	AS            string  `json:"as"`
	Query         string  `json:"query"`
	Status        string  `json:"status"`
	Message       string  `json:"message"`
	Continent     string  `json:"continent"`
	ContinentCode string  `json:"continent_code"`
	District      string  `json:"district"`
	Offset        int     `json:"offset"`
	Currency      string  `json:"currency"`
	Mobile        bool    `json:"mobile"`
	Proxy         bool    `json:"proxy"`
	Hosting       bool    `json:"hosting"`
}

// IPType represents the type of IP address
type IPType string

const (
	IPTypeResidential IPType = "Residential"
	IPTypeDatacenter  IPType = "Datacenter"
	IPTypeVPN         IPType = "VPN"
	IPTypeProxy       IPType = "Proxy"
	IPTypeMobile      IPType = "Mobile"
	IPTypeUnknown     IPType = "Unknown"
)

// GetGeolocationData fetches comprehensive geolocation data for an IP address
func GetGeolocationData(ip string) *GeolocationData {
	// Handle local/private IPs
	if isPrivateIP(ip) {
		return &GeolocationData{
			IP:           ip,
			Country:      "Local Network",
			CountryCode:  "LOCAL",
			Region:       "Local",
			RegionName:   "Local Network",
			City:         "Local",
			Latitude:     0.0,
			Longitude:    0.0,
			Timezone:     "UTC",
			ISP:          "Local Network",
			Organization: "Local Network",
			AS:           "Local",
			Status:       "success",
		}
	}

	// Use ipapi.co for geolocation data (free tier: 1000 requests/month)
	// Alternative: ipinfo.io, maxmind, etc.
	url := fmt.Sprintf("http://ip-api.com/json/%s?fields=status,message,continent,continentCode,country,countryCode,region,regionName,city,district,zip,lat,lon,timezone,offset,currency,isp,org,as,query,mobile,proxy,hosting", ip)

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return &GeolocationData{
			IP:      ip,
			Country: "Unknown",
			Status:  "error",
			Message: err.Error(),
		}
	}
	defer resp.Body.Close()

	var data GeolocationData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return &GeolocationData{
			IP:      ip,
			Country: "Unknown",
			Status:  "error",
			Message: err.Error(),
		}
	}

	return &data
}

// GetIPType determines the type of IP address based on geolocation data
func GetIPType(geoData *GeolocationData) IPType {
	if geoData == nil {
		return IPTypeUnknown
	}

	// Check for local/private IPs
	if isPrivateIP(geoData.IP) {
		return IPTypeResidential
	}

	// Check API response flags
	if geoData.Proxy {
		return IPTypeProxy
	}
	if geoData.Hosting {
		return IPTypeDatacenter
	}
	if geoData.Mobile {
		return IPTypeMobile
	}

	// Check ISP/Organization for common datacenter patterns
	isp := strings.ToLower(geoData.ISP)
	org := strings.ToLower(geoData.Organization)

	datacenterKeywords := []string{
		"amazon", "aws", "google", "gcp", "microsoft", "azure", "digital ocean", "linode",
		"vultr", "ovh", "hetzner", "scaleway", "cloudflare", "fastly", "datacenter",
		"hosting", "server", "cloud", "vps", "dedicated", "colocation",
	}

	for _, keyword := range datacenterKeywords {
		if strings.Contains(isp, keyword) || strings.Contains(org, keyword) {
			return IPTypeDatacenter
		}
	}

	// Check AS (Autonomous System) for VPN/Proxy patterns
	as := strings.ToLower(geoData.AS)
	vpnKeywords := []string{"vpn", "proxy", "tor", "anonymizer", "privacy"}
	for _, keyword := range vpnKeywords {
		if strings.Contains(as, keyword) {
			return IPTypeVPN
		}
	}

	// Default to residential if no flags are set
	return IPTypeResidential
}

// isPrivateIP checks if an IP address is private/local
func isPrivateIP(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	// Check for IPv4 private ranges
	if parsedIP.To4() != nil {
		return parsedIP.IsPrivate() || parsedIP.IsLoopback() || parsedIP.IsUnspecified()
	}

	// Check for IPv6 private ranges
	return parsedIP.IsPrivate() || parsedIP.IsLoopback() || parsedIP.IsUnspecified()
}

// GetCountryFromIP performs basic IP-based country detection
// This is a simple implementation - in production, you might want to use a service like MaxMind
func GetCountryFromIP(ip string) string {
	geoData := GetGeolocationData(ip)
	return geoData.Country
}

// GetCityFromIP performs basic IP-based city detection
func GetCityFromIP(ip string) string {
	geoData := GetGeolocationData(ip)
	return geoData.City
}
