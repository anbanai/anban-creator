package mcp

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/service"
)

var reviewedMCPHandlerCapabilities = map[string]string{
	"accountInfoHandler":                   "svcs.AgentProjectProfileSvc.Get",
	"addTopicHandler":                      "svcs.TopicPoolSvc.Add",
	"agentFeedbackSubmitHandler":           "svcs.AgentFeedbackSvc.Create",
	"analyzeImageHandler":                  "svcs.TaskImageOperationsSvc.Analyze",
	"buildLiveClipManifestHandler":         "svcs.LiveSliceSvc.BuildLiveClipManifest",
	"buildLiveClipPlanHandler":             "svcs.LiveSliceSvc.BuildLiveClipPlan",
	"buildLiveSubjectClipPlanHandler":      "svcs.LiveSliceSvc.BuildLiveSubjectClipPlan",
	"checkSeednoteLoginStatusHandler":      "svcs.SeednoteCapabilitySvc.LoginStatus",
	"claimTopicHandler":                    "svcs.TopicPoolSvc.ClaimTopic",
	"completeLiveSubjectHandler":           "svcs.LiveSliceSvc.CompleteLiveSubject",
	"compressImageHandler":                 "svcs.TaskImageOperationsSvc.Compress",
	"convertMarkdownHandler":               "svcs.WritingSvc.ConvertMarkdown",
	"createLiveAnalysisTaskHandler":        "svcs.LiveSliceSvc.CreateLiveAnalysisTask",
	"downloadImageHandler":                 "svcs.ImageSvc.DownloadImage",
	"exportSeednoteHandler":                "svcs.SeednoteExportSvc.Export",
	"generateImageHandler":                 "svcs.TaskImageSvc.Generate",
	"getMediaPipelineStatusHandler":        "svcs.MediaPipelineSvc.Status",
	"getResourceHandler":                   "svcs.ResourceCatalogSvc.Query",
	"getSeednoteFeedDetailHandler":         "svcs.SeednoteCapabilitySvc.FeedDetail",
	"getSeednoteLoginQRCodeHandler":        "svcs.SeednoteCapabilitySvc.LoginQRCode",
	"getSeednoteUserProfileHandler":        "svcs.SeednoteCapabilitySvc.UserProfile",
	"getTemplateHandler":                   "svcs.TemplateSvc.GetByID",
	"listDraftsHandler":                    "svcs.PublishingSvc.ListDrafts",
	"listPublishedHandler":                 "svcs.PublishingSvc.ListPublished",
	"listResourcesHandler":                 "svcs.ResourceCatalogSvc.Query",
	"listTemplatesHandler":                 "svcs.TemplateSvc.List",
	"listTopicsHandler":                    "svcs.TopicPoolSvc.List",
	"planCreateHandler":                    "svcs.PlanSvc.Create",
	"planListHandler":                      "svcs.PlanSvc.List",
	"prepareFileUploadHandler":             "svcs.FileUploadSvc.Prepare",
	"prepareWorkspaceHandler":              "svcs.WorkspaceSvc.Prepare",
	"progressUpdateHandler":                "svcs.TaskSvc.UpdateProgress",
	"projectGetHandler":                    "svcs.ProjectSvc.Get",
	"projectListHandler":                   "svcs.ProjectSvc.List",
	"publishDraftHandler":                  "svcs.PublishingSvc.PublishDraft",
	"queryLiveAnalysisTaskHandler":         "svcs.LiveSliceSvc.QueryLiveAnalysisTask",
	"recognizeLiveInvalidSentencesHandler": "svcs.LiveSliceSvc.RecognizeLiveInvalidSentences",
	"recognizeLiveSegmentsHandler":         "svcs.LiveSliceSvc.RecognizeLiveSegments",
	"recognizeLiveSubjectsHandler":         "svcs.LiveSliceSvc.RecognizeLiveSubjects",
	"registerRenderedImageHandler":         "svcs.TaskSvc.RegisterRenderedImage",
	"renderTemplateHandler":                "svcs.WritingSvc.RenderTemplate",
	"saveTemplateHandler":                  "svcs.TemplateSvc.SaveGlobal",
	"scoreArticleHandler":                  "svcs.ArticleScoreSvc.Score",
	"searchSeednoteFeedsHandler":           "svcs.SeednoteCapabilitySvc.SearchFeeds",
	"taskCancelHandler":                    "svcs.TaskSvc.CancelForUser",
	"taskFilesHandler":                     "svcs.TaskSvc.GetVisibleFilesForUser",
	"taskGetHandler":                       "svcs.TaskSvc.GetByID",
	"taskListHandler":                      "svcs.TaskSvc.List",
	"titleFinalizeHandler":                 "svcs.TaskSvc.FinalizeTitle",
	"titleListHandler":                     "svcs.TaskSvc.ListTitlesForUser",
	"uploadImageHandler":                   "svcs.TaskImageOperationsSvc.Upload",
	"uploadLiveAudioHandler":               "svcs.LiveSliceSvc.UploadLiveAudio",
}

var reviewedNonToolHandlerExclusions = map[string]string{
	"NewMCPHandler": "HTTP transport factory, not an MCP CallTool handler",
}

func TestEveryMCPHandlerHasReviewedCapabilityBoundary(t *testing.T) {
	files := parseProductionMCPFiles(t)
	handlers := map[string]*ast.FuncDecl{}
	functions := map[string]*ast.FuncDecl{}
	exclusions := map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			functions[localFunctionIndexKey(fn)] = fn
			if fn.Recv != nil {
				continue
			}
			if !strings.HasSuffix(fn.Name.Name, "Handler") {
				continue
			}
			if _, excluded := reviewedNonToolHandlerExclusions[fn.Name.Name]; excluded {
				exclusions[fn.Name.Name] = true
				continue
			}
			handlers[fn.Name.Name] = fn
		}
	}

	for name := range handlers {
		if _, ok := reviewedMCPHandlerCapabilities[name]; !ok {
			t.Errorf("production MCP handler %s is not in the reviewed capability inventory", name)
		}
	}
	for name := range reviewedMCPHandlerCapabilities {
		if _, ok := handlers[name]; !ok {
			t.Errorf("reviewed MCP handler %s no longer exists; update the inventory explicitly", name)
		}
	}
	for name := range reviewedNonToolHandlerExclusions {
		if !exclusions[name] {
			t.Errorf("reviewed non-tool handler exclusion %s no longer exists; update the inventory explicitly", name)
		}
	}
	if t.Failed() {
		return
	}

	names := make([]string, 0, len(handlers))
	for name := range handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		calls := applicationCapabilityCallsForHandler(handlers[name], functions)
		if len(calls) > 1 {
			t.Errorf("%s directly calls %d application capabilities (%s); want at most one", name, len(calls), strings.Join(calls, ", "))
			continue
		}
		want := reviewedMCPHandlerCapabilities[name]
		if len(calls) != 1 || calls[0] != want {
			t.Errorf("%s capability calls = %v, want exactly [%s]", name, calls, want)
		}
	}
}

func TestCapabilityBoundaryDetectsApplicationCallsHiddenBehindMCPHelpers(t *testing.T) {
	tests := map[string]string{
		"plain helper": `package mcp
func hiddenHandler() {
	loadTask()
	recordCost()
}
func loadTask() { svcs.TaskSvc.GetByID(nil, "task") }
func recordCost() { svcs.ProviderCostSvc.RecordProviderTokenUsage(nil, service.RecordProviderTokenCostRequest{}) }
	`,
		"receiver method": `package mcp
type localHelper struct{}
func hiddenHandler() { var helper localHelper; helper.loadTask(); recordCost() }
func (localHelper) loadTask() { svcs.TaskSvc.GetByID(nil, "task") }
func recordCost() { svcs.ProviderCostSvc.RecordProviderTokenUsage(nil, service.RecordProviderTokenCostRequest{}) }
`,
		"function alias": `package mcp
func hiddenHandler() { load := loadTask; load(); recordCost() }
func loadTask() { svcs.TaskSvc.GetByID(nil, "task") }
func recordCost() { svcs.ProviderCostSvc.RecordProviderTokenUsage(nil, service.RecordProviderTokenCostRequest{}) }
`,
		"closure": `package mcp
func hiddenHandler() {
	load := func() { svcs.TaskSvc.GetByID(nil, "task") }
	load()
	recordCost()
}
func recordCost() { svcs.ProviderCostSvc.RecordProviderTokenUsage(nil, service.RecordProviderTokenCostRequest{}) }
`,
		"generic instantiation": `package mcp
func hiddenHandler() { loadTask[int](); recordCost() }
func loadTask[T any]() { svcs.TaskSvc.GetByID(nil, "task") }
func recordCost() { svcs.ProviderCostSvc.RecordProviderTokenUsage(nil, service.RecordProviderTokenCostRequest{}) }
`,
	}
	want := []string{
		"svcs.ProviderCostSvc.RecordProviderTokenUsage",
		"svcs.TaskSvc.GetByID",
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "hidden.go", source, 0)
			if err != nil {
				t.Fatal(err)
			}
			functions := map[string]*ast.FuncDecl{}
			for _, declaration := range file.Decls {
				if fn, ok := declaration.(*ast.FuncDecl); ok {
					functions[localFunctionIndexKey(fn)] = fn
				}
			}

			got := applicationCapabilityCallsForHandler(functions["hiddenHandler"], functions)
			if !slices.Equal(got, want) {
				t.Fatalf("transitive application capability calls = %v, want %v", got, want)
			}
		})
	}
}

func TestCapabilityBoundaryResolvesSameNamedMethodsByReceiverType(t *testing.T) {
	source := `package mcp
type safeHelper struct{}
type capabilityHelper struct{}
func hiddenHandler() { var helper safeHelper; helper.loadTask(); recordCost() }
func (safeHelper) loadTask() {}
func (capabilityHelper) loadTask() { svcs.TaskSvc.GetByID(nil, "task") }
func recordCost() { svcs.ProviderCostSvc.RecordProviderTokenUsage(nil, service.RecordProviderTokenCostRequest{}) }
`
	file, err := parser.ParseFile(token.NewFileSet(), "same_method.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	functions := map[string]*ast.FuncDecl{}
	for _, declaration := range file.Decls {
		if fn, ok := declaration.(*ast.FuncDecl); ok {
			functions[localFunctionIndexKey(fn)] = fn
		}
	}
	got := applicationCapabilityCallsForHandler(functions["hiddenHandler"], functions)
	want := []string{"svcs.ProviderCostSvc.RecordProviderTokenUsage"}
	if !slices.Equal(got, want) {
		t.Fatalf("receiver-specific capability calls = %v, want %v", got, want)
	}
}

func TestCapabilityBoundaryFailsClosedOnChainedLocalReceiver(t *testing.T) {
	source := `package mcp
type localHelper struct{}
func hiddenHandler() { newHelper().loadTask(); recordCost() }
func newHelper() localHelper { return localHelper{} }
func (localHelper) loadTask() { svcs.TaskSvc.GetByID(nil, "task") }
func recordCost() { svcs.ProviderCostSvc.RecordProviderTokenUsage(nil, service.RecordProviderTokenCostRequest{}) }
`
	file, err := parser.ParseFile(token.NewFileSet(), "chained_receiver.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	functions := map[string]*ast.FuncDecl{}
	for _, declaration := range file.Decls {
		if fn, ok := declaration.(*ast.FuncDecl); ok {
			functions[localFunctionIndexKey(fn)] = fn
		}
	}
	got := applicationCapabilityCallsForHandler(functions["hiddenHandler"], functions)
	if !slices.Contains(got, "unsupported-local-call:loadTask") || !slices.Contains(got, "svcs.ProviderCostSvc.RecordProviderTokenUsage") {
		t.Fatalf("chained receiver calls = %v, want explicit fail-closed marker and direct capability", got)
	}
}

func TestMCPToolDescriptionsContainNoWorkflowDirectives(t *testing.T) {
	banned := []string{
		"until completed", "then pass", "call next", "call the next",
		"quality gate", "fallback selection",
		"use this before", "use this when", "must first", "always pass",
		"agents call", "at each step",
	}
	bannedPatterns := []struct {
		name string
		re   *regexp.Regexp
	}{
		{name: "query until", re: regexp.MustCompile(`(?i)\bquery\b[^.]*\buntil\b`)},
		{name: "call next", re: regexp.MustCompile(`(?i)\bcall\b[^.]{0,80}\bnext\b|\bnext\b[^.]{0,80}\bcall\b`)},
		{name: "retry", re: regexp.MustCompile(`(?i)\bretry\b`)},
		{name: "stop or continue", re: regexp.MustCompile(`(?i)\b(stop|continue)\b`)},
	}
	for path, file := range parseProductionMCPFiles(t) {
		ast.Inspect(file, func(node ast.Node) bool {
			kv, ok := node.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := kv.Key.(*ast.Ident)
			value, literal := kv.Value.(*ast.BasicLit)
			if !ok || key.Name != "Description" || !literal || value.Kind != token.STRING {
				return true
			}
			description, err := strconv.Unquote(value.Value)
			if err != nil {
				t.Fatalf("unquote description in %s: %v", path, err)
			}
			lower := strings.ToLower(description)
			for _, phrase := range banned {
				if strings.Contains(lower, phrase) {
					t.Errorf("%s tool description contains workflow directive %q: %q", path, phrase, description)
				}
			}
			for _, pattern := range bannedPatterns {
				if pattern.re.MatchString(description) {
					t.Errorf("%s tool description contains workflow directive %q: %q", path, pattern.name, description)
				}
			}
			return true
		})
	}
}

func parseProductionMCPFiles(t *testing.T) map[string]*ast.File {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]*ast.File)
	fset := token.NewFileSet()
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files[path] = file
	}
	return files
}

func directApplicationCapabilityCalls(fn *ast.FuncDecl) []string {
	calls := make([]string, 0, 2)
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		path := selectorPath(selector)
		if strings.HasPrefix(path, "svcs.") || (strings.HasPrefix(path, "service.") && !isProtocolServiceHelper(path)) {
			calls = append(calls, path)
		}
		return true
	})
	sort.Strings(calls)
	return calls
}

func applicationCapabilityCallsForHandler(fn *ast.FuncDecl, functions map[string]*ast.FuncDecl) []string {
	callSet := make(map[string]struct{}, 2)
	visited := map[*ast.FuncDecl]bool{}
	var visit func(*ast.FuncDecl)
	visit = func(current *ast.FuncDecl) {
		if current == nil || visited[current] {
			return
		}
		visited[current] = true
		for _, capability := range directApplicationCapabilityCalls(current) {
			callSet[capability] = struct{}{}
		}
		aliases := localFunctionAliases(current.Body, functions)
		receiverTypes := localReceiverTypes(current)
		ast.Inspect(current.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			fun := unwrapGenericCall(call.Fun)
			switch target := fun.(type) {
			case *ast.Ident:
				if alias, ok := aliases[target.Name]; ok {
					visit(alias)
				} else {
					visit(functions[target.Name])
				}
			case *ast.SelectorExpr:
				receiver, ok := target.X.(*ast.Ident)
				if !ok {
					if hasLocalMethodNamed(functions, target.Sel.Name) {
						callSet["unsupported-local-call:"+target.Sel.Name] = struct{}{}
					}
					return true
				}
				receiverType := receiverTypes[receiver.Name]
				if receiverType != "" {
					visit(functions[receiverType+"."+target.Sel.Name])
					return true
				}
				if hasLocalMethodNamed(functions, target.Sel.Name) {
					callSet["unsupported-local-call:"+target.Sel.Name] = struct{}{}
				}
			}
			return true
		})
	}
	visit(fn)
	calls := make([]string, 0, len(callSet))
	for capability := range callSet {
		calls = append(calls, capability)
	}
	sort.Strings(calls)
	return calls
}

func localFunctionIndexKey(fn *ast.FuncDecl) string {
	if fn == nil || fn.Recv == nil || len(fn.Recv.List) == 0 {
		if fn == nil {
			return ""
		}
		return fn.Name.Name
	}
	return localReceiverTypeName(fn.Recv.List[0].Type) + "." + fn.Name.Name
}

func localReceiverTypeName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.StarExpr:
		return localReceiverTypeName(value.X)
	case *ast.IndexExpr:
		return localReceiverTypeName(value.X)
	case *ast.IndexListExpr:
		return localReceiverTypeName(value.X)
	default:
		return selectorPath(expr)
	}
}

func localReceiverTypes(fn *ast.FuncDecl) map[string]string {
	types := make(map[string]string)
	addFields := func(fields *ast.FieldList) {
		if fields == nil {
			return
		}
		for _, field := range fields.List {
			typeName := localReceiverTypeName(field.Type)
			for _, name := range field.Names {
				types[name.Name] = typeName
			}
		}
	}
	addFields(fn.Recv)
	addFields(fn.Type.Params)
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch declaration := node.(type) {
		case *ast.ValueSpec:
			typeName := localReceiverTypeName(declaration.Type)
			if typeName == "" && len(declaration.Values) == 1 {
				if literal, ok := declaration.Values[0].(*ast.CompositeLit); ok {
					typeName = localReceiverTypeName(literal.Type)
				}
			}
			for _, name := range declaration.Names {
				if typeName != "" {
					types[name.Name] = typeName
				}
			}
		case *ast.AssignStmt:
			for index, lhs := range declaration.Lhs {
				if index >= len(declaration.Rhs) {
					continue
				}
				name, ok := lhs.(*ast.Ident)
				literal, literalOK := declaration.Rhs[index].(*ast.CompositeLit)
				if ok && literalOK {
					types[name.Name] = localReceiverTypeName(literal.Type)
				}
			}
		}
		return true
	})
	return types
}

func hasLocalMethodNamed(functions map[string]*ast.FuncDecl, method string) bool {
	for _, fn := range functions {
		if fn.Recv != nil && fn.Name.Name == method {
			return true
		}
	}
	return false
}

func unwrapGenericCall(expr ast.Expr) ast.Expr {
	switch value := expr.(type) {
	case *ast.IndexExpr:
		return unwrapGenericCall(value.X)
	case *ast.IndexListExpr:
		return unwrapGenericCall(value.X)
	case *ast.ParenExpr:
		return unwrapGenericCall(value.X)
	default:
		return expr
	}
}

func localFunctionAliases(body *ast.BlockStmt, functions map[string]*ast.FuncDecl) map[string]*ast.FuncDecl {
	aliases := make(map[string]*ast.FuncDecl)
	if body == nil {
		return aliases
	}
	ast.Inspect(body, func(node ast.Node) bool {
		var names []*ast.Ident
		var values []ast.Expr
		switch declaration := node.(type) {
		case *ast.AssignStmt:
			for _, lhs := range declaration.Lhs {
				if name, ok := lhs.(*ast.Ident); ok {
					names = append(names, name)
				}
			}
			values = declaration.Rhs
		case *ast.ValueSpec:
			names, values = declaration.Names, declaration.Values
		default:
			return true
		}
		for index, name := range names {
			if index >= len(values) {
				continue
			}
			if target, ok := unwrapGenericCall(values[index]).(*ast.Ident); ok {
				if fn := functions[target.Name]; fn != nil {
					aliases[name.Name] = fn
				}
			}
		}
		return true
	})
	return aliases
}

func selectorPath(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		prefix := selectorPath(value.X)
		if prefix == "" {
			return value.Sel.Name
		}
		return prefix + "." + value.Sel.Name
	default:
		return ""
	}
}

func isProtocolServiceHelper(path string) bool {
	switch path {
	case "service.ParseLayoutPlan", "service.SanitizeProject":
		return true
	default:
		return false
	}
}

func TestRepositoryInstructionsDefineMCPAsCapabilityTransport(t *testing.T) {
	for _, path := range []string{"../../CLAUDE.md", "../../AGENTS.md"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, want := range []string{
			"MCP is a stateless capability transport",
			"Agents and Skills own business workflow orchestration",
			"one application capability",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q", path, want)
			}
		}
	}
}

func TestGenerateImageSchemaContainsOnlySemanticInputs(t *testing.T) {
	properties := generateImageInputSchema()["properties"].(map[string]any)
	for _, removed := range []string{"operation_id", "verify_with_vision", "verification_prompt", "upload_to_cdn"} {
		if _, ok := properties[removed]; ok {
			t.Fatalf("generate_image exposes removed field %q", removed)
		}
	}
	required := generateImageInputSchema()["required"].([]any)
	for _, value := range required {
		if value == "operation_id" {
			t.Fatal("generate_image still requires operation_id")
		}
	}
}

func TestRegisterRenderedImageSchemaDoesNotUpload(t *testing.T) {
	properties := registerRenderedImageInputSchema()["properties"].(map[string]any)
	if _, ok := properties["upload_to_cdn"]; ok {
		t.Fatal("register_rendered_image still exposes upload_to_cdn")
	}
}

func TestDownloadImageSchemaDoesNotUpload(t *testing.T) {
	properties := downloadImageInputSchema()["properties"].(map[string]any)
	if _, ok := properties["upload"]; ok {
		t.Fatal("download_image still exposes upload")
	}
}

func TestGenerateImagePublicResultContainsOnlyDurableAssetFields(t *testing.T) {
	raw, err := json.Marshal(service.TaskImageAsset{
		Name: "cover.png", Role: "cover", DownloadURL: "/files/cover.png", FilePath: "output/cover.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{
		"provider", "model", "selection_reason", "response_type", "revised_prompt",
		"output_mime", "verification", "wechat_url", "media_id", "billing",
	} {
		if strings.Contains(string(raw), removed) {
			t.Fatalf("generate_image public result exposes %q: %s", removed, raw)
		}
	}
}

func TestGenerateImageFailureDoesNotExposeRouteMetadata(t *testing.T) {
	raw, err := json.Marshal(classifyImageToolFailure(
		context.Background(), context.Background(), context.DeadlineExceeded,
		"generate", time.Minute, false,
	))
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{"provider", "model"} {
		if _, ok := fields[removed]; ok {
			t.Fatalf("generate_image failure exposes %q: %s", removed, raw)
		}
	}
}

func TestExtractedMCPHandlersRequireApplicationCapabilities(t *testing.T) {
	old := svcs
	defer func() { svcs = old }()
	svcs = &Services{}

	tests := []struct {
		name    string
		args    map[string]any
		handler func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error)
		want    string
	}{
		{name: "project profile", args: map[string]any{"project_id": "project"}, handler: accountInfoHandler, want: "agent project profile service not available"},
		{name: "article score", args: map[string]any{"read_count": 100.0, "like_count": 1.0}, handler: scoreArticleHandler, want: "article score service not available"},
		{name: "seednote export", args: map[string]any{"title": "title", "content": "body"}, handler: exportSeednoteHandler, want: "seednote export service not available"},
		{name: "resource catalog", args: map[string]any{"category": "themes"}, handler: listResourcesHandler, want: "resource catalog service not available"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.args)
			if err != nil {
				t.Fatal(err)
			}
			result, err := tt.handler(context.Background(), &mcp.CallToolRequest{
				Params: &mcp.CallToolParamsRaw{Arguments: raw},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || !result.IsError || len(result.Content) == 0 {
				t.Fatalf("result = %#v, want tool error", result)
			}
			text := result.Content[0].(*mcp.TextContent).Text
			if !strings.Contains(text, tt.want) {
				t.Fatalf("error = %q, want %q", text, tt.want)
			}
		})
	}
}
