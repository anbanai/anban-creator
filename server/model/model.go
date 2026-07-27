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
		&TaskFileObjectCleanup{},
		&UploadSession{},
		&Asset{},
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
		&DesignerReference{},
		&AgentFeedback{},
		&IlinkBinding{},
		&IlinkNotification{},
		&BillingWalletAccount{},
		&BillingCreditLot{},
		&BillingWalletEntry{},
		&BillingCatalogVersion{},
		&BillingSKU{},
		&BillingSKUTierPrice{},
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
