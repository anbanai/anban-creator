package model

import (
	"time"

	"gorm.io/datatypes"
)

const ContentTaxonomyVersion = "1.0"

const (
	ContentMetadataPending   = "pending"
	ContentMetadataSucceeded = "succeeded"
	ContentMetadataFailed    = "failed"
	ContentTagCanonical      = "canonical"
	ContentTagCandidate      = "candidate"
)

var ContentTagDimensions = map[string]struct{}{
	"industry": {}, "topic": {}, "audience": {}, "intent": {}, "funnel_stage": {},
	"format": {}, "narrative_hook": {}, "tone": {}, "value_proposition": {},
	"media_shape": {}, "source_relation": {}, "evidence_level": {}, "visual_style": {}, "risk": {},
}

type ContentTagDefaultVocabulary struct {
	Value       string
	DisplayName string
	Aliases     []string
}

var ContentTagDefaultVocabularies = map[string][]ContentTagDefaultVocabulary{
	"industry":          {{Value: "tea", DisplayName: "茶叶", Aliases: []string{"茶", "茶饮", "茶叶"}}, {Value: "software", DisplayName: "软件", Aliases: []string{"软件", "saas", "人工智能"}}, {Value: "beauty", DisplayName: "美妆", Aliases: []string{"美妆", "护肤"}}},
	"audience":          {{Value: "beginner", DisplayName: "新手", Aliases: []string{"新手", "入门", "小白"}}, {Value: "professional", DisplayName: "专业人士", Aliases: []string{"专业人士", "从业者"}}},
	"intent":            {{Value: "education", DisplayName: "教育", Aliases: []string{"教程", "指南"}}, {Value: "conversion", DisplayName: "转化", Aliases: []string{"购买", "下单"}}, {Value: "discovery", DisplayName: "种草", Aliases: []string{"发现", "种草"}}},
	"funnel_stage":      {{Value: "awareness", DisplayName: "认知"}, {Value: "evaluation", DisplayName: "评估"}, {Value: "purchase", DisplayName: "购买"}},
	"format":            {{Value: "tutorial", DisplayName: "教程"}, {Value: "listicle", DisplayName: "清单"}, {Value: "review_compare", DisplayName: "测评对比"}, {Value: "story_case", DisplayName: "故事案例"}},
	"narrative_hook":    {{Value: "rhetorical_question", DisplayName: "反问"}, {Value: "pain_point", DisplayName: "痛点"}, {Value: "numbered_promise", DisplayName: "数字承诺"}, {Value: "scenario", DisplayName: "场景"}},
	"tone":              {{Value: "professional", DisplayName: "专业"}, {Value: "humorous", DisplayName: "幽默"}, {Value: "sharp", DisplayName: "犀利"}, {Value: "empathetic", DisplayName: "共情"}},
	"value_proposition": {{Value: "cost_saving", DisplayName: "省钱"}, {Value: "efficiency", DisplayName: "效率"}, {Value: "risk_avoidance", DisplayName: "避坑"}, {Value: "emotional_value", DisplayName: "情绪价值"}},
	"media_shape":       {{Value: "text_visual", DisplayName: "图文"}, {Value: "multimedia", DisplayName: "多媒体"}, {Value: "video", DisplayName: "视频"}, {Value: "commerce_visual", DisplayName: "电商视觉"}},
	"source_relation":   {{Value: "original", DisplayName: "原创"}, {Value: "adapted", DisplayName: "改编"}},
	"evidence_level":    {{Value: "cited_or_data_supported", DisplayName: "引用或数据支持"}, {Value: "experience_based", DisplayName: "经验支持"}},
	"visual_style":      {{Value: "illustration", DisplayName: "插画"}, {Value: "mixed_visual", DisplayName: "混合视觉"}},
	"risk":              {{Value: "none_detected", DisplayName: "未发现风险"}, {Value: "distribution_or_lead_risk", DisplayName: "导流风险"}},
}

type ContentMetadataReport struct {
	ID              string         `gorm:"type:char(36);primaryKey" json:"id"`
	TaskID          string         `gorm:"type:varchar(128);not null;uniqueIndex:idx_content_metadata_task_execution,priority:1;index" json:"task_id"`
	ExecutionID     string         `gorm:"type:varchar(128);not null;uniqueIndex:idx_content_metadata_task_execution,priority:2" json:"execution_id"`
	Status          string         `gorm:"type:varchar(20);not null;index" json:"status"`
	TaggingStatus   string         `gorm:"type:varchar(20);not null;default:pending;index" json:"tagging_status"`
	FeedbackStatus  string         `gorm:"type:varchar(20);not null;default:pending;index" json:"feedback_status"`
	TaxonomyVersion string         `gorm:"type:varchar(32);not null" json:"taxonomy_version"`
	SourceDigest    string         `gorm:"type:char(64)" json:"source_digest,omitempty"`
	RawMetadata     datatypes.JSON `gorm:"type:json;not null" json:"-"`
	ErrorMessage    string         `gorm:"type:text" json:"error_message,omitempty"`
	Attempts        int            `gorm:"not null;default:0" json:"attempts"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type ContentTagAssignment struct {
	ID             string         `gorm:"type:char(36);primaryKey" json:"id"`
	ReportID       string         `gorm:"type:char(36);not null;index:idx_content_tag_report_dimension,priority:1" json:"report_id"`
	Dimension      string         `gorm:"type:varchar(40);not null;index:idx_content_tag_report_dimension,priority:2" json:"dimension"`
	Value          string         `gorm:"type:varchar(160);not null" json:"value"`
	DisplayName    string         `gorm:"type:varchar(160);not null;default:''" json:"display_name,omitempty"`
	CanonicalValue string         `gorm:"type:varchar(160);not null;index" json:"canonical_value"`
	LabelStatus    string         `gorm:"type:varchar(20);not null" json:"label_status"`
	Confidence     float64        `gorm:"not null;default:0" json:"confidence"`
	Primary        bool           `gorm:"not null;default:false" json:"primary"`
	Evidence       datatypes.JSON `gorm:"type:json" json:"evidence,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
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
