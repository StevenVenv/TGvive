package model

// CommentRule controls comment mirroring behavior for a strategy.
type CommentRule struct {
	Enable bool `json:"enable"`

	// FilterMode: "owner_only" (recommended) or "all" (risky).
	// Aliases for backward compatibility: "whitelist" -> "owner_only".
	FilterMode string `json:"filter_mode"`

	TrustedUserIDs []int64 `json:"trusted_user_ids"`
	AllowAnonymous bool    `json:"allow_anonymous"`

	// AllowedTypes uses the same content type keys as the engine:
	// text|image|video|audio|file|other
	// Aliases: photo->image, document->file
	AllowedTypes []string `json:"allowed_types"`

	// BlockKeywords is a case-insensitive substring blacklist against msg.Message/caption.
	BlockKeywords []string `json:"block_keywords"`
}
