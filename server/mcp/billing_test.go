package mcp

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

func TestMaybeDeductUnderstandingTokensStoresCacheUsageMetadata(t *testing.T) {
	cases := []struct {
		name                  string
		usage                 config.TokenUsage
		wantCachedInput       int64
		wantCacheReadInput    int64
		wantCacheCreation     int64
		wantTotalTokens       int64
		wantTransactionAmount int
	}{
		{
			name: "split cache usage",
			usage: config.TokenUsage{
				InputTokens:              1000,
				CacheReadInputTokens:     300,
				CacheCreationInputTokens: 200,
				OutputTokens:             100,
				TotalTokens:              1600,
			},
			wantCacheReadInput:    300,
			wantCacheCreation:     200,
			wantTotalTokens:       1600,
			wantTransactionAmount: -1470,
		},
		{
			name: "legacy cached input maps to cache read",
			usage: config.TokenUsage{
				InputTokens:       1000,
				CachedInputTokens: 250,
				OutputTokens:      100,
				TotalTokens:       1100,
			},
			wantCachedInput:       250,
			wantCacheReadInput:    250,
			wantTotalTokens:       1100,
			wantTransactionAmount: -975,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldBillSvc := billSvc
			t.Cleanup(func() { billSvc = oldBillSvc })

			ctx := context.Background()
			db := repositoryTestDB(t)
			repo := repository.New(db)
			userID := "user-understanding-cache-metadata"
			if err := repo.Users().Create(ctx, &model.User{
				ID:             userID,
				Email:          userID + "@example.com",
				Password:       "hashed",
				Tier:           model.TierFree,
				InviteCode:     "cachemeta",
				CreditsBalance: 10_000,
			}); err != nil {
				t.Fatalf("create user: %v", err)
			}

			cfg := &config.Config{
				ImageUnderstanding: config.UnderstandingRuntimeConfig{
					ProviderKey: "anthropic",
					Model:       "claude-vision-test",
				},
				ModelPrices: config.ModelPricesConfig{
					TokenModels: map[string]config.TokenModelPrice{
						"anthropic/claude-vision-test": {
							Currency:           "CNY",
							Unit:               1000,
							Input:              config.FlexibleFloat(1),
							Output:             config.FlexibleFloat(2),
							CacheReadInput:     config.FlexibleFloat(0.1),
							CacheCreationInput: config.FlexibleFloat(1.2),
						},
					},
				},
				Billing: config.BillingConfig{
					CreditsPerCNY:         1000,
					TierMultipliers:       map[string]float64{"free": 1},
					DefaultUserMultiplier: 1,
					MinimumChargeCredits:  1,
				},
			}
			logger := zerolog.New(io.Discard)
			creditSvc := service.NewCreditService(repo, &cfg.Credits, &logger)
			SetBillingServices(creditSvc, nil, cfg)

			charged, err := maybeDeductUnderstandingTokens(ctx, userID, "", model.CreditTypeImageUnderstanding, tc.usage)
			if err != nil {
				t.Fatalf("maybeDeductUnderstandingTokens: %v", err)
			}
			if charged != -tc.wantTransactionAmount {
				t.Fatalf("charged credits = %d, want %d", charged, -tc.wantTransactionAmount)
			}

			txs, _, err := creditSvc.ListTransactions(ctx, userID, 0, 10)
			if err != nil {
				t.Fatalf("list transactions: %v", err)
			}
			if len(txs) != 1 {
				t.Fatalf("transaction count = %d, want 1", len(txs))
			}
			if txs[0].Type != model.CreditTypeImageUnderstanding || txs[0].Amount != tc.wantTransactionAmount {
				t.Fatalf("transaction = %s %d, want %s %d", txs[0].Type, txs[0].Amount, model.CreditTypeImageUnderstanding, tc.wantTransactionAmount)
			}

			var metadata model.CreditTransactionMetadata
			if err := json.Unmarshal(txs[0].Metadata, &metadata); err != nil {
				t.Fatalf("decode metadata: %v", err)
			}
			if metadata.CachedInputTokens != tc.wantCachedInput ||
				metadata.CacheReadInputTokens != tc.wantCacheReadInput ||
				metadata.CacheCreationInputTokens != tc.wantCacheCreation ||
				metadata.TotalTokens != tc.wantTotalTokens {
				t.Fatalf("metadata cache usage = cached %d read %d creation %d total %d, want cached %d read %d creation %d total %d",
					metadata.CachedInputTokens,
					metadata.CacheReadInputTokens,
					metadata.CacheCreationInputTokens,
					metadata.TotalTokens,
					tc.wantCachedInput,
					tc.wantCacheReadInput,
					tc.wantCacheCreation,
					tc.wantTotalTokens,
				)
			}
		})
	}
}
