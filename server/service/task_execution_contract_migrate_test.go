package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrateTaskExecutionContractsBackfillsOnlyExactFrozenPackIdentity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskExecution{}); err != nil {
		t.Fatal(err)
	}
	rows := []*model.TaskExecution{
		{ID: "known", TaskID: "known-task", Attempt: 1, AgentPackID: "seednote", AgentPackVersion: "1.0.0", AgentPackDigest: "84c58f8e77326bfc818bcd7e36cacab18b52f7e5d8d61e775516bfdabb817933"},
		{ID: "known-preserve-delivery", TaskID: "known-preserve-task", Attempt: 1, AgentPackID: "seednote", AgentPackVersion: "1.0.0", AgentPackDigest: "84c58f8e77326bfc818bcd7e36cacab18b52f7e5d8d61e775516bfdabb817933", AgentPackDeliveryContract: []byte(`[{"role":"frozen","path":"output/frozen.md","mime_type":"text/markdown"}]`)},
		{ID: "wrong-task-type", TaskID: "wrong-task-type-task", Attempt: 1, AgentPackID: "seednote", AgentPackVersion: "1.0.0", AgentPackDigest: "84c58f8e77326bfc818bcd7e36cacab18b52f7e5d8d61e775516bfdabb817933"},
		{ID: "unknown-digest", TaskID: "unknown-task", Attempt: 1, AgentPackID: "seednote", AgentPackVersion: "1.0.0", AgentPackDigest: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
		{ID: "incomplete", TaskID: "incomplete-task", Attempt: 1, AgentPackID: "", AgentPackVersion: "", AgentPackDigest: ""},
	}
	for _, task := range []*model.Task{
		{ID: "known-task", Type: model.PlatformSeednote},
		{ID: "known-preserve-task", Type: model.PlatformSeednote},
		{ID: "wrong-task-type-task", Type: model.PlatformArticle},
		{ID: "unknown-task", Type: model.PlatformSeednote},
		{ID: "incomplete-task", Type: model.PlatformSeednote},
	} {
		if err := db.Create(task).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}

	logger := zerolog.Nop()
	stats, err := MigrateTaskExecutionContracts(context.Background(), db, &logger)
	if err != nil {
		t.Fatalf("MigrateTaskExecutionContracts: %v", err)
	}
	if stats.Backfilled != 2 || stats.Unmatched != 3 {
		t.Fatalf("stats = %#v, want backfilled=2 unmatched=3", stats)
	}
	var known model.TaskExecution
	if err := db.First(&known, "id = ?", "known").Error; err != nil {
		t.Fatal(err)
	}
	var delivery []agentpack.DeliverySpec
	if err := json.Unmarshal(known.AgentPackDeliveryContract, &delivery); err != nil || len(delivery) == 0 {
		t.Fatalf("known delivery contract = %s, err=%v", known.AgentPackDeliveryContract, err)
	}
	var required []agentpack.ArtifactSpec
	if err := json.Unmarshal(known.AgentPackRequiredArtifactContract, &required); err != nil || len(required) != 2 {
		t.Fatalf("known required contract = %s, err=%v", known.AgentPackRequiredArtifactContract, err)
	}
	var preserved model.TaskExecution
	if err := db.First(&preserved, "id = ?", "known-preserve-delivery").Error; err != nil {
		t.Fatal(err)
	}
	if string(preserved.AgentPackDeliveryContract) != `[{"role":"frozen","path":"output/frozen.md","mime_type":"text/markdown"}]` {
		t.Fatalf("existing delivery contract was overwritten: %s", preserved.AgentPackDeliveryContract)
	}
	if err := json.Unmarshal(preserved.AgentPackRequiredArtifactContract, &required); err != nil || len(required) != 2 {
		t.Fatalf("preserved execution required contract = %s, err=%v", preserved.AgentPackRequiredArtifactContract, err)
	}
	for _, id := range []string{"wrong-task-type", "unknown-digest", "incomplete"} {
		var execution model.TaskExecution
		if err := db.First(&execution, "id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		if len(execution.AgentPackDeliveryContract) != 0 || len(execution.AgentPackRequiredArtifactContract) != 0 {
			t.Fatalf("%s received an unverified contract", id)
		}
	}

	second, err := MigrateTaskExecutionContracts(context.Background(), db, &logger)
	if err != nil || second.Backfilled != 0 || second.Unmatched != 3 {
		t.Fatalf("second migration = %#v, %v", second, err)
	}
}

func TestBackfillTaskExecutionContractsAcceptsConcurrentIdenticalWinner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskExecution{}); err != nil {
		t.Fatal(err)
	}
	registry, err := taskExecutionContractMigrationRegistry()
	if err != nil {
		t.Fatal(err)
	}
	var key frozenPackContractKey
	var contracts frozenPackContracts
	for candidateKey, candidateContracts := range registry {
		if candidateKey.ID == "montage" && candidateKey.TaskType == model.PlatformMontage {
			key, contracts = candidateKey, candidateContracts
			break
		}
	}
	if key.ID == "" {
		t.Fatal("current montage contract missing from migration registry")
	}
	delivery, _ := json.Marshal(contracts.Delivery)
	required, _ := json.Marshal(contracts.Required)
	if err := db.Create(&model.Task{ID: "task", Type: model.PlatformMontage}).Error; err != nil {
		t.Fatal(err)
	}
	execution := &model.TaskExecution{
		ID: "execution", TaskID: "task", Attempt: 1,
		AgentPackID: key.ID, AgentPackVersion: key.Version, AgentPackDigest: key.Digest,
		AgentPackDeliveryContract: delivery, AgentPackRequiredArtifactContract: required,
	}
	if err := db.Create(execution).Error; err != nil {
		t.Fatal(err)
	}
	stale := taskExecutionContractCandidate{
		ID: execution.ID, TaskType: model.PlatformMontage,
		AgentPackID: key.ID, AgentPackVersion: key.Version, AgentPackDigest: key.Digest,
	}
	backfilled, err := backfillTaskExecutionContracts(context.Background(), db, stale, key, contracts)
	if err != nil || backfilled {
		t.Fatalf("concurrent identical backfill = %v, %v; want idempotent no-op", backfilled, err)
	}
}

func TestBackfillTaskExecutionContractsRejectsConcurrentConflict(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskExecution{}); err != nil {
		t.Fatal(err)
	}
	registry, err := taskExecutionContractMigrationRegistry()
	if err != nil {
		t.Fatal(err)
	}
	var key frozenPackContractKey
	var contracts frozenPackContracts
	for candidateKey, candidateContracts := range registry {
		if candidateKey.ID == "montage" && candidateKey.TaskType == model.PlatformMontage {
			key, contracts = candidateKey, candidateContracts
			break
		}
	}
	if err := db.Create(&model.Task{ID: "task", Type: model.PlatformMontage}).Error; err != nil {
		t.Fatal(err)
	}
	execution := &model.TaskExecution{
		ID: "execution", TaskID: "task", Attempt: 1,
		AgentPackID: key.ID, AgentPackVersion: key.Version, AgentPackDigest: key.Digest,
		AgentPackDeliveryContract: []byte(`[{"role":"forged"}]`),
	}
	if err := db.Create(execution).Error; err != nil {
		t.Fatal(err)
	}
	stale := taskExecutionContractCandidate{
		ID: execution.ID, TaskType: model.PlatformMontage,
		AgentPackID: key.ID, AgentPackVersion: key.Version, AgentPackDigest: key.Digest,
	}
	if _, err := backfillTaskExecutionContracts(context.Background(), db, stale, key, contracts); err == nil {
		t.Fatal("concurrent conflicting contract was accepted")
	}
}
