package model

import "gorm.io/gorm"

// AutoMigrate creates or updates all database tables.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&User{},
		&LoginSession{},
		&Project{},
		&Plan{},
		&Task{},
		&TaskExecution{},
		&TaskFile{},
		&PendingUpload{},
		&APIKey{},
		&Feedback{},
		&UserModelConfig{},
		&SeednotePostTracking{},
		&SeednoteMetricSnapshot{},
		&Template{},
		&ViralAnalysis{},
		&PosterTask{},
		&TopicPool{},
		&ImageGeneration{},
		&ImageGenerationResult{},
		&VideoGeneration{},
		&VideoGenerationSegment{},
		&AgentFeedback{},
		&IlinkBinding{},
		&IlinkNotification{},
		&BillingWalletAccount{},
		&BillingCreditLot{},
		&BillingWalletEntry{},
		&BillingCatalogVersion{},
		&BillingSKU{},
		&BillingQuote{},
		&BillingCharge{},
		&BillingChargeAllocation{},
		&BillingDebtAllocation{},
		&BillingSettlementOutbox{},
		&BillingReferralIssue{},
		&BillingProviderCostEvent{},
		&BillingExecutionCostStatus{},
		&BillingMarginFact{},
	)
}
