package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/resources"
)

func TestRenderTemplateLegacyHTMLGolden(t *testing.T) {
	markdownWithoutImage, err := os.ReadFile(filepath.Join("testdata", "article-quality-no-image.md"))
	if err != nil {
		t.Fatal(err)
	}
	markdownWithImage, err := os.ReadFile(filepath.Join("testdata", "article-quality-with-image.md"))
	if err != nil {
		t.Fatal(err)
	}
	svc, repo := setupConvertTest(t)
	userID := "user-legacy-golden"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "autumn-warm")
	articleTypes := []string{"long-form-essay", "listicle", "tutorial", "story-narrative"}
	fixtures := make(map[string]string)

	for _, articleType := range articleTypes {
		t.Run("template_"+articleType, func(t *testing.T) {
			for _, fixture := range []struct {
				name     string
				markdown string
				imageURL string
			}{
				{name: "no_image", markdown: string(markdownWithoutImage)},
				{name: "slot_image", markdown: string(markdownWithoutImage), imageURL: "https://cdn.example.com/slot.png"},
				{name: "inline_and_slot_same_url", markdown: string(markdownWithImage), imageURL: "https://cdn.example.com/article-focus.png"},
			} {
				plan := &LayoutPlan{
					ArticleType: articleType, TemplateName: articleType,
					Slots: []LayoutPlanSlot{{SlotID: "hero", SectionIndex: 0, ImageURL: fixture.imageURL, ImageSize: "full-width"}},
				}
				result, err := svc.RenderTemplate(t.Context(), userID, projectID, fixture.markdown, plan, "", "")
				if err != nil {
					t.Fatalf("render %s: %v", fixture.name, err)
				}
				key := "template." + articleType + "." + fixture.name
				fixtures[key] = normalizedHTMLHash(result.HTML)
			}
		})
	}

	for _, entry := range resources.Manager().List(resources.CategoryLayout) {
		t.Run("module_"+entry.Name, func(t *testing.T) {
			module := entry.Name
			vars := fixtureModuleVars(entry)
			plan := &LayoutPlan{
				ArticleType: "long-form-essay",
				Slots: []LayoutPlanSlot{{
					SlotID: "footer", SectionIndex: -1,
					Module: &module, ModuleVars: vars,
				}},
			}
			result, err := svc.RenderTemplate(t.Context(), userID, projectID, string(markdownWithoutImage), plan, "", "")
			if err != nil {
				t.Fatalf("render %s: %v", entry.Name, err)
			}
			fixtures["module."+entry.Name] = normalizedHTMLHash(result.HTML)
		})
	}

	goldenPath := filepath.Join("testdata", "article-rendering-legacy.sha256.json")
	if os.Getenv("UPDATE_ARTICLE_GOLDENS") == "1" {
		keys := make([]string, 0, len(fixtures))
		for key := range fixtures {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		ordered := make(map[string]string, len(fixtures))
		for _, key := range keys {
			ordered[key] = fixtures[key]
		}
		data, err := json.MarshalIndent(ordered, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]string
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatalf("decode HTML goldens: %v", err)
	}
	if len(fixtures) != len(golden) {
		t.Fatalf("fixture count = %d, golden count = %d; update or review the baseline", len(fixtures), len(golden))
	}
	for key, got := range fixtures {
		if want := golden[key]; want == "" || got != want {
			t.Errorf("normalized HTML golden %s = %s, want %s", key, got, want)
		}
	}
}

func TestRenderTemplateV1ModuleHTMLGolden(t *testing.T) {
	markdown := "# 标题\n\n## 正文\n\n文章主体内容。"
	svc, repo := setupConvertTest(t)
	svc.articleResourceMode = resources.LifecycleModeCompatibility
	userID := "user-contract-golden"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "autumn-warm")
	modules := []struct {
		name string
		vars map[string]string
	}{
		{name: "quote", vars: map[string]string{"text": "把结论放在读者最需要的位置。", "label": "核心观点", "source": "编辑手册"}},
		{name: "callout", vars: map[string]string{"type": "warning", "title": "注意", "body": "检查链接\n核对图片"}},
		{name: "steps", vars: map[string]string{"title": "执行步骤", "steps": "01 | 先定目标 | 说明要解决的问题\n02 | 再验结果 | 对照目标检查变化"}},
		{name: "checklist", vars: map[string]string{"title": "发布前检查", "items": "done | 标题 | 对齐正文\npending | 链接 | 确认可用\nwarn | 图片 | 检查清晰度"}},
		{name: "summary", vars: map[string]string{"variant": "three", "title": "读完带走三点", "items": "结构先于风格 | 模块服务阅读 | 结论具体"}},
		{name: "comparison-table", vars: map[string]string{"title": "方案对比", "left_title": "方案 A", "left_items": "部署快 | 成本低", "right_title": "方案 B", "right_items": "扩展强 | 运维较多"}},
	}
	fixtures := make(map[string]string, len(modules))
	for _, moduleCase := range modules {
		module := moduleCase.name
		plan := &LayoutPlan{
			ArticleType: "tutorial", ContractVersion: "anban.layout.v1",
			Slots: []LayoutPlanSlot{{SlotID: "footer", SectionIndex: -1, Module: &module, ModuleVars: moduleCase.vars}},
		}
		result, err := svc.RenderTemplate(t.Context(), userID, projectID, markdown, plan, "", "")
		if err != nil {
			t.Fatalf("render %s: %v", module, err)
		}
		fixtures["module."+module] = normalizedHTMLHash(result.HTML)
	}
	writeOrCompareHTMLGolden(t, filepath.Join("testdata", "article-rendering-contract-v1.sha256.json"), fixtures, "UPDATE_ARTICLE_CONTRACT_GOLDENS")
}

func writeOrCompareHTMLGolden(t *testing.T, goldenPath string, fixtures map[string]string, updateEnv string) {
	t.Helper()
	if os.Getenv(updateEnv) == "1" {
		keys := make([]string, 0, len(fixtures))
		for key := range fixtures {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		ordered := make(map[string]string, len(fixtures))
		for _, key := range keys {
			ordered[key] = fixtures[key]
		}
		data, err := json.MarshalIndent(ordered, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]string
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatalf("decode HTML goldens: %v", err)
	}
	if len(fixtures) != len(golden) {
		t.Fatalf("fixture count = %d, golden count = %d; update or review the baseline", len(fixtures), len(golden))
	}
	for key, got := range fixtures {
		if want := golden[key]; want == "" || got != want {
			t.Errorf("normalized HTML golden %s = %s, want %s", key, got, want)
		}
	}
}

func fixtureModuleVars(entry resources.ResourceEntry) map[string]string {
	values := make(map[string]string)
	if entry.Fields == nil {
		return values
	}
	for _, field := range entry.Fields.Required {
		value := strings.TrimSpace(field.Example)
		if value == "" {
			value = "示例" + field.Name
		}
		values[field.Name] = value
	}
	return values
}

func normalizedHTMLHash(source string) string {
	tokenizer := xhtml.NewTokenizer(strings.NewReader(source))
	var normalized strings.Builder
	for {
		tokenType := tokenizer.Next()
		if tokenType == xhtml.ErrorToken {
			break
		}
		token := tokenizer.Token()
		switch tokenType {
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			attributes := append([]xhtml.Attribute(nil), token.Attr...)
			sort.Slice(attributes, func(i, j int) bool {
				if attributes[i].Namespace == attributes[j].Namespace {
					return attributes[i].Key < attributes[j].Key
				}
				return attributes[i].Namespace < attributes[j].Namespace
			})
			fmt.Fprintf(&normalized, "<%s", strings.ToLower(token.Data))
			for _, attr := range attributes {
				fmt.Fprintf(&normalized, " %s=%q", strings.ToLower(attr.Key), strings.Join(strings.Fields(attr.Val), " "))
			}
			if tokenType == xhtml.SelfClosingTagToken {
				normalized.WriteString("/")
			}
			normalized.WriteString(">")
		case xhtml.EndTagToken:
			fmt.Fprintf(&normalized, "</%s>", strings.ToLower(token.Data))
		case xhtml.TextToken:
			text := strings.Join(strings.Fields(string(tokenizer.Text())), " ")
			if text != "" {
				normalized.WriteString(text)
			}
		}
	}
	digest := sha256.Sum256([]byte(normalized.String()))
	return hex.EncodeToString(digest[:])
}
