package model

import (
	"time"

	"gorm.io/datatypes"
)

const ContentTaxonomyVersion = "2.0"

const (
	ContentMetadataPending   = "pending"
	ContentMetadataSucceeded = "succeeded"
	ContentMetadataFailed    = "failed"
	ContentTagCanonical      = "canonical"
	ContentTagCandidate      = "candidate"
	ContentTagDiagnostic     = "diagnostic"
	// ContentOriginHotSearch is a Server-owned provenance marker for content
	// created from a hot-search source. It is surfaced as a canonical metadata
	// tag without being mixed into user-authored tags.
	ContentOriginHotSearch = "hot_search"
)

var ContentTagDimensions = map[string]struct{}{
	"domain": {}, "topic": {}, "audience": {}, "content_goal": {}, "format": {}, "tone": {}, "value_proposition": {}, "source_relation": {}, "evidence_level": {}, "risk": {}, "media_form": {}, "content_type": {}, "platform": {}, "artifact_role": {}, "performance": {}, "presentation_device": {}, "funnel_stage": {},
}
var ContentTagGroups = map[string]string{
	"domain": "content_fact", "topic": "content_fact", "audience": "content_fact",
	"content_goal": "strategy_signal", "format": "editorial_structure", "tone": "editorial_structure",
	"presentation_device": "editorial_structure", "funnel_stage": "strategy_signal", "value_proposition": "strategy_signal",
	"source_relation": "source_risk", "evidence_level": "source_risk", "risk": "source_risk",
	"media_form": "media_performance", "content_type": "media_performance", "platform": "media_performance",
	"artifact_role": "media_performance", "performance": "media_performance",
}

// ContentTagDimensionLimits keeps noisy multi-value output from becoming an
// accidental taxonomy. A zero value means the dimension is not accepted.
var ContentTagDimensionLimits = map[string]int{
	"domain": 2, "topic": 3, "audience": 2, "content_goal": 1, "format": 2,
	"tone": 2, "value_proposition": 3, "source_relation": 1, "evidence_level": 2,
	"risk": 3, "media_form": 1, "content_type": 1, "platform": 1, "artifact_role": 1,
	"performance": 1, "presentation_device": 3, "funnel_stage": 1,
}

type ContentTagDefaultVocabulary struct {
	Value       string
	DisplayName string
	Aliases     []string
}

var ContentTagDefaultVocabularies = map[string][]ContentTagDefaultVocabulary{
	"domain":              {{Value: "tea", DisplayName: "茶叶", Aliases: []string{"茶", "茶饮", "茶叶"}}, {Value: "software", DisplayName: "软件", Aliases: []string{"软件", "saas", "人工智能"}}, {Value: "education", DisplayName: "教育", Aliases: []string{"教育", "课程", "学习"}}, {Value: "beauty", DisplayName: "美妆", Aliases: []string{"美妆", "护肤"}}},
	"topic":               {{Value: "tea_brewing", DisplayName: "泡茶", Aliases: []string{"泡茶", "冲泡"}}, {Value: "autumn_tea", DisplayName: "秋季饮茶", Aliases: []string{"秋季饮茶"}}},
	"content_goal":        {{Value: "awareness", DisplayName: "认知"}, {Value: "education", DisplayName: "教育"}, {Value: "evaluation", DisplayName: "评估"}, {Value: "conversion", DisplayName: "转化"}},
	"audience":            {{Value: "beginner", DisplayName: "新手", Aliases: []string{"新手", "入门", "小白"}}, {Value: "professional", DisplayName: "专业人士", Aliases: []string{"专业人士", "从业者"}}},
	"funnel_stage":        {{Value: "awareness", DisplayName: "认知"}, {Value: "evaluation", DisplayName: "评估"}, {Value: "purchase", DisplayName: "购买"}},
	"format":              {{Value: "tutorial", DisplayName: "教程"}, {Value: "listicle", DisplayName: "清单"}, {Value: "review_compare", DisplayName: "测评对比"}, {Value: "story_case", DisplayName: "故事案例"}},
	"presentation_device": {{Value: "rhetorical_question", DisplayName: "反问"}, {Value: "numbered_promise", DisplayName: "数字承诺"}, {Value: "scenario_opening", DisplayName: "场景开场"}},
	"tone":                {{Value: "professional", DisplayName: "专业"}, {Value: "humorous", DisplayName: "幽默"}, {Value: "sharp", DisplayName: "犀利"}, {Value: "empathetic", DisplayName: "共情"}},
	"value_proposition":   {{Value: "cost_saving", DisplayName: "省钱"}, {Value: "efficiency", DisplayName: "效率"}, {Value: "risk_avoidance", DisplayName: "避坑"}, {Value: "emotional_value", DisplayName: "情绪价值"}},
	"source_relation":     {{Value: "original", DisplayName: "原创"}, {Value: "adapted", DisplayName: "改编"}, {Value: "hot_search", DisplayName: "热搜"}},
	"evidence_level":      {{Value: "cited_or_data_supported", DisplayName: "引用或数据支持"}, {Value: "experience_based", DisplayName: "经验支持"}},
	"risk":                {{Value: "distribution_or_lead_risk", DisplayName: "导流风险"}},
	"media_form":          {{Value: "article", DisplayName: "文章"}, {Value: "graphic_text", DisplayName: "图文"}, {Value: "image", DisplayName: "图片"}, {Value: "video", DisplayName: "视频"}, {Value: "commerce_asset", DisplayName: "电商素材"}},
	"content_type":        {{Value: "wechat-article", DisplayName: "公众号文章"}, {Value: "wechat-picture", DisplayName: "公众号贴图"}, {Value: "seednote", DisplayName: "种草笔记"}},
	"platform":            {{Value: "wechat", DisplayName: "微信公众号"}, {Value: "seednote", DisplayName: "种草平台"}},
	"artifact_role":       {{Value: "final", DisplayName: "最终交付"}, {Value: "final_markdown", DisplayName: "最终 Markdown"}, {Value: "content", DisplayName: "内容"}, {Value: "copywriting", DisplayName: "文案"}, {Value: "final_video", DisplayName: "最终视频"}, {Value: "draft", DisplayName: "草稿"}, {Value: "html", DisplayName: "HTML"}},
	"performance":         {{Value: "viral", DisplayName: "爆款", Aliases: []string{"爆款", "爆文", "爆款内容"}}},
}

type ContentMetadataReport struct {
	ID              string         `gorm:"type:char(36);primaryKey" json:"id"`
	TaskID          string         `gorm:"type:varchar(128);not null;uniqueIndex:idx_content_metadata_task_execution,priority:1;index" json:"task_id"`
	ExecutionID     string         `gorm:"type:varchar(128);not null;uniqueIndex:idx_content_metadata_task_execution,priority:2" json:"execution_id"`
	Status          string         `gorm:"type:varchar(20);not null;index" json:"status"`
	TaggingStatus   string         `gorm:"type:varchar(20);not null;default:pending;index" json:"tagging_status"`
	FeedbackStatus  string         `gorm:"type:varchar(20);not null;default:pending;index" json:"feedback_status"`
	TaxonomyVersion string         `gorm:"type:varchar(32);not null" json:"taxonomy_version"`
	ContentSubject  string         `gorm:"type:varchar(255);not null;default:'';index" json:"content_subject"`
	ArtifactRole    string         `gorm:"type:varchar(80);not null;default:''" json:"artifact_role"`
	Platform        string         `gorm:"type:varchar(40);not null;default:''" json:"platform"`
	ContentType     string         `gorm:"type:varchar(80);not null;default:''" json:"content_type"`
	SourceDigest    string         `gorm:"type:char(64)" json:"source_digest,omitempty"`
	RawMetadata     datatypes.JSON `gorm:"type:json;not null" json:"-"`
	ErrorMessage    string         `gorm:"type:text" json:"error_message,omitempty"`
	Attempts        int            `gorm:"not null;default:0" json:"attempts"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type ContentTagAssignment struct {
	ID               string         `gorm:"type:char(36);primaryKey" json:"id"`
	ReportID         string         `gorm:"type:char(36);not null;index:idx_content_tag_report_dimension,priority:1" json:"report_id"`
	Dimension        string         `gorm:"type:varchar(40);not null;index:idx_content_tag_report_dimension,priority:2" json:"dimension"`
	Value            string         `gorm:"type:varchar(160);not null" json:"value"`
	DisplayName      string         `gorm:"type:varchar(160);not null;default:''" json:"display_name,omitempty"`
	CanonicalValue   string         `gorm:"type:varchar(160);not null;index" json:"canonical_value"`
	LabelStatus      string         `gorm:"type:varchar(20);not null" json:"label_status"`
	Confidence       float64        `gorm:"not null;default:0" json:"confidence"`
	Primary          bool           `gorm:"not null;default:false" json:"primary"`
	Evidence         datatypes.JSON `gorm:"type:json" json:"evidence,omitempty"`
	ContentSubject   string         `gorm:"type:varchar(255);not null;default:'';index" json:"content_subject"`
	ArtifactRole     string         `gorm:"type:varchar(80);not null;default:''" json:"artifact_role"`
	TagGroup         string         `gorm:"type:varchar(40);not null;default:'';index" json:"tag_group"`
	Source           string         `gorm:"type:varchar(40);not null;default:'agent'" json:"source"`
	AnalyticsVisible bool           `gorm:"not null;default:false;index" json:"analytics_visible"`
	TaxonomyVersion  string         `gorm:"type:varchar(32);not null;default:'2.0';index" json:"taxonomy_version"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

type ContentTagVocabulary struct {
	ID              string         `gorm:"type:char(36);primaryKey" json:"id"`
	Dimension       string         `gorm:"type:varchar(40);not null;uniqueIndex:idx_content_tag_vocab_dimension_value,priority:1" json:"dimension"`
	Value           string         `gorm:"type:varchar(160);not null;uniqueIndex:idx_content_tag_vocab_dimension_value,priority:2" json:"value"`
	DisplayName     string         `gorm:"type:varchar(160);not null" json:"display_name"`
	Status          string         `gorm:"type:varchar(20);not null;default:canonical" json:"status"`
	Aliases         datatypes.JSON `gorm:"type:json" json:"aliases,omitempty"`
	TaxonomyVersion string         `gorm:"type:varchar(32);not null;uniqueIndex:idx_content_tag_vocab_dimension_value,priority:3" json:"taxonomy_version"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

func (ContentMetadataReport) TableName() string { return "content_metadata_reports" }
func (ContentTagAssignment) TableName() string  { return "content_tag_assignments" }
func (ContentTagVocabulary) TableName() string  { return "content_tag_vocabularies" }
