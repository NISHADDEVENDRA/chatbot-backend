package models

type EmbedSettings struct {
	AllowEmbedding bool     `json:"allow_embedding"`
	AllowedDomains []string `json:"allowed_domains"`
	SecureMode     bool     `json:"secure_mode"`
}

type EmbedSnippet struct {
	ClientID    string `json:"client_id"`
	EmbedSecret string `json:"embed_secret"`
	ScriptTag   string `json:"script_tag"`
	IframeTag   string `json:"iframe_tag"`
}

type TokenUsage struct {
	Used      int     `json:"used"`
	Limit     int     `json:"limit"`
	Remaining int     `json:"remaining"`
	Usage     float64 `json:"usage_percentage"`
}

type SystemHealth struct {
	Status         string                 `json:"status"`
	Timestamp      string                 `json:"timestamp"`
	Database       string                 `json:"database"`
	GeminiAPI      string                 `json:"gemini_api"`
	ActiveSessions int                    `json:"active_sessions"`
	Metrics        map[string]interface{} `json:"metrics"`
}