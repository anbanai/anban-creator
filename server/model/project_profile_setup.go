package model

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

const (
	MaxProjectAgentIDs           = 7
	MaxProfileAccountLinks       = 10
	MaxProfileAccountURLBytes    = 500
	MaxProfileAccountNameBytes   = 200
	MaxProfileFollowerRangeBytes = 100
	MaxProfileAnswerBytes        = 5000
	MaxProfileSamples            = 20
	MaxProfileSampleBytes        = 20000
	ProfileSourceUserConfirmed   = "[用户确认]"
	ProfileSourceNeedsSupplement = "[待补充]"
)

var projectAgentIDs = map[string]struct{}{
	AgentIDArticle: {}, AgentIDWechatPicture: {}, AgentIDSeednote: {},
	AgentIDMontage: {}, AgentIDHypit: {}, AgentIDWhiteboard: {}, AgentIDLocalLife: {},
}

var profileAccountPlatforms = map[string]struct{}{
	PlatformWechat: {}, PlatformSeednote: {}, "douyin": {}, "bilibili": {},
	"weibo": {}, "zhihu": {}, "kuaishou": {}, "channels": {},
}

// ProjectAllowsAgent preserves legacy derived capabilities for rows without an
// explicit selection. Auxiliary Packs such as profile-analysis are independent
// of the six selectable creative objects.
func ProjectAllowsAgent(project *Project, agentID string) bool {
	if project == nil || project.AgentIDs == nil {
		return true
	}
	if _, selectable := projectAgentIDs[agentID]; !selectable {
		return true
	}
	for _, selected := range project.AgentIDs {
		if selected == agentID {
			return true
		}
	}
	return false
}

// ValidateProjectAgentIDs accepts any subset of the six project-facing Agent
// Packs, including an explicitly empty selection.
func ValidateProjectAgentIDs(ids []string) error {
	if len(ids) > MaxProjectAgentIDs {
		return fmt.Errorf("too many project agent_ids")
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := projectAgentIDs[id]; !ok {
			return fmt.Errorf("unsupported project agent_id %q", id)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("duplicate project agent_id %q", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

// DefaultProjectAgentIDs supplies the old single-platform default for new
// callers that omit agent_ids. Platforms without one of the six project Agent
// Packs retain nil so handlers can use the legacy task/plan projection.
func DefaultProjectAgentIDs(platform string) []string {
	switch strings.TrimSpace(platform) {
	case PlatformWechat:
		return []string{AgentIDArticle}
	case PlatformSeednote:
		return []string{AgentIDSeednote}
	case PlatformMontage:
		return []string{AgentIDMontage}
	case PlatformHypit:
		return []string{AgentIDHypit}
	case PlatformWhiteboardAnimation:
		return []string{AgentIDWhiteboard}
	case PlatformLocalLife:
		return []string{AgentIDLocalLife}
	default:
		return nil
	}
}

type ProjectProfileAccount struct {
	Platform      string `json:"platform"`
	AccountName   string `json:"account_name,omitempty"`
	FollowerRange string `json:"follower_range,omitempty"`
	ProfileURL    string `json:"profile_url"`
	Source        string `json:"source"`
}

type ProjectProfileIntentAnswers struct {
	Goals           string `json:"goals"`
	Direction       string `json:"direction"`
	Differentiation string `json:"differentiation"`
	Strengths       string `json:"strengths,omitempty"`
	Resources       string `json:"resources,omitempty"`
}

type ProjectProfileContentAnswers struct {
	Preferences       string `json:"preferences"`
	Formats           string `json:"formats"`
	Tone              string `json:"tone"`
	Audience          string `json:"audience"`
	Favorites         string `json:"favorites,omitempty"`
	ReferenceAccounts string `json:"reference_accounts,omitempty"`
}

type ProjectProfileBoundaryAnswers struct {
	Exclusions    string `json:"exclusions"`
	Collaboration string `json:"collaboration"`
	Compliance    string `json:"compliance"`
}

type ProjectProfileAnswers struct {
	Intent     ProjectProfileIntentAnswers   `json:"intent"`
	Content    ProjectProfileContentAnswers  `json:"content"`
	Boundaries ProjectProfileBoundaryAnswers `json:"boundaries"`
}

// ProjectProfileSetup is project-scoped source material for profile analysis.
// Account sources are user-supplied and never shared between projects.
type ProjectProfileSetup struct {
	AccountStatus    string                  `json:"account_status"`
	PlatformAccounts []ProjectProfileAccount `json:"platform_accounts"`
	Answers          ProjectProfileAnswers   `json:"answers"`
	Samples          []string                `json:"samples"`
}

func NewProjectProfileSetup() ProjectProfileSetup {
	return ProjectProfileSetup{
		AccountStatus:    "new",
		PlatformAccounts: []ProjectProfileAccount{},
		Samples:          []string{},
	}
}

// Normalize fills stable defaults, trims user input, and derives account
// provenance so clients cannot label user input as Agent analysis.
func (s *ProjectProfileSetup) Normalize() {
	if s == nil {
		return
	}
	s.AccountStatus = strings.TrimSpace(s.AccountStatus)
	if s.AccountStatus == "" {
		s.AccountStatus = "new"
	}
	if s.PlatformAccounts == nil {
		s.PlatformAccounts = []ProjectProfileAccount{}
	}
	for i := range s.PlatformAccounts {
		account := &s.PlatformAccounts[i]
		account.Platform = strings.TrimSpace(account.Platform)
		account.AccountName = strings.TrimSpace(account.AccountName)
		account.FollowerRange = strings.TrimSpace(account.FollowerRange)
		account.ProfileURL = strings.TrimSpace(account.ProfileURL)
		if account.ProfileURL == "" {
			account.Source = ProfileSourceNeedsSupplement
		} else {
			account.Source = ProfileSourceUserConfirmed
		}
	}
	for _, value := range []*string{
		&s.Answers.Intent.Goals, &s.Answers.Intent.Direction, &s.Answers.Intent.Differentiation,
		&s.Answers.Intent.Strengths, &s.Answers.Intent.Resources,
		&s.Answers.Content.Preferences, &s.Answers.Content.Formats, &s.Answers.Content.Tone, &s.Answers.Content.Audience,
		&s.Answers.Content.Favorites, &s.Answers.Content.ReferenceAccounts,
		&s.Answers.Boundaries.Exclusions, &s.Answers.Boundaries.Collaboration, &s.Answers.Boundaries.Compliance,
	} {
		*value = strings.TrimSpace(*value)
	}
	if s.Samples == nil {
		s.Samples = []string{}
	}
	for i := range s.Samples {
		s.Samples[i] = strings.TrimSpace(s.Samples[i])
	}
}

func (s ProjectProfileSetup) Validate() error {
	if s.AccountStatus != "new" && s.AccountStatus != "existing" {
		return fmt.Errorf("profile_setup.account_status must be new or existing")
	}
	if len(s.PlatformAccounts) > MaxProfileAccountLinks {
		return fmt.Errorf("too many profile_setup.platform_accounts")
	}
	for i, account := range s.PlatformAccounts {
		if _, supported := profileAccountPlatforms[account.Platform]; !supported {
			return fmt.Errorf("profile_setup.platform_accounts[%d].platform is unsupported", i)
		}
		if len([]byte(account.ProfileURL)) > MaxProfileAccountURLBytes {
			return fmt.Errorf("profile_setup.platform_accounts[%d].profile_url is too long", i)
		}
		if len([]byte(account.AccountName)) > MaxProfileAccountNameBytes || len([]byte(account.FollowerRange)) > MaxProfileFollowerRangeBytes {
			return fmt.Errorf("profile_setup.platform_accounts[%d] account details are too long", i)
		}
		if account.ProfileURL == "" {
			if account.Source != ProfileSourceNeedsSupplement {
				return fmt.Errorf("profile_setup.platform_accounts[%d].source is invalid", i)
			}
		} else {
			if !isPublicHTTPURL(account.ProfileURL) {
				return fmt.Errorf("profile_setup.platform_accounts[%d].profile_url must be a public http(s) URL", i)
			}
			if account.Source != ProfileSourceUserConfirmed {
				return fmt.Errorf("profile_setup.platform_accounts[%d].source is invalid", i)
			}
		}
	}
	for name, value := range map[string]string{
		"answers.intent.goals":               s.Answers.Intent.Goals,
		"answers.intent.direction":           s.Answers.Intent.Direction,
		"answers.intent.differentiation":     s.Answers.Intent.Differentiation,
		"answers.intent.strengths":           s.Answers.Intent.Strengths,
		"answers.intent.resources":           s.Answers.Intent.Resources,
		"answers.content.preferences":        s.Answers.Content.Preferences,
		"answers.content.formats":            s.Answers.Content.Formats,
		"answers.content.tone":               s.Answers.Content.Tone,
		"answers.content.audience":           s.Answers.Content.Audience,
		"answers.content.favorites":          s.Answers.Content.Favorites,
		"answers.content.reference_accounts": s.Answers.Content.ReferenceAccounts,
		"answers.boundaries.exclusions":      s.Answers.Boundaries.Exclusions,
		"answers.boundaries.collaboration":   s.Answers.Boundaries.Collaboration,
		"answers.boundaries.compliance":      s.Answers.Boundaries.Compliance,
	} {
		if len([]byte(value)) > MaxProfileAnswerBytes {
			return fmt.Errorf("profile_setup.%s is too long", name)
		}
	}
	if len(s.Samples) > MaxProfileSamples {
		return fmt.Errorf("too many profile_setup.samples")
	}
	for i, sample := range s.Samples {
		if len([]byte(sample)) > MaxProfileSampleBytes {
			return fmt.Errorf("profile_setup.samples[%d] is too long", i)
		}
	}
	return nil
}

func (s ProjectProfileSetup) HasAnalysisInput() bool {
	for _, account := range s.PlatformAccounts {
		if strings.TrimSpace(account.ProfileURL) != "" {
			return true
		}
	}
	if len(s.Samples) > 0 {
		return true
	}
	for _, value := range []string{
		s.Answers.Intent.Goals, s.Answers.Intent.Direction, s.Answers.Intent.Differentiation,
		s.Answers.Intent.Strengths, s.Answers.Intent.Resources,
		s.Answers.Content.Preferences, s.Answers.Content.Formats, s.Answers.Content.Tone, s.Answers.Content.Audience,
		s.Answers.Content.Favorites, s.Answers.Content.ReferenceAccounts,
		s.Answers.Boundaries.Exclusions, s.Answers.Boundaries.Collaboration, s.Answers.Boundaries.Compliance,
	} {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func (s ProjectProfileSetup) PrimaryProfileURL(projectPlatform string) string {
	preferred := ""
	switch strings.TrimSpace(projectPlatform) {
	case PlatformWechat, AgentIDArticle, AgentIDWechatPicture:
		preferred = PlatformWechat
	case PlatformSeednote:
		preferred = PlatformSeednote
	}
	if preferred != "" {
		for _, account := range s.PlatformAccounts {
			if account.Platform == preferred && account.ProfileURL != "" {
				return account.ProfileURL
			}
		}
	}
	for _, account := range s.PlatformAccounts {
		if account.ProfileURL != "" {
			return account.ProfileURL
		}
	}
	return ""
}

func isPublicHTTPURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast()) {
		return false
	}
	return true
}
