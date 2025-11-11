package services

import (
	"fmt"
)

// Navigation item to features mapping
// When a navigation item is enabled, all its features are automatically enabled
var NavigationItemFeatures = map[string][]string{
	"dashboard": {
		"dashboard_view",
		"dashboard_stats",
		"dashboard_quick_actions",
	},
	"chat": {
		"chat_send",
		"chat_receive",
		"chat_interface",
		"chat_history_access",
		"chat_realtime",
	},
	"documents": {
		"pdf_upload",
		"document_view",
		"document_delete",
		"document_manage",
		"document_export",
		"document_status",
	},
	"branding": {
		"branding_logo_update",
		"branding_theme_update",
		"branding_welcome_update",
		"branding_prequestions_update",
		"branding_widget_config",
		"branding_launcher_config",
	},
	"analytics": {
		"analytics_view",
		"analytics_export",
		"analytics_charts",
		"analytics_metrics",
	},
	"token_usage": {
		"token_usage_view",
		"token_usage_history",
		"token_usage_charts",
		"token_limit_view",
	},
	"quality_dashboard": {
		"quality_metrics_view",
		"quality_feedback_view",
		"quality_alerts_view",
		"quality_insights_view",
		"quality_trends_view",
	},
	"chat_history": {
		"chat_history_view",
		"chat_history_search",
		"chat_history_export",
		"chat_history_filter",
		"chat_history_details",
	},
	"images": {
		"image_upload",
		"image_view",
		"image_delete",
		"image_manage",
		"image_use_in_chat",
	},
	"facebook_posts": {
		"facebook_post_add",
		"facebook_post_delete",
		"facebook_post_manage",
		"facebook_post_view",
		"facebook_post_use_in_chat",
	},
	"instagram_posts": {
		"instagram_post_add",
		"instagram_post_delete",
		"instagram_post_manage",
		"instagram_post_view",
		"instagram_post_use_in_chat",
	},
	"website_embed": {
		"website_embed_configure",
		"website_embed_enable",
		"website_embed_view",
		"website_embed_use_in_chat",
	},
	"email_templates": {
		"email_template_create",
		"email_template_edit",
		"email_template_delete",
		"email_template_preview",
		"email_template_use",
	},
	"calendly": {
		"calendly_configure",
		"calendly_enable",
		"calendly_view",
		"calendly_use",
	},
	"qr_codes": {
		"qr_code_generate_call",
		"qr_code_generate_whatsapp",
		"qr_code_generate_telegram",
		"qr_code_download",
		"qr_code_configure",
	},
}

// ValidNavigationItems - List of all valid navigation items
var ValidNavigationItems = []string{
	"dashboard",
	"chat",
	"documents",
	"branding",
	"analytics",
	"token_usage",
	"quality_dashboard",
	"chat_history",
	"images",
	"facebook_posts",
	"instagram_posts",
	"website_embed",
	"email_templates",
	"calendly",
	"qr_codes",
}

// GetNavigationItemFeatures returns all features for a navigation item
func GetNavigationItemFeatures(navigationItem string) []string {
	if features, exists := NavigationItemFeatures[navigationItem]; exists {
		return features
	}
	return []string{}
}

// SyncFeaturesFromNavigationItems automatically populates enabled features based on navigation items
func SyncFeaturesFromNavigationItems(navigationItems []string) []string {
	enabledFeatures := make(map[string]bool)
	
	for _, item := range navigationItems {
		features := GetNavigationItemFeatures(item)
		for _, feature := range features {
			enabledFeatures[feature] = true
		}
	}
	
	// Convert map to slice
	result := make([]string, 0, len(enabledFeatures))
	for feature := range enabledFeatures {
		result = append(result, feature)
	}
	
	return result
}

// ValidateNavigationItems validates that all navigation items are valid
func ValidateNavigationItems(items []string) error {
	validMap := make(map[string]bool)
	for _, item := range ValidNavigationItems {
		validMap[item] = true
	}
	
	for _, item := range items {
		if !validMap[item] {
			return fmt.Errorf("invalid navigation item: %s", item)
		}
	}
	
	return nil
}

// HasNavigationItem checks if a navigation item is in the allowed list
func HasNavigationItem(allowedItems []string, item string) bool {
	// If allowedItems is empty, all items are allowed (backward compatible)
	if len(allowedItems) == 0 {
		return true
	}
	
	for _, allowed := range allowedItems {
		if allowed == item {
			return true
		}
	}
	return false
}

// HasFeature checks if a feature is in the enabled list
func HasFeature(enabledFeatures []string, feature string) bool {
	// If enabledFeatures is empty, all features are enabled (backward compatible)
	if len(enabledFeatures) == 0 {
		return true
	}
	
	for _, enabled := range enabledFeatures {
		if enabled == feature {
			return true
		}
	}
	return false
}

