package articlecheck

import (
	"net/url"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	xhtml "golang.org/x/net/html"
)

const SchemaVersion = "1.0"

type Request struct {
	Target             string
	Markdown           string
	HTML               string
	Title              string
	Author             string
	Digest             string
	Template           string
	Modules            []ModuleUsage
	ImageSlots         []ImageSlotUsage
	ExpectedImageURLs  []string
	EvidencePaths      []string
	HTMLServerVerified bool
}

type ImageSlotUsage struct {
	ID       string
	URL      string
	Required bool
}

type ModuleUsage struct {
	Name           string
	Known          bool
	ContractKnown  bool
	Required       []string
	Allowed        []string
	Values         map[string]string
	Enums          map[string][]string
	VariantField   string
	DefaultVariant string
	Variants       map[string]VariantUsage
}

type VariantUsage struct {
	Required    []string
	RequiredAny [][]string
}

type Structure struct {
	H1          int `json:"h1"`
	H2          int `json:"h2"`
	Paragraphs  int `json:"paragraphs"`
	Tables      int `json:"tables"`
	Blockquotes int `json:"blockquotes"`
	Images      int `json:"images"`
	Links       int `json:"links"`
	TaskItems   int `json:"task_items"`
}

type Readiness struct {
	Status   string   `json:"status"`
	Targets  []string `json:"targets"`
	Blockers []string `json:"blockers"`
}

type Result struct {
	SchemaVersion string    `json:"schema_version"`
	Target        string    `json:"target"`
	Structure     Structure `json:"structure"`
	Template      string    `json:"template,omitempty"`
	Modules       []string  `json:"modules,omitempty"`
	Missing       []string  `json:"missing"`
	Warnings      []string  `json:"warnings"`
	EvidencePaths []string  `json:"evidence_paths"`
	Readiness     Readiness `json:"readiness"`
}

var unsafeTags = map[string]struct{}{
	"script": {}, "iframe": {}, "form": {}, "input": {}, "style": {}, "link": {},
	"object": {}, "embed": {}, "meta": {}, "base": {}, "frame": {}, "frameset": {},
}

func Inspect(req Request) Result {
	result := Result{
		SchemaVersion: SchemaVersion,
		Target:        strings.TrimSpace(req.Target),
		Template:      strings.TrimSpace(req.Template),
		Missing:       []string{},
		Warnings:      []string{},
		EvidencePaths: uniqueSorted(req.EvidencePaths),
		Readiness:     Readiness{Targets: []string{strings.TrimSpace(req.Target)}, Blockers: []string{}},
	}
	blockers := []string{}
	missing := []string{}
	warnings := []string{}
	target := strings.TrimSpace(req.Target)
	if target != "source" && target != "rendered" && target != "delivery" {
		blockers = append(blockers, "target.invalid")
	}
	if strings.TrimSpace(req.Markdown) == "" {
		missing = append(missing, "markdown")
		blockers = append(blockers, "markdown.required")
	} else {
		result.Structure = inspectMarkdown(req.Markdown)
		blockers = append(blockers, inspectMarkdownURLs(req.Markdown)...)
		if result.Structure.H1 == 0 {
			warnings = append(warnings, "structure.missing_h1")
		}
		if result.Structure.H2 == 0 {
			warnings = append(warnings, "structure.missing_h2")
		}
		if hasMarkdownRawDirective(req.Markdown) {
			blockers = append(blockers, "markdown.raw_directive")
		}
	}
	blockers = append(blockers, inspectImageSlots(req.ImageSlots)...)
	if result.Template == "" {
		warnings = append(warnings, "template.unspecified")
	}
	result.Modules = make([]string, 0, len(req.Modules))
	for _, module := range req.Modules {
		name := strings.TrimSpace(module.Name)
		result.Modules = append(result.Modules, name)
		if name == "" || !module.Known {
			code := "module.unknown"
			if name != "" {
				code += ":" + name
			}
			blockers = append(blockers, code)
			continue
		}
		if !module.ContractKnown {
			blockers = append(blockers, "module.contract_missing:"+name)
			continue
		}
		if len(module.Allowed) > 0 {
			for field := range module.Values {
				if !contains(module.Allowed, field) {
					blockers = append(blockers, "module."+name+"."+field+":unknown_field")
				}
			}
		}
		for field, value := range module.Values {
			if strings.Contains(value, ":::") {
				blockers = append(blockers, "module."+name+"."+field+":raw_directive")
			}
		}
		required := append([]string(nil), module.Required...)
		selectedVariant := ""
		if module.VariantField != "" {
			selectedVariant = strings.TrimSpace(module.Values[module.VariantField])
			if selectedVariant == "" {
				selectedVariant = module.DefaultVariant
			}
			if len(module.Variants) > 0 {
				variant, ok := module.Variants[selectedVariant]
				if !ok {
					blockers = append(blockers, "module."+name+"."+module.VariantField+":invalid_variant")
				} else {
					required = append(required, variant.Required...)
					for _, group := range variant.RequiredAny {
						found := false
						for _, field := range group {
							if strings.TrimSpace(module.Values[field]) != "" {
								found = true
								break
							}
						}
						if !found {
							blockers = append(blockers, "module."+name+"."+module.VariantField+":required_any:"+strings.Join(group, "|"))
						}
					}
				}
			}
		}
		for _, field := range required {
			if strings.TrimSpace(module.Values[field]) == "" {
				code := "module." + name + "." + field
				missing = append(missing, code)
				blockers = append(blockers, code)
			}
		}
		fields := make([]string, 0, len(module.Enums))
		for field := range module.Enums {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		for _, field := range fields {
			value := strings.TrimSpace(module.Values[field])
			if value == "" {
				continue
			}
			if !contains(module.Enums[field], value) {
				blockers = append(blockers, "module."+name+"."+field+":invalid_variant")
			}
		}
	}
	result.Modules = uniqueSorted(result.Modules)

	if target == "rendered" || target == "delivery" {
		if strings.TrimSpace(req.HTML) == "" {
			missing = append(missing, "html")
			blockers = append(blockers, "html.required")
		} else {
			blockers = append(blockers, inspectHTML(req.HTML, req.ExpectedImageURLs)...)
		}
	}
	if target == "delivery" {
		for _, field := range []struct{ name, value string }{
			{"title", req.Title}, {"digest", req.Digest},
		} {
			if strings.TrimSpace(field.value) == "" {
				missing = append(missing, field.name)
				blockers = append(blockers, "delivery."+field.name+"_required")
			}
		}
		if !req.HTMLServerVerified {
			blockers = append(blockers, "html.server_render_required")
		}
	}
	result.Missing = uniqueSorted(missing)
	result.Warnings = uniqueSorted(warnings)
	result.Readiness.Blockers = uniqueSorted(blockers)
	result.Readiness.Status = "ready"
	if len(result.Readiness.Blockers) > 0 {
		result.Readiness.Status = "blocked"
	}
	return result
}

// ValidateHTMLFragment returns stable issue codes for safety and image checks
// shared by rendering inspection and final delivery validation.
func ValidateHTMLFragment(source string, expectedImageURLs []string) []string {
	return inspectHTML(source, expectedImageURLs)
}

func inspectMarkdown(markdown string) Structure {
	var result Structure
	source := []byte(markdown)
	parser := goldmark.New(goldmark.WithExtensions(extension.GFM, extension.TaskList))
	doc := parser.Parser().Parse(text.NewReader(source))
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := node.(type) {
		case *ast.Heading:
			if n.Level == 1 {
				result.H1++
			} else if n.Level == 2 {
				result.H2++
			}
		case *ast.Paragraph:
			result.Paragraphs++
		case *ast.Blockquote:
			result.Blockquotes++
		case *ast.Image:
			result.Images++
		case *ast.Link:
			result.Links++
		case *extast.Table:
			result.Tables++
		case *extast.TaskCheckBox:
			result.TaskItems++
		}
		return ast.WalkContinue, nil
	})
	return result
}

func inspectMarkdownURLs(markdown string) []string {
	var findings []string
	imageCounts := map[string]int{}
	source := []byte(markdown)
	parser := goldmark.New(goldmark.WithExtensions(extension.GFM, extension.TaskList))
	doc := parser.Parser().Parse(text.NewReader(source))
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := node.(type) {
		case *ast.Image:
			imageURL := string(n.Destination)
			imageCounts[imageURL]++
			if unsafeURL(imageURL, true) {
				findings = append(findings, "url.unsafe_image")
			}
		case *ast.Link:
			if unsafeURL(string(n.Destination), false) {
				findings = append(findings, "url.unsafe_link")
			}
		}
		return ast.WalkContinue, nil
	})
	for _, count := range imageCounts {
		if count > 1 {
			findings = append(findings, "image.duplicate")
		}
	}
	return findings
}

func hasMarkdownRawDirective(markdown string) bool {
	source := []byte(markdown)
	parser := goldmark.New(goldmark.WithExtensions(extension.GFM, extension.TaskList))
	doc := parser.Parser().Parse(text.NewReader(source))
	found := false
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || found {
			return ast.WalkContinue, nil
		}
		switch node.(type) {
		case *ast.CodeBlock, *ast.FencedCodeBlock, *ast.CodeSpan:
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			if strings.Contains(string(node.(*ast.Text).Segment.Value(source)), ":::") {
				found = true
				return ast.WalkStop, nil
			}
		}
		return ast.WalkContinue, nil
	})
	return found
}

func inspectImageSlots(slots []ImageSlotUsage) []string {
	findings := []string{}
	seen := map[string]struct{}{}
	for _, slot := range slots {
		imageURL := strings.TrimSpace(slot.URL)
		if slot.Required && imageURL == "" {
			findings = append(findings, "image.required_slot_missing")
			continue
		}
		if imageURL == "" {
			continue
		}
		if unsafeURL(imageURL, true) {
			findings = append(findings, "url.unsafe_slot")
		}
		if _, exists := seen[imageURL]; exists {
			findings = append(findings, "image.duplicate")
		}
		seen[imageURL] = struct{}{}
	}
	return findings
}

func inspectHTML(source string, expectedImageURLs []string) []string {
	findings := []string{}
	tokenizer := xhtml.NewTokenizer(strings.NewReader(source))
	imageCounts := map[string]int{}
	codeDepth := 0
	for {
		tokenType := tokenizer.Next()
		if tokenType == xhtml.ErrorToken {
			break
		}
		if tokenType == xhtml.TextToken {
			if codeDepth == 0 && strings.Contains(string(tokenizer.Text()), ":::") {
				findings = append(findings, "html.raw_directive")
			}
			continue
		}
		if tokenType == xhtml.EndTagToken {
			tag := strings.ToLower(tokenizer.Token().Data)
			if (tag == "code" || tag == "pre") && codeDepth > 0 {
				codeDepth--
			}
			continue
		}
		if tokenType != xhtml.StartTagToken && tokenType != xhtml.SelfClosingTagToken {
			continue
		}
		token := tokenizer.Token()
		tag := strings.ToLower(token.Data)
		if tokenType == xhtml.StartTagToken && (tag == "code" || tag == "pre") {
			codeDepth++
		}
		if _, unsafe := unsafeTags[tag]; unsafe {
			findings = append(findings, "html.unsafe_tag:"+tag)
		}
		for _, attr := range token.Attr {
			name := strings.ToLower(attr.Key)
			if name == "src" && tag == "img" {
				imageCounts[attr.Val]++
				if unsafeURL(attr.Val, true) {
					findings = append(findings, "url.unsafe_html")
				}
			}
			if name == "href" && unsafeURL(attr.Val, false) {
				findings = append(findings, "url.unsafe_html")
			}
			if strings.HasPrefix(name, "on") || name == "srcset" {
				findings = append(findings, "html.unsafe_attribute:"+name)
			}
		}
	}
	for imageURL, count := range imageCounts {
		if count > 1 {
			findings = append(findings, "image.duplicate:"+imageURL)
		}
	}
	for _, expected := range expectedImageURLs {
		if imageCounts[expected] != 1 {
			if imageCounts[expected] == 0 {
				findings = append(findings, "image.missing:"+expected)
			} else {
				findings = append(findings, "image.duplicate:"+expected)
			}
		}
	}
	return uniqueSorted(findings)
}

func unsafeURL(raw string, image bool) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return true
	}
	if image {
		return parsed.Host == "" || (!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https"))
	}
	if parsed.Scheme == "" {
		return strings.HasPrefix(raw, "//") || (!strings.HasPrefix(raw, "#") && strings.Contains(raw, ":"))
	}
	return !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func uniqueSorted(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
