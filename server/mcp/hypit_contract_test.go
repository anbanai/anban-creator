package mcp

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/anbanai/anban-creator/server/agentpack"
)

func TestHypitManagedDeliveryContract(t *testing.T) {
	catalog, err := agentpack.LoadCatalog(filepath.Join("..", "..", "harness"))
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]*agentpack.Catalog{"source": catalog, "embedded": agentpack.Default()} {
		t.Run(name, func(t *testing.T) {
			pack, ok := c.ForTaskType("hypit")
			if !ok {
				t.Fatal("video replication has no managed task route")
			}
			if pack.Kind != agentpack.KindManaged || pack.Channel != "hypit" || pack.Runtime.Profile != "hypit" || pack.Runtime.Adapter != agentpack.AdapterStandard {
				t.Fatalf("wrong execution route: %#v", pack)
			}
			if !pack.SupportsTaskKind("hypit") || !slices.Contains(pack.Surfaces, "task") || pack.SupportsPlan() {
				t.Fatalf("wrong Hypit surfaces or task contract: %#v", pack)
			}
			resolved, ok := c.ForTaskType("hypit")
			if !ok || resolved.ID != pack.ID {
				t.Fatal("task type must resolve to the same Pack")
			}
			if operation, ok := c.BillingOperation("hypit"); !ok || operation != "task.hypit" {
				t.Fatalf("billing operation %q", operation)
			}
			required := map[string]string{"output/final.mp4": "video/mp4", "output/cover.png": "image/png", "output/project.json": "application/json", "output/project.zip": "application/zip", "output/delivery-manifest.json": "application/json", "output/quality-report.json": "application/json"}
			for path, mime := range required {
				found := false
				for _, artifact := range pack.Artifacts {
					if artifact.Path == path && artifact.Required && artifact.MIMEType == mime {
						found = true
					}
				}
				if !found {
					t.Errorf("required delivery %s (%s) missing", path, mime)
				}
				found = false
				for _, delivery := range pack.Delivery {
					if delivery.Path == path {
						found = true
					}
				}
				if !found {
					t.Errorf("user cannot download %s", path)
				}
			}
		})
	}
}

func TestVideoPacksAreTaskOnlyWhenTheirInputsCannotBeScheduled(t *testing.T) {
	for name, c := range map[string]*agentpack.Catalog{"source": mustLoadCatalog(t), "embedded": agentpack.Default()} {
		t.Run(name, func(t *testing.T) {
			for _, tt := range []struct {
				packID   string
				channel  string
				taskKind string
			}{
				{packID: "montage", channel: "montage", taskKind: "montage"},
				{packID: "whiteboard-animation", channel: "whiteboard-animation", taskKind: "whiteboard-animation"},
			} {
				pack, ok := c.Pack(tt.packID)
				if !ok {
					t.Fatalf("%s Pack missing", tt.packID)
				}
				if pack.Kind != agentpack.KindManaged || pack.Channel != tt.channel || !pack.SupportsTaskKind(tt.taskKind) || !slices.Contains(pack.Surfaces, "task") || pack.SupportsPlan() {
					t.Fatalf("%s has invalid task-only contract: %#v", tt.packID, pack)
				}
			}
		})
	}
}

func mustLoadCatalog(t *testing.T) *agentpack.Catalog {
	t.Helper()
	catalog, err := agentpack.LoadCatalog(filepath.Join("..", "..", "harness"))
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}
