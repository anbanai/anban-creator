package mcp

import (
	"context"
	"encoding/json"
	"fmt"
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
	"accountInfoHandler":              "svcs.AgentProjectProfileSvc.Get",
	"addTopicHandler":                 "svcs.TopicPoolSvc.Add",
	"agentFeedbackSubmitHandler":      "svcs.AgentFeedbackSvc.Create",
	"analyzeImageHandler":             "svcs.TaskImageOperationsSvc.Analyze",
	"analyzeVideoHandler":             "svcs.TaskVideoOperationsSvc.Analyze",
	"buildLiveClipManifestHandler":    "svcs.LiveSliceSvc.BuildLiveClipManifest",
	"buildLiveClipPlanHandler":        "svcs.LiveSliceSvc.BuildLiveClipPlan",
	"buildLiveSubjectClipPlanHandler": "svcs.LiveSliceSvc.BuildLiveSubjectClipPlan",
	"checkSeednoteLoginStatusHandler": "svcs.SeednoteCapabilitySvc.LoginStatus",
	"claimTopicHandler":               "svcs.TopicPoolSvc.ClaimTopic",
	"compressImageHandler":            "svcs.TaskImageOperationsSvc.Compress",
	"convertMarkdownHandler":          "svcs.ContentRenderSvc.ConvertMarkdown",
	"createLiveAnalysisTaskHandler":   "svcs.LiveSliceSvc.CreateLiveAnalysisTask",
	"downloadImageHandler":            "svcs.ImageSvc.DownloadImage",
	"exportSeednoteHandler":           "svcs.SeednoteExportSvc.Export",
	"generateImageHandler":            "svcs.TaskImageSvc.Generate",
	"getMediaPipelineStatusHandler":   "svcs.MediaPipelineSvc.Status",
	"getResourceHandler":              "svcs.ResourceCatalogSvc.Query",
	"getSeednoteFeedDetailHandler":    "svcs.SeednoteCapabilitySvc.FeedDetail",
	"getSeednoteLoginQRCodeHandler":   "svcs.SeednoteCapabilitySvc.LoginQRCode",
	"getSeednoteUserProfileHandler":   "svcs.SeednoteCapabilitySvc.UserProfile",
	"getTemplateHandler":              "svcs.TemplateSvc.GetByID",
	"listDraftsHandler":               "svcs.PublishingSvc.ListDrafts",
	"listPublishedHandler":            "svcs.PublishingSvc.ListPublished",
	"listResourcesHandler":            "svcs.ResourceCatalogSvc.Query",
	"listTemplatesHandler":            "svcs.TemplateSvc.List",
	"listTopicsHandler":               "svcs.TopicPoolSvc.List",
	"planCreateHandler":               "svcs.PlanSvc.Create",
	"planListHandler":                 "svcs.PlanSvc.List",
	"prepareFileUploadHandler":        "svcs.FileUploadSvc.Prepare",
	"progressUpdateHandler":           "svcs.TaskSvc.UpdateProgress",
	"projectGetHandler":               "svcs.ProjectSvc.Get",
	"projectListHandler":              "svcs.ProjectSvc.List",
	"publishDraftHandler":             "svcs.PublishingSvc.PublishDraft",
	"queryLiveAnalysisTaskHandler":    "svcs.LiveSliceSvc.QueryLiveAnalysisTask",
	"renderTemplateHandler":           "svcs.ContentRenderSvc.RenderTemplate",
	"saveTemplateHandler":             "svcs.TemplateSvc.SaveGlobal",
	"scoreArticleHandler":             "svcs.ArticleScoreSvc.Score",
	"searchSeednoteFeedsHandler":      "svcs.SeednoteCapabilitySvc.SearchFeeds",
	"taskCancelHandler":               "svcs.TaskSvc.CancelForUser",
	"taskFilesHandler":                "svcs.TaskSvc.GetVisibleFilesForUser",
	"taskGetHandler":                  "svcs.TaskSvc.GetByID",
	"taskListHandler":                 "svcs.TaskSvc.List",
	"titleFinalizeHandler":            "svcs.TaskSvc.FinalizeTitle",
	"titleListHandler":                "svcs.TaskSvc.ListTitlesForUser",
	"uploadImageHandler":              "svcs.TaskImageOperationsSvc.Upload",
	"uploadLiveAudioHandler":          "svcs.LiveSliceSvc.UploadLiveAudio",
}

func TestEveryMCPHandlerHasReviewedCapabilityBoundary(t *testing.T) {
	files := parseProductionMCPFiles(t)
	functions := map[string]*ast.FuncDecl{}
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			functions[localFunctionIndexKey(fn)] = fn
		}
	}
	handlers := registeredToolHandlers(files, functions)

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

func TestRegisteredToolHandlersIncludeNonconformingNamesAndAnonymousFunctions(t *testing.T) {
	source := `package mcp
func register(server interface{ AddTool(any, any) }) {
	server.AddTool(nil, customTool)
	server.AddTool(nil, func() {
		svcs.TaskSvc.GetByID(nil, "task")
		svcs.ProviderCostSvc.RecordProviderTokenUsage(nil, service.RecordProviderTokenCostRequest{})
	})
}
func customTool() { svcs.TaskSvc.GetByID(nil, "task") }
`
	file, err := parser.ParseFile(token.NewFileSet(), "registered.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	functions := map[string]*ast.FuncDecl{}
	for _, declaration := range file.Decls {
		if fn, ok := declaration.(*ast.FuncDecl); ok {
			functions[localFunctionIndexKey(fn)] = fn
		}
	}
	handlers := registeredToolHandlers(map[string]*ast.File{"registered.go": file}, functions)
	if handlers["customTool"] == nil {
		t.Fatal("registered function without Handler suffix was not inventoried")
	}
	var anonymous *ast.FuncDecl
	for name, handler := range handlers {
		if strings.HasPrefix(name, "anonymous@registered.go:") {
			anonymous = handler
		}
	}
	if anonymous == nil {
		t.Fatal("registered anonymous handler was not inventoried")
	}
	if calls := applicationCapabilityCallsForHandler(anonymous, functions); len(calls) != 2 {
		t.Fatalf("anonymous handler capabilities=%v, want both application calls", calls)
	}
}

func registeredToolHandlers(files map[string]*ast.File, functions map[string]*ast.FuncDecl) map[string]*ast.FuncDecl {
	handlers := map[string]*ast.FuncDecl{}
	for path, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "AddTool" {
				return true
			}
			handlerExpr := unwrapGenericCall(call.Args[1])
			switch handler := handlerExpr.(type) {
			case *ast.Ident:
				handlers[handler.Name] = functions[handler.Name]
			case *ast.FuncLit:
				name := fmt.Sprintf("anonymous@%s:%d", path, handler.Pos())
				handlers[name] = &ast.FuncDecl{Name: ast.NewIdent(name), Type: handler.Type, Body: handler.Body}
			default:
				name := fmt.Sprintf("unsupported@%s:%d", path, handlerExpr.Pos())
				handlers[name] = nil
			}
			return true
		})
	}
	return handlers
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

func TestCapabilityBoundaryDetectsServiceAliases(t *testing.T) {
	source := `package mcp
func aliasHandler() {
	services := svcs
	taskSvc := services.TaskSvc
	costSvc := svcs.ProviderCostSvc
	taskSvc.GetByID(nil, "task")
	costSvc.RecordProviderTokenUsage(nil, service.RecordProviderTokenCostRequest{})
}`
	file, err := parser.ParseFile(token.NewFileSet(), "alias.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	functions := map[string]*ast.FuncDecl{}
	for _, declaration := range file.Decls {
		if fn, ok := declaration.(*ast.FuncDecl); ok {
			functions[localFunctionIndexKey(fn)] = fn
		}
	}
	want := []string{"svcs.ProviderCostSvc.RecordProviderTokenUsage", "svcs.TaskSvc.GetByID"}
	if got := applicationCapabilityCallsForHandler(functions["aliasHandler"], functions); !slices.Equal(got, want) {
		t.Fatalf("service alias capabilities=%v, want %v", got, want)
	}
}

func TestCapabilityBoundaryDetectsServiceMethodValueAliases(t *testing.T) {
	source := `package mcp
func aliasHandler() {
	get := svcs.TaskSvc.GetByID
	record := svcs.ProviderCostSvc.RecordProviderTokenUsage
	get(nil, "task")
	record(nil, service.RecordProviderTokenCostRequest{})
}`
	file, err := parser.ParseFile(token.NewFileSet(), "method_alias.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	functions := map[string]*ast.FuncDecl{}
	for _, declaration := range file.Decls {
		if fn, ok := declaration.(*ast.FuncDecl); ok {
			functions[localFunctionIndexKey(fn)] = fn
		}
	}
	want := []string{"svcs.ProviderCostSvc.RecordProviderTokenUsage", "svcs.TaskSvc.GetByID"}
	if got := applicationCapabilityCallsForHandler(functions["aliasHandler"], functions); !slices.Equal(got, want) {
		t.Fatalf("service method aliases=%v, want %v", got, want)
	}
}

func TestCapabilityBoundaryRespectsLexicallyShadowedServiceAliases(t *testing.T) {
	source := `package mcp
func shadowHandler() {
	taskSvc := svcs.TaskSvc
	{
		taskSvc := safeService{}
		taskSvc.GetByID(nil, "task")
	}
}`
	file, err := parser.ParseFile(token.NewFileSet(), "shadow.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	functions := map[string]*ast.FuncDecl{}
	for _, declaration := range file.Decls {
		if fn, ok := declaration.(*ast.FuncDecl); ok {
			functions[localFunctionIndexKey(fn)] = fn
		}
	}
	if got := applicationCapabilityCallsForHandler(functions["shadowHandler"], functions); len(got) != 0 {
		t.Fatalf("lexically shadowed alias capabilities=%v, want none", got)
	}
}

func TestCapabilityBoundaryRetainsEarlierAliasCallsAfterReassignment(t *testing.T) {
	source := `package mcp
func reassignedHandler() {
	taskSvc := svcs.TaskSvc
	taskSvc.GetByID(nil, "task")
	taskSvc = safeService{}
}`
	file, err := parser.ParseFile(token.NewFileSet(), "reassigned.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	functions := map[string]*ast.FuncDecl{}
	for _, declaration := range file.Decls {
		if fn, ok := declaration.(*ast.FuncDecl); ok {
			functions[localFunctionIndexKey(fn)] = fn
		}
	}
	want := []string{"svcs.TaskSvc.GetByID"}
	if got := applicationCapabilityCallsForHandler(functions["reassignedHandler"], functions); !slices.Equal(got, want) {
		t.Fatalf("capabilities before alias reassignment=%v, want %v", got, want)
	}
}

func TestCapabilityBoundaryRetainsEveryServicePathForReusedAlias(t *testing.T) {
	source := `package mcp
func reusedHandler() {
	svc := svcs.TaskSvc
	svc.GetByID(nil, "task")
	svc = svcs.ProjectSvc
	svc.GetByID(nil, "project")
}`
	file, err := parser.ParseFile(token.NewFileSet(), "reused.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	functions := map[string]*ast.FuncDecl{}
	for _, declaration := range file.Decls {
		if fn, ok := declaration.(*ast.FuncDecl); ok {
			functions[localFunctionIndexKey(fn)] = fn
		}
	}
	want := []string{"svcs.ProjectSvc.GetByID", "svcs.TaskSvc.GetByID"}
	if got := applicationCapabilityCallsForHandler(functions["reusedHandler"], functions); !slices.Equal(got, want) {
		t.Fatalf("capabilities for reused alias=%v, want %v", got, want)
	}
}

func TestToolDescriptionValueFailsClosedOnNonLiteral(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "description.go", `package mcp
var generatedDescription = "state only"
var tool = mcp.Tool{Description: generatedDescription}
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		kv, ok := node.(*ast.KeyValueExpr)
		if !ok || descriptionKeyName(kv.Key) != "description" {
			return true
		}
		found = true
		if _, err := literalToolDescription(kv.Value); err == nil {
			t.Error("non-literal tool description was not rejected")
		}
		return true
	})
	if !found {
		t.Fatal("description field was not found")
	}
}

func TestToolDescriptionScannerScopesBusinessMapsAndRejectsSchemaMutation(t *testing.T) {
	mutationSource := `package mcp
func mutationSchema() map[string]any {
	result := map[string]any{"type": "object"}
	result["description"] = generatedDescription
	return result
}
func register(server interface{ AddTool(any, any) }) {
	server.AddTool(&mcp.Tool{Description: "state only", InputSchema: mutationSchema()}, handler)
}
func handler() {
	_ = map[string]any{"description": dynamicBusinessValue}
}`
	fragmentSource := `package mcp
func fragmentSchema(fragment map[string]any) map[string]any { return fragment }
func register(server interface{ AddTool(any, any) }) {
	server.AddTool(&mcp.Tool{Description: "state only", InputSchema: fragmentSchema(dynamicFragment)}, handler)
}`
	compositeSource := `package mcp
func compositeSchema() map[string]any {
	return map[string]any{"type": "object", "properties": dynamicFragment}
}
func register(server interface{ AddTool(any, any) }) {
	server.AddTool(&mcp.Tool{Description: "state only", InputSchema: compositeSchema()}, handler)
}`
	returnSource := `package mcp
func returnSchema() map[string]any { return dynamicFragment }
func register(server interface{ AddTool(any, any) }) {
	server.AddTool(&mcp.Tool{Description: "state only", InputSchema: returnSchema()}, handler)
}`
	sliceSource := `package mcp
func sliceSchema() map[string]any {
	return map[string]any{"allOf": []any{dynamicFragment}}
}
func register(server interface{ AddTool(any, any) }) {
	server.AddTool(&mcp.Tool{Description: "state only", InputSchema: sliceSchema()}, handler)
}`
	reassignSource := `package mcp
func reassignSchema() map[string]any {
	result := map[string]any{"type": "object"}
	result = dynamicFragment
	return result
}
func register(server interface{ AddTool(any, any) }) {
	server.AddTool(&mcp.Tool{Description: "state only", InputSchema: reassignSchema()}, handler)
}`
	unknownCallSource := `package mcp
func unknownCallSchema() map[string]any {
	props := map[string]any{"type": "object"}
	maps.Copy(props, dynamicFragment)
	return props
}
func register(server interface{ AddTool(any, any) }) {
	server.AddTool(&mcp.Tool{Description: "state only", InputSchema: unknownCallSchema()}, handler)
}`
	dynamicKeySource := `package mcp
const descriptionKey = "description"
func dynamicKeySchema() map[string]any {
	return map[string]any{descriptionKey: "retry until completed"}
}
func register(server interface{ AddTool(any, any) }) {
	server.AddTool(&mcp.Tool{Description: "state only", InputSchema: dynamicKeySchema()}, handler)
}`
	mutationFile, err := parser.ParseFile(token.NewFileSet(), "description_scope.go", mutationSource, 0)
	if err != nil {
		t.Fatal(err)
	}
	fragmentFile, err := parser.ParseFile(token.NewFileSet(), "description_fragment.go", fragmentSource, 0)
	if err != nil {
		t.Fatal(err)
	}
	compositeFile, err := parser.ParseFile(token.NewFileSet(), "description_composite.go", compositeSource, 0)
	if err != nil {
		t.Fatal(err)
	}
	returnFile, err := parser.ParseFile(token.NewFileSet(), "description_return.go", returnSource, 0)
	if err != nil {
		t.Fatal(err)
	}
	sliceFile, err := parser.ParseFile(token.NewFileSet(), "description_slice.go", sliceSource, 0)
	if err != nil {
		t.Fatal(err)
	}
	reassignFile, err := parser.ParseFile(token.NewFileSet(), "description_reassign.go", reassignSource, 0)
	if err != nil {
		t.Fatal(err)
	}
	unknownCallFile, err := parser.ParseFile(token.NewFileSet(), "description_unknown_call.go", unknownCallSource, 0)
	if err != nil {
		t.Fatal(err)
	}
	dynamicKeyFile, err := parser.ParseFile(token.NewFileSet(), "description_dynamic_key.go", dynamicKeySource, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, reviewErrors := collectMCPToolDescriptions(map[string]*ast.File{
		"description_scope.go":        mutationFile,
		"description_fragment.go":     fragmentFile,
		"description_composite.go":    compositeFile,
		"description_return.go":       returnFile,
		"description_slice.go":        sliceFile,
		"description_reassign.go":     reassignFile,
		"description_unknown_call.go": unknownCallFile,
		"description_dynamic_key.go":  dynamicKeyFile,
	})
	if len(reviewErrors) != 8 {
		t.Fatalf("description review errors=%v, want eight dynamic schema errors", reviewErrors)
	}
}

type mcpDescriptionCollector struct {
	functions    map[string]*ast.FuncDecl
	descriptions []string
	errors       []error
	seenValues   map[token.Pos]bool
	seenErrors   map[token.Pos]bool
	visitedFuncs map[*ast.FuncDecl]bool
	boundParams  map[*ast.Object]bool
	reviewedKeys map[*ast.Object]bool
}

func collectMCPToolDescriptions(files map[string]*ast.File) ([]string, []error) {
	collector := &mcpDescriptionCollector{
		functions:    make(map[string]*ast.FuncDecl),
		seenValues:   make(map[token.Pos]bool),
		seenErrors:   make(map[token.Pos]bool),
		visitedFuncs: make(map[*ast.FuncDecl]bool),
		boundParams:  make(map[*ast.Object]bool),
		reviewedKeys: make(map[*ast.Object]bool),
	}
	for _, file := range files {
		for _, declaration := range file.Decls {
			if fn, ok := declaration.(*ast.FuncDecl); ok {
				collector.functions[localFunctionIndexKey(fn)] = fn
			}
		}
	}
	for path, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok || selectorPath(literal.Type) != "mcp.Tool" {
				return true
			}
			for _, element := range literal.Elts {
				kv, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok {
					continue
				}
				switch key.Name {
				case "Description":
					collector.addDescription(path, kv.Value)
				case "InputSchema":
					collector.scanSchemaExpr(path, kv.Value, true)
				}
			}
			return true
		})
	}
	return collector.descriptions, collector.errors
}

func (c *mcpDescriptionCollector) addDescription(path string, expr ast.Expr) {
	if expr == nil || c.seenValues[expr.Pos()] {
		return
	}
	c.seenValues[expr.Pos()] = true
	description, err := literalToolDescription(expr)
	if err != nil {
		c.addSchemaError(path, expr, err.Error())
		return
	}
	c.descriptions = append(c.descriptions, description)
}

func (c *mcpDescriptionCollector) scanSchemaExpr(path string, expr ast.Expr, failUnsupported bool) {
	switch value := unwrapGenericCall(expr).(type) {
	case *ast.CompositeLit:
		c.scanSchemaComposite(path, value)
	case *ast.CallExpr:
		name, ok := unwrapGenericCall(value.Fun).(*ast.Ident)
		var fn *ast.FuncDecl
		if ok {
			fn = c.functions[name.Name]
		}
		if ok && fn != nil {
			c.bindFunctionArguments(fn, value.Args)
		}
		for _, arg := range value.Args {
			c.scanSchemaExpr(path, arg, true)
		}
		if !ok || fn == nil {
			if failUnsupported {
				c.addSchemaError(path, value, "input schema call cannot be statically reviewed")
			}
			return
		}
		c.scanSchemaFunction(path, fn)
	case *ast.Ident:
		if value.Name == "nil" || value.Name == "true" || value.Name == "false" || c.boundParams[value.Obj] {
			return
		}
		if initializer := localIdentifierInitializer(value); initializer != nil {
			c.scanSchemaExpr(path, initializer, true)
			return
		}
		if failUnsupported {
			c.addSchemaError(path, value, fmt.Sprintf("input schema identifier %s cannot be statically reviewed", value.Name))
		}
	case *ast.BasicLit:
		return
	case *ast.TypeAssertExpr:
		c.scanSchemaExpr(path, value.X, failUnsupported)
	case *ast.UnaryExpr:
		c.scanSchemaExpr(path, value.X, failUnsupported)
	default:
		if failUnsupported {
			c.addSchemaError(path, value, "input schema expression cannot be statically reviewed")
		}
	}
}

func (c *mcpDescriptionCollector) addSchemaError(path string, node ast.Node, message string) {
	if node != nil && c.seenErrors[node.Pos()] {
		return
	}
	if node != nil {
		c.seenErrors[node.Pos()] = true
	}
	c.errors = append(c.errors, fmt.Errorf("%s: %s", path, message))
}

func (c *mcpDescriptionCollector) scanSchemaComposite(path string, literal *ast.CompositeLit) {
	for _, element := range literal.Elts {
		kv, ok := element.(*ast.KeyValueExpr)
		if !ok {
			if expr, ok := element.(ast.Expr); ok {
				c.scanSchemaExpr(path, expr, true)
			}
			continue
		}
		if _, literalKey := kv.Key.(*ast.BasicLit); !literalKey {
			c.addSchemaError(path, kv.Key, "input schema key cannot be statically reviewed")
		}
		if isSchemaDescriptionKey(kv.Key) {
			if _, propertyDefinition := unwrapGenericCall(kv.Value).(*ast.CompositeLit); !propertyDefinition {
				c.addDescription(path, kv.Value)
			}
		}
		c.scanSchemaExpr(path, kv.Value, true)
	}
}

func (c *mcpDescriptionCollector) scanSchemaFunction(path string, fn *ast.FuncDecl) {
	if fn == nil || c.visitedFuncs[fn] {
		return
	}
	c.visitedFuncs[fn] = true
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.KeyValueExpr:
			if isSchemaDescriptionKey(value.Key) {
				if _, propertyDefinition := unwrapGenericCall(value.Value).(*ast.CompositeLit); !propertyDefinition {
					c.addDescription(path, value.Value)
				}
			}
			c.scanSchemaExpr(path, value.Value, true)
		case *ast.AssignStmt:
			for index, lhs := range value.Lhs {
				if index >= len(value.Rhs) {
					continue
				}
				indexed, ok := lhs.(*ast.IndexExpr)
				if ok {
					if isSchemaDescriptionKey(indexed.Index) {
						c.addDescription(path, value.Rhs[index])
					} else if _, literalKey := indexed.Index.(*ast.BasicLit); !literalKey && !c.isReviewedDynamicKey(indexed.Index) {
						c.addSchemaError(path, indexed.Index, "input schema key cannot be statically reviewed")
					}
				}
				c.scanSchemaExpr(path, value.Rhs[index], true)
			}
		case *ast.RangeStmt:
			c.scanSchemaExpr(path, value.X, true)
			if c.isReviewedRangeSource(value.X) {
				for _, expr := range []ast.Expr{value.Key, value.Value} {
					if ident, ok := expr.(*ast.Ident); ok && ident.Obj != nil {
						c.boundParams[ident.Obj] = true
					}
				}
				if ident, ok := value.Key.(*ast.Ident); ok && ident.Obj != nil {
					c.reviewedKeys[ident.Obj] = true
				}
			}
		case *ast.CallExpr:
			name, ok := unwrapGenericCall(value.Fun).(*ast.Ident)
			if ok && c.functions[name.Name] != nil {
				c.bindFunctionArguments(c.functions[name.Name], value.Args)
				for _, arg := range value.Args {
					c.scanSchemaExpr(path, arg, true)
				}
				c.scanSchemaFunction(path, c.functions[name.Name])
			} else {
				c.addSchemaError(path, value, "input schema call cannot be statically reviewed")
			}
		case *ast.ReturnStmt:
			for _, result := range value.Results {
				c.scanSchemaExpr(path, result, true)
			}
		}
		return true
	})
}

func (c *mcpDescriptionCollector) isReviewedDynamicKey(expr ast.Expr) bool {
	ident, ok := unwrapGenericCall(expr).(*ast.Ident)
	return ok && ident.Obj != nil && c.reviewedKeys[ident.Obj]
}

func (c *mcpDescriptionCollector) isReviewedRangeSource(expr ast.Expr) bool {
	ident, ok := unwrapGenericCall(expr).(*ast.Ident)
	if !ok || ident.Obj == nil {
		return false
	}
	if c.boundParams[ident.Obj] {
		return true
	}
	initializer := localIdentifierInitializer(ident)
	if initializer == nil || initializer == expr {
		return false
	}
	return c.isReviewedRangeSource(initializer)
}

func (c *mcpDescriptionCollector) bindFunctionArguments(fn *ast.FuncDecl, args []ast.Expr) {
	if fn == nil || fn.Type.Params == nil {
		return
	}
	index := 0
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			if index >= len(args) {
				return
			}
			if name.Obj != nil {
				c.boundParams[name.Obj] = true
			}
			index++
		}
	}
}

func localIdentifierInitializer(identifier *ast.Ident) ast.Expr {
	if identifier == nil || identifier.Obj == nil {
		return nil
	}
	switch declaration := identifier.Obj.Decl.(type) {
	case *ast.AssignStmt:
		for index, lhs := range declaration.Lhs {
			name, ok := lhs.(*ast.Ident)
			if ok && name.Obj == identifier.Obj && index < len(declaration.Rhs) {
				return declaration.Rhs[index]
			}
		}
	case *ast.ValueSpec:
		for index, name := range declaration.Names {
			if name.Obj == identifier.Obj && index < len(declaration.Values) {
				return declaration.Values[index]
			}
		}
	}
	return nil
}

func isSchemaDescriptionKey(expr ast.Expr) bool {
	_, literal := expr.(*ast.BasicLit)
	return literal && descriptionKeyName(expr) == "description"
}

func literalToolDescription(expr ast.Expr) (string, error) {
	value, ok := expr.(*ast.BasicLit)
	if !ok || value.Kind != token.STRING {
		return "", fmt.Errorf("description must be a string literal")
	}
	description, err := strconv.Unquote(value.Value)
	if err != nil {
		return "", fmt.Errorf("unquote description: %w", err)
	}
	return description, nil
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
		"agents call", "at each step", "new agents should",
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
	descriptions, reviewErrors := collectMCPToolDescriptions(parseProductionMCPFiles(t))
	for _, err := range reviewErrors {
		t.Errorf("tool description cannot be statically reviewed: %v", err)
	}
	for _, description := range descriptions {
		lower := strings.ToLower(description)
		for _, phrase := range banned {
			if strings.Contains(lower, phrase) {
				t.Errorf("tool description contains workflow directive %q: %q", phrase, description)
			}
		}
		for _, pattern := range bannedPatterns {
			if pattern.re.MatchString(description) {
				t.Errorf("tool description contains workflow directive %q: %q", pattern.name, description)
			}
		}
	}
}

func descriptionKeyName(expr ast.Expr) string {
	switch key := expr.(type) {
	case *ast.Ident:
		return strings.ToLower(key.Name)
	case *ast.BasicLit:
		if key.Kind != token.STRING {
			return ""
		}
		value, err := strconv.Unquote(key.Value)
		if err != nil {
			return ""
		}
		return strings.ToLower(value)
	default:
		return ""
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
	serviceAliases := localServiceAliases(fn.Body)
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		for _, path := range serviceAliases.normalizeAll(selector) {
			if strings.HasPrefix(path, "svcs.") || (strings.HasPrefix(path, "service.") && !isProtocolServiceHelper(path)) {
				calls = append(calls, path)
			}
		}
		return true
	})
	sort.Strings(calls)
	return calls
}

type serviceAliasPaths struct {
	bodyStart, bodyEnd token.Pos
	byObject           map[*ast.Object]map[string]struct{}
}

func localServiceAliases(body *ast.BlockStmt) *serviceAliasPaths {
	aliases := &serviceAliasPaths{byObject: make(map[*ast.Object]map[string]struct{})}
	if body == nil {
		return aliases
	}
	aliases.bodyStart, aliases.bodyEnd = body.Pos(), body.End()
	ast.Inspect(body, func(node ast.Node) bool {
		var names []ast.Expr
		var values []ast.Expr
		switch declaration := node.(type) {
		case *ast.AssignStmt:
			names, values = declaration.Lhs, declaration.Rhs
		case *ast.ValueSpec:
			values = declaration.Values
			for _, name := range declaration.Names {
				names = append(names, name)
			}
		default:
			return true
		}
		for index, lhs := range names {
			if index >= len(values) {
				continue
			}
			name, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}
			for _, sourcePath := range aliases.normalizeAll(unwrapGenericCall(values[index])) {
				if sourcePath == "svcs" || strings.HasPrefix(sourcePath, "svcs.") {
					aliases.add(name.Obj, sourcePath)
				}
			}
		}
		return true
	})
	return aliases
}

func (a *serviceAliasPaths) add(object *ast.Object, path string) {
	if object == nil || path == "" {
		return
	}
	if a.byObject[object] == nil {
		a.byObject[object] = make(map[string]struct{})
	}
	a.byObject[object][path] = struct{}{}
}

func (a *serviceAliasPaths) normalizeAll(expr ast.Expr) []string {
	path := selectorPath(expr)
	root := selectorRootIdent(expr)
	if root == nil {
		if path == "" {
			return nil
		}
		return []string{path}
	}
	normalized := make([]string, 0, 1)
	if root.Obj != nil {
		for alias := range a.byObject[root.Obj] {
			normalized = append(normalized, alias)
		}
	}
	if len(normalized) == 0 && root.Name == "svcs" && a.isPackageServiceRoot(root) {
		normalized = append(normalized, "svcs")
	}
	if len(normalized) == 0 {
		if path == "" {
			return nil
		}
		return []string{path}
	}
	_, suffix, _ := strings.Cut(path, ".")
	for index := range normalized {
		if suffix != "" {
			normalized[index] += "." + suffix
		}
	}
	sort.Strings(normalized)
	return normalized
}

func (a *serviceAliasPaths) isPackageServiceRoot(root *ast.Ident) bool {
	if root.Obj == nil || root.Obj.Decl == nil {
		return true
	}
	declaration, ok := root.Obj.Decl.(ast.Node)
	return !ok || declaration.Pos() < a.bodyStart || declaration.End() > a.bodyEnd
}

func (a *serviceAliasPaths) capabilitiesForIdent(ident *ast.Ident) []string {
	if ident == nil || ident.Obj == nil {
		return nil
	}
	capabilities := make([]string, 0, len(a.byObject[ident.Obj]))
	for capability := range a.byObject[ident.Obj] {
		if strings.Count(capability, ".") >= 2 {
			capabilities = append(capabilities, capability)
		}
	}
	sort.Strings(capabilities)
	return capabilities
}

func selectorRootIdent(expr ast.Expr) *ast.Ident {
	switch value := unwrapGenericCall(expr).(type) {
	case *ast.Ident:
		return value
	case *ast.SelectorExpr:
		return selectorRootIdent(value.X)
	default:
		return nil
	}
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
		serviceAliases := localServiceAliases(current.Body)
		receiverTypes := localReceiverTypes(current)
		ast.Inspect(current.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			fun := unwrapGenericCall(call.Fun)
			switch target := fun.(type) {
			case *ast.Ident:
				if capabilities := serviceAliases.capabilitiesForIdent(target); len(capabilities) > 0 {
					for _, capability := range capabilities {
						callSet[capability] = struct{}{}
					}
					return true
				}
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

func TestGenerateImageImplementationDoesNotContainRemovedCompositeWorkflow(t *testing.T) {
	for _, path := range []string{"image_tools.go", "../service/image.go"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, removed := range []string{"verify_with_vision", "verification_prompt", "upload_to_cdn", "operation_id"} {
			if strings.Contains(string(data), removed) {
				t.Fatalf("%s retains removed generate_image composite workflow term %q", path, removed)
			}
		}
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
		TaskFileID: "file-1", FilePath: "output/cover.png", DownloadURL: "/files/cover.png",
		MimeType: "image/png", FileSize: 123, ContentHash: strings.Repeat("a", 64),
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
