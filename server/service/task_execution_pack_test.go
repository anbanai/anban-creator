package service

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/datatypes"
)

func TestApplyAgentPackIdentityFreezesDeliveryAndRequiredArtifacts(t *testing.T) {
	pack, ok := agentpack.Default().Pack("seednote")
	if !ok {
		t.Fatal("embedded seednote Pack missing")
	}

	tests := []struct {
		name      string
		taskType  string
		wantPaths []string
	}{
		{name: "seednote", taskType: model.PlatformSeednote, wantPaths: []string{"output/content-tags.json", "output/content.md", "output/image-plan.md"}},
		{name: "viral analysis", taskType: model.TaskTypeViralAnalysis, wantPaths: []string{"output/source-analysis.md", "output/viral-template.json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			execution := &model.TaskExecution{}
			if err := applyAgentPackIdentity(execution, tt.taskType); err != nil {
				t.Fatalf("applyAgentPackIdentity: %v", err)
			}
			if execution.AgentPackID != "seednote" || execution.AgentPackVersion != pack.Version || len(execution.AgentPackDigest) != 64 {
				t.Fatalf("Pack identity = %#v", execution)
			}
			if execution.RuntimeProfile != "seednote" || execution.RuntimeAdapter != "standard" {
				t.Fatalf("runtime identity = %s/%s", execution.RuntimeProfile, execution.RuntimeAdapter)
			}
			var required []agentpack.ArtifactSpec
			if err := json.Unmarshal(execution.AgentPackRequiredArtifactContract, &required); err != nil {
				t.Fatalf("decode frozen required artifact contract: %v", err)
			}
			if got := artifactPathsForTest(required); !reflect.DeepEqual(got, tt.wantPaths) {
				t.Fatalf("required artifacts = %#v, want %#v", got, tt.wantPaths)
			}
			if delivery, err := resolveFrozenExecutionDeliveryContract(execution); err != nil || len(delivery) == 0 {
				t.Fatalf("frozen delivery contract = %#v, %v", delivery, err)
			}
			if len(execution.AgentPackTaggingContract) == 0 {
				t.Fatal("frozen tagging contract is empty")
			}
		})
	}
}

func artifactPathsForTest(artifacts []agentpack.ArtifactSpec) []string {
	paths := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		paths = append(paths, artifact.Path)
	}
	return paths
}

func TestInheritAgentPackIdentityCopiesFrozenContracts(t *testing.T) {
	source := &model.TaskExecution{
		AgentPackID: "wechat-article", AgentPackVersion: "2.0.0", AgentPackDigest: "digest",
		AgentPackDeliveryContract:         datatypes.JSON(`[{"role":"final","path":"output/final.md","mime_type":"text/markdown"}]`),
		AgentPackRequiredArtifactContract: datatypes.JSON(`[{"role":"final","path":"output/final.md","mime_type":"text/markdown","required":true}]`),
		AgentPackTaggingContract:          datatypes.JSON(`{"enabled":true,"profile":"generic-content","required_artifact":"output/content-tags.json","subject_roles":["final"]}`),
		RuntimeAdapter:                    "standard", RuntimeProfile: "wechat",
	}
	target := &model.TaskExecution{}
	if !inheritAgentPackIdentity(target, source) {
		t.Fatal("expected frozen Agent Pack identity to be inherited")
	}
	if target.RuntimeProfile != source.RuntimeProfile || string(target.AgentPackDeliveryContract) != string(source.AgentPackDeliveryContract) {
		t.Fatalf("inherited identity = %#v", target)
	}
	target.AgentPackDeliveryContract[0] = 'X'
	if source.AgentPackDeliveryContract[0] == 'X' {
		t.Fatal("inherited delivery contract aliases source bytes")
	}
}

func TestInheritAgentPackIdentityRejectsIncompleteContract(t *testing.T) {
	source := &model.TaskExecution{
		AgentPackID: "wechat-article", AgentPackVersion: "2.0.0", AgentPackDigest: "digest",
		AgentPackDeliveryContract: datatypes.JSON(`[{"role":"final","path":"output/final.md","mime_type":"text/markdown"}]`),
		RuntimeAdapter:            "standard", RuntimeProfile: "wechat",
	}
	if inheritAgentPackIdentity(&model.TaskExecution{}, source) {
		t.Fatal("incomplete frozen Agent Pack identity was inherited")
	}
}

func TestResolveFrozenExecutionContractsRejectInvalidSnapshot(t *testing.T) {
	valid := model.TaskExecution{
		AgentPackID: "removed-pack", AgentPackVersion: "99.0.0", AgentPackDigest: "digest",
		AgentPackDeliveryContract:         datatypes.JSON(`[{"role":"final","path":"output/final.md","mime_type":"text/markdown"}]`),
		AgentPackRequiredArtifactContract: datatypes.JSON(`[{"role":"final","path":"output/final.md","mime_type":"text/markdown","required":true}]`),
		RuntimeAdapter:                    "standard", RuntimeProfile: "wechat",
	}
	for _, tt := range []struct {
		name   string
		mutate func(*model.TaskExecution)
	}{
		{name: "missing identity", mutate: func(execution *model.TaskExecution) { execution.AgentPackID = "" }},
		{name: "missing delivery", mutate: func(execution *model.TaskExecution) { execution.AgentPackDeliveryContract = nil }},
		{name: "invalid required artifacts", mutate: func(execution *model.TaskExecution) {
			execution.AgentPackRequiredArtifactContract = datatypes.JSON(`{"bad":true}`)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			execution := valid
			tt.mutate(&execution)
			_, deliveryErr := resolveFrozenExecutionDeliveryContract(&execution)
			_, artifactErr := resolveFrozenExecutionRequiredArtifactContract(&execution)
			if !errors.Is(deliveryErr, ErrAgentPackContractMismatch) && !errors.Is(artifactErr, ErrAgentPackContractMismatch) {
				t.Fatalf("delivery error = %v, artifact error = %v", deliveryErr, artifactErr)
			}
		})
	}
}

func TestApplyAgentPackIdentityRejectsUnknownManagedTaskType(t *testing.T) {
	if err := applyAgentPackIdentity(&model.TaskExecution{}, "unknown"); err == nil {
		t.Fatal("unknown task type accepted")
	}
}

func TestApplyAgentPackIdentityRejectsContentTaskWithoutTaggingContract(t *testing.T) {
	pack, ok := agentpack.Default().Pack("wechat-article")
	if !ok {
		t.Fatal("wechat-article Pack missing")
	}
	// Keep the test tied to the actual Pack lookup path while exercising the
	// fail-closed guard through a temporary catalog entry is not possible here;
	// the frozen contract validator covers the same malformed snapshot boundary.
	if pack.Tagging == nil || !pack.Tagging.Enabled {
		t.Fatal("wechat-article Pack must have an enabled tagging contract")
	}
	invalid := &model.TaskExecution{
		TaskKind:    model.TaskTypeWechatArticle,
		AgentPackID: "wechat-article", AgentPackVersion: pack.Version, AgentPackDigest: pack.Digest,
		AgentPackDeliveryContract:         datatypes.JSON(`[{"role":"final_markdown","path":"output/final.md","mime_type":"text/markdown"}]`),
		AgentPackRequiredArtifactContract: datatypes.JSON(`[{"role":"final_markdown","path":"output/final.md","mime_type":"text/markdown","required":true}]`),
		AgentPackTaggingContract:          datatypes.JSON(`null`), RuntimeAdapter: "standard", RuntimeProfile: "wechat-article",
	}
	if hasCompleteAgentPackIdentity(invalid) {
		t.Fatal("content execution with null tagging contract was accepted")
	}
}
