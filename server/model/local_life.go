package model

import (
	"fmt"
	"strings"

	"gorm.io/datatypes"
)

type LocalLifeMixType struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Description         string   `json:"description"`
	UseCases            string   `json:"use_cases"`
	RecommendedAssets   []string `json:"recommended_assets"`
	ShotOrder           string   `json:"shot_order"`
	RecommendedDuration string   `json:"recommended_duration"`
	Guardrails          []string `json:"guardrails"`
	VoiceStyle          string   `json:"voice_style"`
}

type LocalLifeAsset struct {
	Type       string `json:"type"`
	URL        string `json:"url,omitempty"`
	TaskFileID string `json:"task_file_id,omitempty"`
	FileName   string `json:"file_name,omitempty"`
	MimeType   string `json:"mime_type,omitempty"`
	FileSize   int64  `json:"file_size,omitempty"`
}

type LocalLifeInput struct {
	MixType               string           `json:"mix_type"`
	Brief                 string           `json:"brief"`
	MerchantFacts         string           `json:"merchant_facts,omitempty"`
	SourceAssets          []LocalLifeAsset `json:"source_assets"`
	VariantCount          int              `json:"variant_count"`
	TargetDurationSeconds int64            `json:"target_duration_seconds"`
	AspectRatio           string           `json:"aspect_ratio"`
	VoiceID               string           `json:"voice_id,omitempty"`
	BGMAssetID            string           `json:"bgm_asset_id,omitempty"`
}

func (t *Task) SetLocalLifeInput(input LocalLifeInput) {
	t.LocalLifeInput = datatypes.NewJSONType(input)
}

func LocalLifeMixTypes() []LocalLifeMixType {
	return []LocalLifeMixType{
		{ID: "restaurant", Name: "餐饮门店", Description: "突出门头、环境、菜品特写、用餐氛围和真实优惠。", UseCases: "火锅、烧烤、咖啡、餐馆、甜品店", RecommendedAssets: []string{"门头视频或照片", "环境视频", "菜品特写"}, ShotOrder: "门头→环境→菜品→用餐氛围→到店理由", RecommendedDuration: "20-40 秒", Guardrails: []string{"不得编造价格、优惠、评价"}, VoiceStyle: "热情、真实、生活化"},
		{ID: "retail", Name: "零售门店", Description: "突出商品细节、陈列、店内环境、上新和促销活动。", UseCases: "服装、美妆、便利店、家居、数码", RecommendedAssets: []string{"店铺外观", "商品细节", "陈列环境"}, ShotOrder: "门店→陈列→商品细节→上新或活动→到店理由", RecommendedDuration: "15-35 秒", Guardrails: []string{"促销信息必须来自用户事实"}, VoiceStyle: "清晰、利落、有购买动机"},
		{ID: "personal-service", Name: "美业与生活服务", Description: "突出服务过程、设施环境、体验流程和预约信息。", UseCases: "美发、美容、健身、洗护、摄影、维修", RecommendedAssets: []string{"服务过程", "设施环境", "工作人员或工具"}, ShotOrder: "空间→服务流程→细节→预约方式→到店理由", RecommendedDuration: "20-40 秒", Guardrails: []string{"不得虚构效果或医疗功效"}, VoiceStyle: "专业、克制、可信"},
		{ID: "leisure", Name: "休闲娱乐与场所", Description: "突出空间氛围、设施、活动过程和到店体验。", UseCases: "民宿、景区、亲子乐园、KTV、展馆、运动场馆", RecommendedAssets: []string{"空间全景", "设施细节", "活动过程"}, ShotOrder: "入口或全景→设施→活动→氛围→到店理由", RecommendedDuration: "20-45 秒", Guardrails: []string{"不得承诺素材未展示的服务"}, VoiceStyle: "轻松、沉浸、有画面感"},
		{ID: "event", Name: "活动与开业宣传", Description: "突出现场氛围、活动亮点、时间地点和参与方式。", UseCases: "开业、周年庆、团建、展览、节日活动、线下集市", RecommendedAssets: []string{"现场全景", "活动过程", "标识或舞台"}, ShotOrder: "现场→亮点→参与过程→时间地点→行动提示", RecommendedDuration: "15-40 秒", Guardrails: []string{"时间、地点和活动内容必须有事实依据"}, VoiceStyle: "有节奏、明确、带行动提示"},
		{ID: "general-store", Name: "通用门店", Description: "适合无法归类的门店，生成环境、商品或服务和到店理由的稳健介绍。", UseCases: "综合门店、社区商家、首次尝试", RecommendedAssets: []string{"门店外观", "店内环境", "商品或服务细节"}, ShotOrder: "门店→环境→商品或服务→到店理由", RecommendedDuration: "20-40 秒", Guardrails: []string{"只使用用户提供的事实"}, VoiceStyle: "自然、稳健、易理解"},
	}
}

func LocalLifeMixTypeByID(id string) *LocalLifeMixType {
	for _, item := range LocalLifeMixTypes() {
		if item.ID == strings.TrimSpace(id) {
			copy := item
			return &copy
		}
	}
	return nil
}

func (in *LocalLifeInput) Validate() error {
	if in == nil {
		return fmt.Errorf("local_life_input is required")
	}
	if strings.TrimSpace(in.Brief) == "" {
		return fmt.Errorf("brief is required")
	}
	if in.MixType == "" {
		in.MixType = "general-store"
	}
	if LocalLifeMixTypeByID(in.MixType) == nil {
		return fmt.Errorf("unsupported mix_type %q", in.MixType)
	}
	if in.VariantCount == 0 {
		in.VariantCount = 3
	}
	if in.VariantCount < 1 || in.VariantCount > 10 {
		return fmt.Errorf("variant_count must be between 1 and 10")
	}
	if in.TargetDurationSeconds == 0 {
		in.TargetDurationSeconds = 30
	}
	if in.TargetDurationSeconds < 10 || in.TargetDurationSeconds > 60 {
		return fmt.Errorf("target_duration_seconds must be between 10 and 60")
	}
	if in.AspectRatio == "" {
		in.AspectRatio = "9:16"
	}
	if in.AspectRatio != "9:16" && in.AspectRatio != "16:9" && in.AspectRatio != "1:1" {
		return fmt.Errorf("aspect_ratio must be 9:16, 16:9, or 1:1")
	}
	if len(in.SourceAssets) == 0 {
		return fmt.Errorf("at least one source asset is required")
	}
	for i, asset := range in.SourceAssets {
		if asset.Type != "video" && asset.Type != "image" {
			return fmt.Errorf("source_assets[%d].type must be video or image", i)
		}
		if strings.TrimSpace(asset.TaskFileID) == "" && strings.TrimSpace(asset.URL) == "" {
			return fmt.Errorf("source_assets[%d] requires task_file_id or url", i)
		}
		if asset.TaskFileID != "" && asset.URL != "" {
			return fmt.Errorf("source_assets[%d] cannot contain both task_file_id and url", i)
		}
	}
	return nil
}
