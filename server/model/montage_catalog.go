package model

// PinnedMontagePipelineKeys and PinnedMontagePlaybookKeys are the stable keys
// exposed by the OpenMontage revision bundled in the Montage runtime image.
// Display metadata remains server-owned, while validation uses these shared
// definitions so runtime admission and the capability API cannot drift.
var pinnedMontagePipelineKeys = [...]string{
	"animated-explainer", "animation", "avatar-spokesperson", "character-animation",
	"cinematic", "clip-factory", "documentary-montage", "framework-smoke", "hybrid",
	"localization-dub", "podcast-repurpose", "screen-demo", "talking-head",
}

var pinnedMontagePlaybookKeys = [...]string{
	"anime-ghibli", "clean-professional", "flat-motion-graphics", "minimalist-diagram", "premium-minimalist",
}

func PinnedMontagePipelineKeys() []string {
	return append([]string(nil), pinnedMontagePipelineKeys[:]...)
}

func IsPinnedMontagePipeline(key string) bool {
	for _, item := range pinnedMontagePipelineKeys {
		if item == key {
			return true
		}
	}
	return false
}

func IsPinnedMontagePlaybook(key string) bool {
	if key == "" || key == "auto" {
		return true
	}
	for _, item := range pinnedMontagePlaybookKeys {
		if item == key {
			return true
		}
	}
	return false
}

func PinnedMontageOutputProfiles() []MontageOutputProfile {
	return []MontageOutputProfile{
		{Key: "youtube_landscape", DisplayName: "YouTube 横版", Width: 1920, Height: 1080, AspectRatio: "16:9"},
		{Key: "youtube_4k", DisplayName: "YouTube 4K", Width: 3840, Height: 2160, AspectRatio: "16:9"},
		{Key: "youtube_shorts", DisplayName: "YouTube Shorts", Width: 1080, Height: 1920, AspectRatio: "9:16"},
		{Key: "instagram_reels", DisplayName: "Instagram Reels", Width: 1080, Height: 1920, AspectRatio: "9:16"},
		{Key: "instagram_feed", DisplayName: "Instagram Feed", Width: 1080, Height: 1080, AspectRatio: "1:1"},
		{Key: "tiktok", DisplayName: "TikTok", Width: 1080, Height: 1920, AspectRatio: "9:16"},
		{Key: "linkedin", DisplayName: "LinkedIn", Width: 1920, Height: 1080, AspectRatio: "16:9"},
		{Key: "cinematic", DisplayName: "电影宽银幕", Width: 2560, Height: 1080, AspectRatio: "21:9"},
	}
}
