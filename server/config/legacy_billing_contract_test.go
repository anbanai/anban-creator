package config_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProductionSourcesRejectLegacyDynamicBillingContracts(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	forbidden := []string{
		"SettleAgentRuntime", "DeductForOperation", "DeductForMCPOperation", "ReserveCredits",
		"payment_required", "billing_shortfall", "total_cost_usd", "tier_multipliers",
		"billing_multiplier", "agent_runtime_reserve", "daily_sign_in", "register_bonus",
		"CreditService", "CreditTransaction", "CreditsBalance", `"/credits`, "model_prices:",
		"recharge_tiers:", "credit_cost:",
	}
	allowedProviderErrors := map[string]map[string]bool{
		filepath.Join("server", "app", "image", "openai.go"):     {"payment_required": true},
		filepath.Join("server", "app", "image", "volcengine.go"): {"payment_required": true},
	}
	roots := []string{"server", filepath.Join("agent-ts", "src"), filepath.Join("studio", "src")}
	for _, relativeRoot := range roots {
		walkRoot := filepath.Join(root, relativeRoot)
		err := filepath.WalkDir(walkRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			switch filepath.Ext(path) {
			case ".go", ".yaml", ".yml", ".ts", ".tsx", ".vue", ".json":
			default:
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			relativePath := strings.TrimPrefix(path, root+string(filepath.Separator))
			for _, token := range forbidden {
				if allowedProviderErrors[relativePath][token] {
					continue
				}
				if strings.Contains(string(raw), token) {
					t.Errorf("%s contains removed billing contract %q", relativePath, token)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", relativeRoot, err)
		}
	}
}
