package events

import "strings"

func ParseDeviceType(ua string) string {
	lc := strings.ToLower(ua)
	if strings.Contains(lc, "ipad") || strings.Contains(lc, "tablet") {
		return "tablet"
	}
	if strings.Contains(lc, "mobile") || strings.Contains(lc, "android") || strings.Contains(lc, "iphone") {
		return "mobile"
	}
	return "desktop"
}

func ParseIsBot(ua string) bool {
	lc := strings.ToLower(ua)
	for _, kw := range []string{"bot", "crawl", "spider", "scrapy", "curl", "wget", "python"} {
		if strings.Contains(lc, kw) {
			return true
		}
	}
	return false
}
