package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func TopicScriptFingerprint(userID, projectID, assetID, platform string, duration int, aspect string) string {
	body, _ := json.Marshal(struct {
		AssetID     string `json:"asset_id"`
		ProjectID   string `json:"project_id"`
		UserID      string `json:"user_id"`
		Platform    string `json:"platform"`
		Duration    int    `json:"duration"`
		AspectRatio string `json:"aspect_ratio"`
	}{assetID, projectID, userID, platform, duration, aspect})
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:])
}
