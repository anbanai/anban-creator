package handler

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/storage"
)

func montageSourceAssetURLs(input *model.MontageInput) []string {
	if input == nil || len(input.SourceAssets) == 0 {
		return nil
	}
	urls := make([]string, 0, len(input.SourceAssets))
	for _, asset := range input.SourceAssets {
		if asset.URL != "" {
			urls = append(urls, asset.URL)
		}
	}
	return urls
}

func rewriteFinalizedMontageAssetURLs(input *model.MontageInput, rewrites map[string]string) {
	if input == nil {
		return
	}
	for i := range input.SourceAssets {
		input.SourceAssets[i].URL = rewriteFinalizedUploadURL(input.SourceAssets[i].URL, rewrites)
	}
}

func validateMontageSourceAssetURLs(input *model.MontageInput) error {
	for _, url := range montageSourceAssetURLs(input) {
		if !validAttachmentURL(url) {
			return fmt.Errorf("montage_input.source_assets.url must be an internal file path or an http(s) URL")
		}
	}
	return nil
}

func taskAPIResponses(tasks []*model.Task, store storage.Provider) []map[string]any {
	items := make([]map[string]any, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, taskAPIResponse(task, store))
	}
	return items
}

func taskAPIResponse(task *model.Task, store storage.Provider) map[string]any {
	if task == nil {
		return nil
	}
	resp := modelAPIMap(task)
	resp["billing_total_credits"] = task.BillingPriceCredits
	rewriteMontageAPIField(resp, task.Type, task.MontageInput.Data())
	enrichOwnedObjectKeys(resp, store)
	sanitizeTaskAPIResponse(resp, task)
	return resp
}

// taskAPIPrivateFields are task fields that exist for Server-side execution,
// identity, cost accounting, and scheduling only. They must never reach the
// Studio API.
var taskAPIPrivateFields = []string{
	"agent_profile_fingerprint",
	"current_execution_id",
	"last_heartbeat_at",
	"max_retries",
	"rate_limit_retry_count",
	"retry_count",
	"billing_quote_id",
}

// sanitizeTaskAPIResponse removes Server-internal task fields and replaces the
// full agent profile snapshot with its lean public projection.
func sanitizeTaskAPIResponse(resp map[string]any, task *model.Task) {
	for _, key := range taskAPIPrivateFields {
		delete(resp, key)
	}
	if task != nil && task.AgentProfileSnapshot.ProfileID != "" {
		resp["agent_profile_snapshot"] = modelAPIMap(task.AgentProfileSnapshot.APISnapshot())
	} else {
		delete(resp, "agent_profile_snapshot")
	}
	if raw, ok := resp["progress_log"].(string); ok {
		if clean := sanitizeTaskAPIPublicLog(raw); clean != raw {
			if clean == "" {
				delete(resp, "progress_log")
			} else {
				resp["progress_log"] = clean
			}
		}
	}
}

// sanitizeTaskAPIPublicLog filters persisted agent log lines down to the
// user-facing progress surface, dropping internal Claude Code diagnostics
// (e.g. "[claude-code:unrecognized_model] ...") and tool-trace records
// ("Using tool: ..."). The Agent runtime no longer writes these lines; this is
// defense-in-depth for legacy rows that already contain them.
func sanitizeTaskAPIPublicLog(raw string) string {
	if raw == "" {
		return ""
	}
	lines := strings.Split(raw, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimRight(line, "\r \t")
		if trimmed == "" || isInternalAgentLogLine(trimmed) {
			continue
		}
		kept = append(kept, trimmed)
	}
	return strings.Join(kept, "\n")
}

func isInternalAgentLogLine(line string) bool {
	return strings.HasPrefix(line, "[claude-code:") || strings.HasPrefix(line, "Using tool:")
}

func planAPIResponses(plans []*model.Plan, store storage.Provider) []map[string]any {
	items := make([]map[string]any, 0, len(plans))
	for _, plan := range plans {
		items = append(items, planAPIResponse(plan, store))
	}
	return items
}

func planAPIResponse(plan *model.Plan, store storage.Provider) map[string]any {
	if plan == nil {
		return nil
	}
	resp := modelAPIMap(plan)
	rewriteMontageAPIField(resp, plan.Type, plan.MontageInput.Data())
	enrichOwnedObjectKeys(resp, store)
	return resp
}

func enrichOwnedObjectKeys(resp map[string]any, store storage.Provider) {
	if resp == nil || store == nil {
		return
	}
	enrichOwnedObjectKeysValue(resp, store)
}

func enrichOwnedObjectKeysValue(value any, store storage.Provider) {
	switch value := value.(type) {
	case map[string]any:
		if rawURL, _ := value["url"].(string); rawURL != "" {
			if existing, _ := value["key"].(string); existing == "" {
				if key, ok := ownedStorageKey(store, rawURL); ok {
					value["key"] = key
				}
			}
		}
		for _, nested := range value {
			enrichOwnedObjectKeysValue(nested, store)
		}
	case []any:
		for _, nested := range value {
			enrichOwnedObjectKeysValue(nested, store)
		}
	}
}

func ownedStorageKey(store storage.Provider, rawURL string) (string, bool) {
	rawURL = strings.TrimSpace(rawURL)
	if store == nil || rawURL == "" || !store.IsOwnedURL(rawURL) {
		return "", false
	}
	return storage.StorageKeyFromURL(rawURL)
}

func modelAPIMap(value any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		return map[string]any{}
	}
	return resp
}

func rewriteMontageAPIField(resp map[string]any, platform string, input model.MontageInput) {
	delete(resp, "montage_input")
	if model.IsMontagePlatform(platform) {
		resp["montage_input"] = modelAPIMap(input)
	}
}
