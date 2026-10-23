package articlecheck

import (
	"strings"
	"testing"
)

func TestInspectSourceCountsStructureAndValidatesModuleContract(t *testing.T) {
	result := Inspect(Request{
		Target:   "source",
		Markdown: "# 标题\n\n引言段落。\n\n## 步骤\n\n- [x] 完成\n\n| 输入 | 输出 |\n| --- | --- |\n| A | B |\n\n> 引用\n\n![图](https://cdn.example.com/a.png)\n\n[链接](https://example.com)",
		Template: "tutorial",
		Modules: []ModuleUsage{{
			Name: "steps", Known: true, ContractKnown: true,
			Required: []string{"title", "items"},
			Values:   map[string]string{"title": "操作步骤"},
		}},
	})
	if result.SchemaVersion != "1.0" || result.Structure.H1 != 1 || result.Structure.H2 != 1 ||
		result.Structure.Tables != 1 || result.Structure.Blockquotes != 1 ||
		result.Structure.Images != 1 || result.Structure.Links != 1 || result.Structure.TaskItems == 0 {
		t.Fatalf("unexpected inspection result: %#v", result)
	}
	if !containsFinding(result.Missing, "module.steps.items") || !containsFinding(result.Readiness.Blockers, "module.steps.items") {
		t.Fatalf("missing required module field was not blocking: %#v", result)
	}
}

func TestInspectSourceBlocksUnknownModuleInvalidVariantAndUnsafeURL(t *testing.T) {
	result := Inspect(Request{
		Target:   "source",
		Markdown: "# 标题\n\n## 内容\n\n![bad](javascript:alert(1))",
		Modules: []ModuleUsage{
			{Name: "unknown", Known: false},
			{Name: "callout", Known: true, ContractKnown: true, Values: map[string]string{"tone": "loud"}, Enums: map[string][]string{"tone": {"info", "warning"}}},
		},
	})
	for _, code := range []string{"module.unknown", "module.callout.tone", "url.unsafe_image"} {
		if !containsFinding(result.Readiness.Blockers, code) {
			t.Errorf("readiness blockers %v missing %q", result.Readiness.Blockers, code)
		}
	}
}

func TestInspectRequiresAbsoluteHTTPImageURLs(t *testing.T) {
	tests := []struct {
		name   string
		target string
		body   string
	}{
		{name: "markdown relative path", target: "source", body: "![图](/images/article.png)"},
		{name: "markdown protocol-relative URL", target: "source", body: "![图](//cdn.example.com/article.png)"},
		{name: "html relative path", target: "rendered", body: `<img src="/images/article.png">`},
		{name: "html missing host", target: "rendered", body: `<img src="https:article.png">`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := Inspect(Request{Target: tc.target, Markdown: "# Title\n\n## Body\n\n" + tc.body, HTML: tc.body})
			if result.Readiness.Status != "blocked" {
				t.Fatalf("image URL was accepted: %#v", result.Readiness)
			}
			if !containsFinding(result.Readiness.Blockers, "url.unsafe_image") &&
				!containsFinding(result.Readiness.Blockers, "url.unsafe_html") {
				t.Fatalf("unsafe image URL finding is missing: %v", result.Readiness.Blockers)
			}
		})
	}
}

func TestInspectAllowsRawDirectiveLiteralsInsideCode(t *testing.T) {
	markdown := "# Title\n\n## Example\n\nUse `:::callout` as a literal.\n\n```text\n:::callout\n```\n"
	if result := Inspect(Request{Target: "source", Markdown: markdown}); result.Readiness.Status != "ready" {
		t.Fatalf("code examples blocked by source inspection: %#v", result.Readiness)
	}

	html := `<section><p>Use <code>:::callout</code> as a literal.</p><pre><code>:::callout</code></pre></section>`
	if result := Inspect(Request{Target: "rendered", Markdown: markdown, HTML: html}); result.Readiness.Status != "ready" {
		t.Fatalf("code examples blocked by HTML inspection: %#v", result.Readiness)
	}
}

func TestInspectSourceBlocksDuplicateInlineImageURL(t *testing.T) {
	result := Inspect(Request{
		Target:   "source",
		Markdown: "# Title\n\n## Body\n\n![one](https://cdn.example.com/repeated.png)\n\n![two](https://cdn.example.com/repeated.png)",
	})
	if !containsFinding(result.Readiness.Blockers, "image.duplicate") {
		t.Fatalf("duplicate inline image URL was not blocked: %v", result.Readiness.Blockers)
	}
}

func TestInspectSourceBlocksRawDirectiveInsideModuleValue(t *testing.T) {
	result := Inspect(Request{
		Target: "source", Markdown: "# 标题\n\n## 内容\n\n正文。",
		Modules: []ModuleUsage{{
			Name: "callout", Known: true, ContractKnown: true,
			Allowed: []string{"body"}, Values: map[string]string{"body": ":::script\\nalert(1)"},
		}},
	})
	if !containsFinding(result.Readiness.Blockers, "module.callout.body:raw_directive") {
		t.Fatalf("module raw directive was not blocked: %v", result.Readiness.Blockers)
	}
}

func TestInspectSourceAppliesVariantSpecificRequiredFields(t *testing.T) {
	result := Inspect(Request{
		Target: "source", Markdown: "# 标题\n\n## 内容\n\n正文。",
		Modules: []ModuleUsage{{
			Name: "summary", Known: true, ContractKnown: true,
			VariantField: "variant", DefaultVariant: "one-line",
			Values: map[string]string{"variant": "three", "highlight": "核心判断"},
			Variants: map[string]VariantUsage{
				"one-line": {Required: []string{"highlight"}},
				"three":    {Required: []string{"items"}},
			},
		}},
	})
	if !containsFinding(result.Missing, "module.summary.items") || !containsFinding(result.Readiness.Blockers, "module.summary.items") {
		t.Fatalf("variant-specific field was not required: %#v", result)
	}
}

func TestInspectSourceBlocksNativeModuleWithoutContract(t *testing.T) {
	result := Inspect(Request{
		Target: "source", Markdown: "# 标题\n\n## 内容\n\n正文。",
		Modules: []ModuleUsage{{Name: "legacy-only", Known: true, ContractKnown: false}},
	})
	if !containsFinding(result.Readiness.Blockers, "module.contract_missing:legacy-only") {
		t.Fatalf("missing native contract was not blocked: %v", result.Readiness.Blockers)
	}
}

func TestInspectRenderedHTMLRejectsUnsafeTagsDirectivesAndDuplicateImages(t *testing.T) {
	result := Inspect(Request{
		Target:            "rendered",
		Markdown:          "# 标题\n\n正文。",
		HTML:              `<section><p>正文</p><script>alert(1)</script><p>:::callout</p><img src="https://cdn.example.com/a.png"><img src="https://cdn.example.com/a.png"></section>`,
		ExpectedImageURLs: []string{"https://cdn.example.com/a.png"},
	})
	for _, code := range []string{"html.unsafe_tag", "html.raw_directive", "image.duplicate"} {
		if !containsFinding(result.Readiness.Blockers, code) {
			t.Errorf("readiness blockers %v missing %q", result.Readiness.Blockers, code)
		}
	}
}

func TestInspectDeliveryRequiresMetadataAndServerRenderedHTML(t *testing.T) {
	result := Inspect(Request{Target: "delivery", Markdown: "# Title\n\nBody", HTML: `<section><p>Body</p></section>`})
	if !containsFinding(result.Missing, "title") || !containsFinding(result.Missing, "digest") {
		t.Fatalf("delivery metadata missing list = %v", result.Missing)
	}
	if !containsFinding(result.Readiness.Blockers, "html.server_render_required") {
		t.Fatalf("delivery must require verified server HTML: %v", result.Readiness.Blockers)
	}
}

func TestInspectRenderedHTMLRejectsIllegalURL(t *testing.T) {
	result := Inspect(Request{
		Target: "rendered", Markdown: "# Title\n\nBody",
		HTML: `<section><a href="javascript:alert(1)">bad</a><p>Body</p></section>`,
	})
	if !containsFinding(result.Readiness.Blockers, "url.unsafe_html") {
		t.Fatalf("unsafe HTML URL not blocked: %v", result.Readiness.Blockers)
	}
}

func containsFinding(values []string, want string) bool {
	for _, value := range values {
		if value == want || strings.HasPrefix(value, want+":") {
			return true
		}
	}
	return false
}
