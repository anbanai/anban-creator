package mcp

import (
 "path/filepath"
 "testing"
 "github.com/anbanai/anban-creator/server/agentpack"
)

func TestHypitManagedPackDeliveryContract(t *testing.T) {
 catalog, err := agentpack.LoadCatalog(filepath.Join("..", "..", "harness"))
 if err != nil { t.Fatal(err) }
 for name, c := range map[string]*agentpack.Catalog{"source": catalog, "embedded": agentpack.Default()} {
  t.Run(name, func(t *testing.T) {
   pack, ok := c.ForTaskType("hypit")
   if !ok { t.Fatal("video replication has no managed task route") }
   if pack.Kind != agentpack.KindManaged || pack.Runtime.Profile != "hypit" || pack.Runtime.Adapter != agentpack.AdapterStandard { t.Fatalf("wrong execution route: %#v", pack) }
   project, ok := c.ForProjectPlatform("hypit")
   if !ok || project.ID != pack.ID { t.Fatal("project and task must resolve to the same Pack") }
   if operation, ok := c.BillingOperation("hypit"); !ok || operation != "task.hypit" { t.Fatalf("billing operation %q", operation) }
   required := map[string]string{"output/final.mp4":"video/mp4", "output/cover.png":"image/png", "output/project.json":"application/json", "output/project.zip":"application/zip", "output/delivery-manifest.json":"application/json", "output/quality-report.json":"application/json"}
   for path, mime := range required {
    found := false
    for _, artifact := range pack.Artifacts { if artifact.Path == path && artifact.Required && artifact.MIMEType == mime { found = true } }
    if !found { t.Errorf("required delivery %s (%s) missing", path, mime) }
    found = false
    for _, delivery := range pack.Delivery { if delivery.Path == path { found = true } }
    if !found { t.Errorf("user cannot download %s", path) }
   }
  })
 }
}
