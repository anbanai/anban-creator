# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Writer for WeChat** (wechatwriter) is a Go CLI tool that transforms Markdown articles into WeChat-formatted HTML with professional styling, AI-powered writing assistance, humanization features, and direct publishing to WeChat draft box.

- **Language**: Go 1.26.0
- **CLI Framework**: Cobra
- **Logging**: Zap (structured logging)
- **WeChat SDK**: silenceper/wechat/v2
- **Architecture**: Modular CLI with separate concerns for conversion, image processing, draft management, and writing assistance

## Build & Test Commands

### Building

```bash
# Quick build for current platform (development)
make fast

# Build for current platform (outputs to bin/wechatwriter)
make build

# Build for all platforms (release)
make release

# Build via go directly
go build -o wechatwriter ./app
```

### Testing

```bash
# Run all tests
make test
# or
go test -v ./...

# Run specific package tests
go test -v ./app/config
go test -v ./app/image

# Run tests with coverage
go test -cover ./...

# Run specific test
go test -v -run TestConfig_Validate ./app/config
```

### Code Quality

```bash
# Format code
make fmt

# Static analysis
make vet

# Lint (requires golangci-lint)
make lint

# Download/update dependencies
make deps
```

### Installation

```bash
# Install to GOPATH/bin
make install
# or
go install ./app
```

## Architecture

### Core Module Structure

The project follows a layered architecture with clear separation of concerns:

```
app/
├── main.go                 # CLI entry point, command routing
├── {command}.go            # Individual command implementations (convert, write, humanize, etc.)
├── errors.go               # Hinter interface, AppError type
├── doctor.go               # Diagnostic checks (config, env, network)
│
├── ai/                    # AI text generation client
│   └── client.go          # OpenAI-compatible chat completions (default model: claude-sonnet-4-6)
│
├── config/                 # Configuration management
│   └── config.go          # Single-account config (JSON only)
│
├── converter/             # Markdown → WeChat HTML conversion
│   ├── converter.go       # Core conversion interface & orchestration
│   ├── ai.go             # AI mode implementation (Claude-based)
│   ├── image.go          # Image reference extraction & placeholder handling
│   ├── prompt.go         # AI prompt building with theme support
│   └── theme.go          # Theme management system
│
├── writer/               # Styled writing assistance
│   ├── assistant.go      # Writing style orchestration
│   ├── generator.go      # Content generation
│   ├── cover_generator.go # Cover image prompt generation
│   ├── style.go          # Style definition loading (YAML-based)
│   └── types.go          # Data structures
│
├── humanizer/            # AI writing trace removal
│   ├── humanizer.go      # Detection & removal of AI patterns
│   ├── prompt.go         # Humanization prompts
│   └── result.go         # Quality scoring (5 dimensions)
│
├── image/                # Image processing & generation
│   ├── processor.go      # Unified image handling (upload, compress, generate)
│   ├── compress.go       # Image compression (max 1920px, preserves aspect ratio)
│   ├── provider.go       # Provider interface
│   ├── openai.go         # OpenAI DALL-E provider
│   ├── gemini.go         # Google Gemini provider (google.golang.org/genai)
│   ├── openrouter.go     # OpenRouter multi-model gateway provider
│   └── volcengine.go     # Volcengine Seedream provider (async polling)
│
├── draft/                # WeChat draft management
│   └── service.go        # Draft creation & publishing
│
└── wechat/               # WeChat API wrapper
    ├── service.go        # Material upload, access token management
    └── errors.go         # WechatAPIError with retryable detection
```

### Configuration System

**Single Account Support**: The config system supports one WeChat account configured via the config file (`.wechatwriter/settings.json`).

**Config Search Priority**: CWD → `CLAUDE_PLUGIN_ROOT` → `~/.config/wechatwriter/` → `~/.wechatwriter/` → executable-relative

**Two Loading Modes**:
- `Load()` / `LoadWithDefaults()`: Full validation including WeChat AppID/Secret
- `LoadMinimal()`: Skips WeChat validation, used by commands that don't need WeChat API (write, doctor)

### Conversion Flow

The converter module orchestrates a multi-step process:

1. **Image Extraction**: Parse Markdown for image references (local/online/AI-generated)
2. **Markdown → HTML**: Generate WeChat-compatible HTML with theme styling
   - **AI Mode**: Uses Claude with theme-specific prompts (autumn-warm, spring-fresh, ocean-calm, custom)
   - All CSS must be inlined (no external stylesheets)
   - Safe HTML tags only (no script, iframe, form elements)
3. **Image Placeholders**: Replace image references with `<!-- IMG:0 -->` format
4. **Image Processing** (if enabled):
   - Local: compress → upload to WeChat CDN
   - Online: download → compress → upload
   - AI: generate → download → compress → upload
5. **Placeholder Replacement**: Replace placeholders with WeChat CDN URLs
6. **Draft Publishing** (optional): Create draft in WeChat backend

### Image Generation Providers

**OpenAI**:

- Synchronous API
- Models: dall-e-2, dall-e-3
- Provider value: `openai` (or empty, default)
- Requires paid API key

**Google Gemini** (provider: `gemini` or `google`):

- Uses official `google.golang.org/genai` Go SDK
- Returns inline image data (no URL download needed)
- Default model: `gemini-3-pro-image-preview`
- Supports image size via `size` field in WIDTHxHEIGHT format (e.g., `2560x1440`, `1728x2304`)
- Temp file prefix: `wechatwriter_gemini_`

**OpenRouter** (provider: `openrouter` or `or`):

- Multi-model gateway supporting Gemini, Flux, and others
- Uses Chat Completions API, returns base64-encoded images
- Default model: `google/gemini-3-pro-image-preview`
- Default base URL: `https://openrouter.ai/api/v1`
- Temp file prefix: `wechatwriter_openrouter_`

**Volcengine/Seedream** (provider: `volcengine`, `volc`, or `seedream`):

- Bytedance's image generation model with async task polling
- Default model: `doubao-seedream-4-5-251128`
- Default base URL: `https://ark.cn-beijing.volces.com/api/v3`
- Temp file prefix: `wechatwriter_volcengine_`

### Writing Styles

Located in `writers/*.yaml`, each style defines:

- **Core Traits**: Distinctive voice characteristics
- **Structure Patterns**: Preferred content organization
- **Language Usage**: Vocabulary, sentence rhythm, formatting preferences
- **Domain Knowledge**: Specialized knowledge to incorporate

Built-in styles:

- `dan-koe`: Concise, punchy, philosophical depth with practical insights
- `cultural-depth`: Rich cultural references, literary style
- `casual-science`: Accessible explanations with engaging examples

### AI Humanization

Detects and removes 5 categories of AI patterns:

1. **Content Patterns**: Over-emphasis, vague attribution, promotional language
2. **Language Patterns**: AI vocabulary, negative parallelism, three-part structures
3. **Style Patterns**: Excessive dashes, bold overuse, emoji patterns
4. **Filler Patterns**: Filler phrases, over-qualification, generic conclusions
5. **Collaboration Traces**: Dialogue-style fillers, knowledge cutoff disclaimers

Intensity levels: `gentle`, `medium`, `aggressive`

Quality scoring (5 dimensions, 10 points each):

- Directness: Gets to the point quickly
- Rhythm: Varied sentence length
- Trust: Respects reader intelligence
- Authenticity: Sounds human-written
- Precision: No redundant content

## Development Patterns

### Adding New Commands

1. Create `app/{command}.go` with cobra command definition
2. Add command to `rootCmd` in `main.go`
3. Use `initConfig()` for lazy config loading (allows --help without config)
4. Return JSON responses via `responseSuccess()` or `responseError()`

### Adding New Image Providers

1. Implement `Provider` interface in `app/image/{provider}.go`:

   ```go
   type Provider interface {
       Name() string
       Generate(ctx context.Context, prompt string) (*GenerateResult, error)
   }
   ```

2. Register in `app/image/provider.go` factory
3. Add tests following `modelscope_test.go` pattern (use httptest for mocking)

### Adding New Themes

1. Create YAML file in `themes/{name}.yaml`
2. Theme system auto-loads from YAML with hot-reload support
3. Theme structure includes: core_traits, structure_patterns, language_usage, domain_knowledge

### Writing Tests

Follow the established patterns in `app/config/config_test.go`:

- Use table-driven tests for multiple scenarios
- Create temp files/dirs with `t.TempDir()`
- Mock HTTP calls with `httptest.NewServer`
- Test both success and error paths
- Use custom error types for better error handling

Example test structure:

```go
func TestFeature(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {"valid case", "input", "expected", false},
        {"error case", "bad", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

### Error Handling

- `Hinter` interface (`app/errors.go`): cross-cutting pattern for user-friendly error hints, implemented by `AppError`, `ConfigError`, `GenerateError`, `WechatAPIError`
- `WechatAPIError` (`app/wechat/errors.go`): parses WeChat error codes into messages with hints; `IsRetryable()` identifies transient errors
- Log errors with zap structured logging: `log.Error("msg", zap.Error(err), zap.String("field", value))`
- Return errors via JSON response in CLI commands using `responseError()`
- Mask sensitive values in logs (see `maskMediaID()` pattern)

### WeChat API Integration

**Access Token Management**:

- Automatically cached and refreshed by wechat SDK
- No manual token management needed

**Material Upload**:

- Images must be < 10MB (enforced by compression)
- Returns media_id and CDN URL
- Retry logic handles transient failures

**Draft Creation**:

- Content size limit: < 20,000 characters or 1MB
- API mode generates larger HTML due to inline CSS (use sparingly for long articles)
- HTML must use safe tags only

## Important Constraints

1. **WeChat HTML Requirements**:
   - All CSS must be inlined (style attributes)
   - No external resources (fonts, scripts, stylesheets)
   - Safe tags only: section, p, span, strong, em, h1-h6, ul, ol, li, blockquote, pre, code, table, img, br, hr
   - No: script, iframe, form, input, style, link

2. **Image Processing**:
   - Max width: 1920px (configurable)
   - Max size: 10MB for WeChat upload
   - Compression preserves aspect ratio
   - Supported formats: JPG, JPEG, PNG, GIF, BMP, WebP

3. **Configuration**:
   - Config is JSON only (`.wechatwriter/settings.json`)

4. **AI Generation**:
   - Prompts must be in Chinese for better results with Chinese content
   - Theme prompts define complete styling system (colors, typography, spacing)
   - Image generation prompts should be descriptive but concise

## CLI Commands Overview

- `./bin/wechatwriter account init` - Create config file with guided setup
- `./bin/wechatwriter account history` - View unified history of drafts and published articles
- `./bin/wechatwriter convert <file>` - Convert Markdown to WeChat HTML
- `./bin/wechatwriter write` - Style-based writing assistance
- `./bin/wechatwriter humanize <file>` - Remove AI writing traces
- `./bin/wechatwriter score <file>` - Evaluate article quality (viral potential scoring)
- `./bin/wechatwriter outline` - Generate article outline
- `./bin/wechatwriter draft` - Manage WeChat drafts
- `./bin/wechatwriter draft article <json_file>` - Create 图文文章 (news article) draft from JSON
- `./bin/wechatwriter draft post` - Create 小绿书 (Xiaolvshu/newspic) image posts (max 20 images)
- `./bin/wechatwriter image generate <prompt>` - Generate AI images
- `./bin/wechatwriter image upload <file>` - Upload image to WeChat CDN
- `./bin/wechatwriter image download <url>` - Download image
- `./bin/wechatwriter doctor` - Diagnose config and connection issues

## Skills Integration

The project includes Claude Code skills in `skills/` directory:

- `content-writing` - Article writing workflow
- `visual-design` - Image and theme management
- `topic-research` - Research and scoring
- `seo-optimization` - SEO best practices
- `article-publishing` - Article draft publishing workflows
- `post-publishing` - Image post (小绿书) publishing workflows
- `content-analysis` - Content quality analysis

Skills are auto-loaded when working in this repository or via plugin marketplace.

## Notes for Development

- The codebase uses Go 1.26 features - ensure compatibility when adding new code
- Log structured data with zap, not fmt.Printf
- JSON responses should use `printJSON()` helper for consistent formatting
- Two Cobra command patterns coexist: package-level var with `init()` (older) and factory functions returning `*cobra.Command` (preferred for new commands)
- Image processing is performance-sensitive - consider adding concurrency for batch operations
- WeChat API calls have rate limits - implement backoff/retry where needed

## Plugin & Agent Ecosystem

```
.claude-plugin/
├── plugin.json          # Plugin manifest v2.3.0
└── marketplace.json     # Marketplace listing

hooks/hooks.json         # SessionStart (env setup), SubagentStop, TaskCompleted

agents/
├── wechatarticle.md    # Full article pipeline agent (maxTurns: 50)
└── wechatpost.md      # Image post pipeline agent (maxTurns: 25)

output-styles/
└── wechat-creator.md    # WeChat creator output style
```
