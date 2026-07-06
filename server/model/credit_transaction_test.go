package model

import (
	"encoding/json"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestCreditTransactionMetadataColumnRoundTrip(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&CreditTransaction{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !db.Migrator().HasColumn(&CreditTransaction{}, "metadata") {
		t.Fatal("credit_transactions.metadata column missing")
	}

	raw, err := json.Marshal(CreditTransactionMetadata{
		Provider:     "openai",
		Model:        "gpt-test",
		InputTokens:  12,
		OutputTokens: 34,
		FinalCredits: 56,
	})
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	tx := CreditTransaction{
		UserID:       "user-1",
		Type:         CreditTypeArticleWrite,
		Amount:       -56,
		BalanceAfter: 944,
		Metadata:     raw,
	}
	if err := db.Create(&tx).Error; err != nil {
		t.Fatalf("create tx: %v", err)
	}

	var got CreditTransaction
	if err := db.First(&got, tx.ID).Error; err != nil {
		t.Fatalf("load tx: %v", err)
	}
	var metadata CreditTransactionMetadata
	if err := json.Unmarshal(got.Metadata, &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if metadata.Provider != "openai" || metadata.Model != "gpt-test" || metadata.FinalCredits != 56 {
		t.Fatalf("metadata round-trip = %#v", metadata)
	}
}
