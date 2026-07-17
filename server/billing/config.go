package billing

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var ErrInvalidConfig = errors.New("invalid billing configuration")

type ConfigError struct {
	File  string
	Field string
	Err   error
}

func (e *ConfigError) Error() string {
	location := e.File
	if e.Field != "" {
		location += ": " + e.Field
	}
	if e.Err == nil {
		return location + ": invalid billing configuration"
	}
	return location + ": " + e.Err.Error()
}

func (e *ConfigError) Unwrap() error { return e.Err }

func (e *ConfigError) Is(target error) bool { return target == ErrInvalidConfig }

type Bundle struct {
	Policy     PolicyCatalog
	Products   ProductCatalog
	Costs      CostCatalog
	Promotions PromotionCatalog
}

type PolicyCatalog struct {
	Version             string                    `yaml:"version"`
	CreditsPerCNY       int64                     `yaml:"credits_per_cny"`
	TaskAdmission       TaskAdmissionPolicy       `yaml:"task_admission"`
	AcceptedTask        AcceptedTaskPolicy        `yaml:"accepted_task"`
	TopUp               TopUpPolicy               `yaml:"top_up"`
	Promotions          PromotionsPolicy          `yaml:"promotions"`
	TaskFailureReversal TaskFailureReversalPolicy `yaml:"task_failure_reversal"`
}

type TaskAdmissionPolicy struct {
	RequireZeroDebt  bool `yaml:"require_zero_debt"`
	RequireFullPrice bool `yaml:"require_full_price"`
}

type AcceptedTaskPolicy struct {
	ContinueWhenBalanceNegative  bool `yaml:"continue_when_balance_negative"`
	OperationChargeMayCreateDebt bool `yaml:"operation_charge_may_create_debt"`
}

type TopUpPolicy struct {
	RepayDebtFirst bool `yaml:"repay_debt_first"`
}

type PromotionsPolicy struct {
	MayRepayDebt bool `yaml:"may_repay_debt"`
}

type TaskFailureReversalPolicy struct {
	Enabled bool     `yaml:"enabled"`
	Reasons []string `yaml:"reasons"`
}

type ProductCatalog struct {
	CatalogID string      `yaml:"catalog_id"`
	Currency  string      `yaml:"currency"`
	SKUs      []SKUConfig `yaml:"skus"`
}

type SKUConfig struct {
	ID           string `yaml:"id"`
	Operation    string `yaml:"operation"`
	ChargePolicy string `yaml:"charge_policy"`
	PriceCredits int64  `yaml:"price_credits"`
	Route        string `yaml:"route"`
	Delivery     string `yaml:"delivery"`
}

type rawCostCatalog struct {
	CatalogID     string                    `yaml:"catalog_id"`
	CurrencyRates map[string]decimalString  `yaml:"currency_rates"`
	Models        map[string]rawModelConfig `yaml:"models"`
}

type rawModelConfig struct {
	PricingType        string        `yaml:"pricing_type"`
	Currency           string        `yaml:"currency"`
	Unit               int64         `yaml:"unit"`
	Input              decimalString `yaml:"input"`
	CacheReadInput     decimalString `yaml:"cache_read_input"`
	CacheCreationInput decimalString `yaml:"cache_creation_input"`
	Output             decimalString `yaml:"output"`
	Tiers              []rawCostTier `yaml:"tiers"`
	OperatorEvidence   string        `yaml:"operator_evidence"`
	EffectiveAt        string        `yaml:"effective_at"`
}

type rawCostTier struct {
	MaxPixels int64         `yaml:"max_pixels"`
	Price     decimalString `yaml:"price"`
}

type CostCatalog struct {
	CatalogID     string
	CurrencyRates map[string]MicroCNY
	Models        map[string]ModelCostConfig
}

type ModelCostConfig struct {
	PricingType        string
	Currency           string
	Unit               int64
	Input              MicroCNY
	CacheReadInput     MicroCNY
	CacheCreationInput MicroCNY
	Output             MicroCNY
	Tiers              []CostTier
	OperatorEvidence   string
	EffectiveAt        time.Time
}

type CostTier struct {
	MaxPixels int64
	Price     MicroCNY
}

type rawPromotionCatalog struct {
	CatalogID string               `yaml:"catalog_id"`
	Programs  []rawReferralProgram `yaml:"programs"`
}

type rawReferralProgram struct {
	ID                string        `yaml:"id"`
	Trigger           string        `yaml:"trigger"`
	MinimumTopUpCNY   decimalString `yaml:"minimum_topup_cny"`
	InviterCredits    int64         `yaml:"inviter_credits"`
	InviteeCredits    int64         `yaml:"invitee_credits"`
	ExpiresAfter      string        `yaml:"expires_after"`
	MaxInviterRewards int64         `yaml:"max_inviter_rewards"`
	CanRepayDebt      bool          `yaml:"can_repay_debt"`
}

type PromotionCatalog struct {
	CatalogID string
	Programs  []ReferralProgram
}

type ReferralProgram struct {
	ID                string
	Trigger           string
	MinimumTopUpCNY   MicroCNY
	InviterCredits    int64
	InviteeCredits    int64
	ExpiresAfter      time.Duration
	MaxInviterRewards int64
	CanRepayDebt      bool
}

type decimalString string

func (d *decimalString) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return errors.New("must be a quoted decimal string")
	}
	*d = decimalString(node.Value)
	return nil
}

func LoadBundle(dir string) (*Bundle, error) {
	var policy PolicyCatalog
	if err := decodeRequired(filepath.Join(dir, "policy.yaml"), &policy); err != nil {
		return nil, err
	}
	var products ProductCatalog
	if err := decodeRequired(filepath.Join(dir, "products.yaml"), &products); err != nil {
		return nil, err
	}
	var rawCosts rawCostCatalog
	if err := decodeRequired(filepath.Join(dir, "costs.yaml"), &rawCosts); err != nil {
		return nil, err
	}
	var rawPromotions rawPromotionCatalog
	if err := decodeRequired(filepath.Join(dir, "promotions.yaml"), &rawPromotions); err != nil {
		return nil, err
	}

	costs, err := validateCosts(rawCosts)
	if err != nil {
		return nil, err
	}
	promotions, err := validatePromotions(rawPromotions)
	if err != nil {
		return nil, err
	}
	bundle := &Bundle{Policy: policy, Products: products, Costs: costs, Promotions: promotions}
	if err := validateBundle(bundle); err != nil {
		return nil, err
	}
	return bundle, nil
}

func decodeRequired(path string, destination any) error {
	file, err := os.Open(path)
	if err != nil {
		return configError(filepath.Base(path), "", err)
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(destination); err != nil {
		if errors.Is(err, io.EOF) {
			err = errors.New("empty YAML document")
		}
		return configError(filepath.Base(path), "", err)
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("trailing YAML document")
		}
		return configError(filepath.Base(path), "", err)
	}
	return nil
}

func validateBundle(bundle *Bundle) error {
	bundle.Policy.Version = strings.TrimSpace(bundle.Policy.Version)
	if bundle.Policy.Version == "" {
		return configError("policy.yaml", "version", errors.New("is required"))
	}
	if bundle.Policy.CreditsPerCNY <= 0 {
		return configError("policy.yaml", "credits_per_cny", errors.New("must be positive"))
	}
	if !bundle.Policy.TaskAdmission.RequireZeroDebt {
		return configError("policy.yaml", "task_admission.require_zero_debt", errors.New("must be true"))
	}
	if !bundle.Policy.TaskAdmission.RequireFullPrice {
		return configError("policy.yaml", "task_admission.require_full_price", errors.New("must be true"))
	}
	if !bundle.Policy.AcceptedTask.ContinueWhenBalanceNegative {
		return configError("policy.yaml", "accepted_task.continue_when_balance_negative", errors.New("must be true"))
	}
	if !bundle.Policy.AcceptedTask.OperationChargeMayCreateDebt {
		return configError("policy.yaml", "accepted_task.operation_charge_may_create_debt", errors.New("must be true"))
	}
	if !bundle.Policy.TopUp.RepayDebtFirst {
		return configError("policy.yaml", "top_up.repay_debt_first", errors.New("must be true"))
	}
	if bundle.Policy.Promotions.MayRepayDebt {
		return configError("policy.yaml", "promotions.may_repay_debt", errors.New("must be false"))
	}
	if !bundle.Policy.TaskFailureReversal.Enabled {
		return configError("policy.yaml", "task_failure_reversal.enabled", errors.New("must be true"))
	}
	if len(bundle.Policy.TaskFailureReversal.Reasons) == 0 {
		return configError("policy.yaml", "task_failure_reversal.reasons", errors.New("must not be empty"))
	}
	allowedReversalReasons := map[string]struct{}{
		"platform_error":           {},
		"provider_error":           {},
		"execution_timeout":        {},
		"infrastructure_cancelled": {},
	}
	if len(bundle.Policy.TaskFailureReversal.Reasons) != len(allowedReversalReasons) {
		return configError("policy.yaml", "task_failure_reversal.reasons", errors.New("must equal the approved set: platform_error, provider_error, execution_timeout, infrastructure_cancelled"))
	}
	seenReasons := make(map[string]struct{}, len(bundle.Policy.TaskFailureReversal.Reasons))
	for index, reason := range bundle.Policy.TaskFailureReversal.Reasons {
		reason = strings.TrimSpace(reason)
		bundle.Policy.TaskFailureReversal.Reasons[index] = reason
		if reason == "" {
			return configError("policy.yaml", "task_failure_reversal.reasons", errors.New("must not contain an empty reason"))
		}
		if _, exists := seenReasons[reason]; exists {
			return configError("policy.yaml", "task_failure_reversal.reasons", fmt.Errorf("duplicate reason %q", reason))
		}
		if _, allowed := allowedReversalReasons[reason]; !allowed {
			return configError("policy.yaml", "task_failure_reversal.reasons", fmt.Errorf("unsupported reversal reason %q", reason))
		}
		seenReasons[reason] = struct{}{}
	}

	bundle.Products.CatalogID = strings.TrimSpace(bundle.Products.CatalogID)
	bundle.Products.Currency = strings.TrimSpace(bundle.Products.Currency)
	if bundle.Products.CatalogID == "" {
		return configError("products.yaml", "catalog_id", errors.New("is required"))
	}
	if bundle.Products.Currency != "credits" {
		return configError("products.yaml", "currency", errors.New("must be credits"))
	}
	if len(bundle.Products.SKUs) == 0 {
		return configError("products.yaml", "skus", errors.New("must not be empty"))
	}
	seenSKUs := make(map[string]struct{}, len(bundle.Products.SKUs))
	type billableIdentity struct {
		operation    string
		chargePolicy string
		route        string
	}
	seenBillableIdentities := make(map[billableIdentity]string, len(bundle.Products.SKUs))
	for index, sku := range bundle.Products.SKUs {
		field := fmt.Sprintf("skus[%d]", index)
		sku.ID = strings.TrimSpace(sku.ID)
		sku.Operation = strings.TrimSpace(sku.Operation)
		sku.ChargePolicy = strings.TrimSpace(sku.ChargePolicy)
		sku.Route = strings.TrimSpace(sku.Route)
		sku.Delivery = strings.TrimSpace(sku.Delivery)
		bundle.Products.SKUs[index] = sku
		if sku.ID == "" || sku.Operation == "" || sku.ChargePolicy == "" || sku.Delivery == "" {
			return configError("products.yaml", field, errors.New("id, operation, charge_policy, and delivery are required"))
		}
		if _, exists := seenSKUs[sku.ID]; exists {
			return configError("products.yaml", field+".id", fmt.Errorf("duplicate SKU id %q", sku.ID))
		}
		seenSKUs[sku.ID] = struct{}{}
		if sku.PriceCredits < 0 {
			return configError("products.yaml", field+".price_credits", errors.New("must be non-negative"))
		}
		switch sku.ChargePolicy {
		case "task_admission":
		case "accepted_task_operation", "standalone_operation":
			if sku.Route == "" {
				return configError("products.yaml", field+".route", fmt.Errorf("route is required for %s", sku.ChargePolicy))
			}
		default:
			return configError("products.yaml", field+".charge_policy", fmt.Errorf("unsupported value %q", sku.ChargePolicy))
		}
		identity := billableIdentity{
			operation:    sku.Operation,
			chargePolicy: sku.ChargePolicy,
			route:        sku.Route,
		}
		if existingID, exists := seenBillableIdentities[identity]; exists {
			return configError("products.yaml", field, fmt.Errorf("SKU %q duplicates billable identity of %q", sku.ID, existingID))
		}
		seenBillableIdentities[identity] = sku.ID
	}
	for index, program := range bundle.Promotions.Programs {
		if program.CanRepayDebt {
			return configError("promotions.yaml", fmt.Sprintf("programs[%d].can_repay_debt", index), errors.New("can_repay_debt must be false"))
		}
	}
	return nil
}

func validateCosts(raw rawCostCatalog) (CostCatalog, error) {
	costs := CostCatalog{
		CatalogID:     strings.TrimSpace(raw.CatalogID),
		CurrencyRates: make(map[string]MicroCNY, len(raw.CurrencyRates)),
		Models:        make(map[string]ModelCostConfig, len(raw.Models)),
	}
	if costs.CatalogID == "" {
		return costs, configError("costs.yaml", "catalog_id", errors.New("is required"))
	}
	for currency, value := range raw.CurrencyRates {
		canonicalCurrency := strings.TrimSpace(currency)
		if canonicalCurrency == "" {
			return costs, configError("costs.yaml", "currency_rates", errors.New("contains an empty currency"))
		}
		if _, exists := costs.CurrencyRates[canonicalCurrency]; exists {
			return costs, configError("costs.yaml", "currency_rates", fmt.Errorf("duplicate canonical currency %q", canonicalCurrency))
		}
		parsed, err := ParseMicroCNY(string(value))
		if err != nil || parsed <= 0 {
			if err == nil {
				err = errors.New("must be positive")
			}
			return costs, configError("costs.yaml", "currency_rates."+canonicalCurrency, err)
		}
		costs.CurrencyRates[canonicalCurrency] = parsed
	}
	cnyRate, exists := costs.CurrencyRates["CNY"]
	if !exists {
		return costs, configError("costs.yaml", "currency_rates.CNY", errors.New("is required"))
	}
	if cnyRate != MicroCNY(microCNYPerCNY) {
		return costs, configError("costs.yaml", "currency_rates.CNY", errors.New("must equal 1.00"))
	}
	for modelID, rawModel := range raw.Models {
		canonicalModelID := strings.TrimSpace(modelID)
		field := "models." + canonicalModelID
		if canonicalModelID == "" {
			return costs, configError("costs.yaml", "models", errors.New("contains an empty model id"))
		}
		if _, exists := costs.Models[canonicalModelID]; exists {
			return costs, configError("costs.yaml", field, fmt.Errorf("duplicate model id %q", canonicalModelID))
		}
		canonicalCurrency := strings.TrimSpace(rawModel.Currency)
		if _, exists := costs.CurrencyRates[canonicalCurrency]; !exists {
			return costs, configError("costs.yaml", field+".currency", fmt.Errorf("references missing currency rate %q", canonicalCurrency))
		}
		if strings.TrimSpace(rawModel.OperatorEvidence) == "" {
			return costs, configError("costs.yaml", field+".operator_evidence", errors.New("operator_evidence is required"))
		}
		effectiveAt, err := time.Parse(time.RFC3339, rawModel.EffectiveAt)
		if err != nil {
			return costs, configError("costs.yaml", field+".effective_at", errors.New("effective_at must be RFC3339"))
		}
		pricingType := strings.TrimSpace(rawModel.PricingType)
		model := ModelCostConfig{
			PricingType: pricingType, Currency: canonicalCurrency, Unit: rawModel.Unit,
			OperatorEvidence: strings.TrimSpace(rawModel.OperatorEvidence), EffectiveAt: effectiveAt,
		}
		prices := []struct {
			name  string
			raw   decimalString
			value *MicroCNY
		}{
			{name: "input", raw: rawModel.Input, value: &model.Input},
			{name: "cache_read_input", raw: rawModel.CacheReadInput, value: &model.CacheReadInput},
			{name: "cache_creation_input", raw: rawModel.CacheCreationInput, value: &model.CacheCreationInput},
			{name: "output", raw: rawModel.Output, value: &model.Output},
		}
		for _, price := range prices {
			if price.raw == "" {
				continue
			}
			parsed, err := ParseMicroCNY(string(price.raw))
			if err != nil {
				return costs, configError("costs.yaml", field+"."+price.name, err)
			}
			*price.value = parsed
		}
		switch pricingType {
		case "token":
			if rawModel.Unit <= 0 || rawModel.Input == "" || rawModel.CacheReadInput == "" || rawModel.CacheCreationInput == "" || rawModel.Output == "" || len(rawModel.Tiers) != 0 {
				return costs, configError("costs.yaml", field, errors.New("token pricing requires positive unit and input, cache_read_input, cache_creation_input, and output prices and forbids tiers"))
			}
			if model.Input <= 0 || model.CacheReadInput <= 0 || model.CacheCreationInput <= 0 || model.Output <= 0 {
				return costs, configError("costs.yaml", field, errors.New("token prices must be positive"))
			}
		case "output_pixel_tier":
			if len(rawModel.Tiers) == 0 || rawModel.Unit != 0 || rawModel.Input != "" || rawModel.Output != "" || rawModel.CacheReadInput != "" || rawModel.CacheCreationInput != "" {
				return costs, configError("costs.yaml", field, errors.New("output_pixel_tier pricing requires tiers and forbids token fields"))
			}
			var previousMaxPixels int64
			for index, rawTier := range rawModel.Tiers {
				parsed, err := ParseMicroCNY(string(rawTier.Price))
				if err != nil {
					return costs, configError("costs.yaml", fmt.Sprintf("%s.tiers[%d].price", field, index), err)
				}
				if parsed <= 0 {
					return costs, configError("costs.yaml", fmt.Sprintf("%s.tiers[%d].price", field, index), errors.New("price must be positive"))
				}
				last := index == len(rawModel.Tiers)-1
				if last {
					if rawTier.MaxPixels != 0 {
						return costs, configError("costs.yaml", fmt.Sprintf("%s.tiers[%d].max_pixels", field, index), errors.New("final tier must be unbounded"))
					}
				} else {
					if rawTier.MaxPixels == 0 {
						return costs, configError("costs.yaml", fmt.Sprintf("%s.tiers[%d].max_pixels", field, index), errors.New("only the final tier may be unbounded"))
					}
					if rawTier.MaxPixels < 0 {
						return costs, configError("costs.yaml", fmt.Sprintf("%s.tiers[%d].max_pixels", field, index), errors.New("bounded max_pixels must be positive"))
					}
					if rawTier.MaxPixels <= previousMaxPixels {
						return costs, configError("costs.yaml", fmt.Sprintf("%s.tiers[%d].max_pixels", field, index), errors.New("max_pixels must be strictly ascending"))
					}
					previousMaxPixels = rawTier.MaxPixels
				}
				model.Tiers = append(model.Tiers, CostTier{MaxPixels: rawTier.MaxPixels, Price: parsed})
			}
		default:
			return costs, configError("costs.yaml", field+".pricing_type", fmt.Errorf("unsupported value %q", pricingType))
		}
		costs.Models[canonicalModelID] = model
	}
	if len(costs.Models) == 0 {
		return costs, configError("costs.yaml", "models", errors.New("must not be empty"))
	}
	return costs, nil
}

func validatePromotions(raw rawPromotionCatalog) (PromotionCatalog, error) {
	promotions := PromotionCatalog{CatalogID: strings.TrimSpace(raw.CatalogID), Programs: make([]ReferralProgram, 0, len(raw.Programs))}
	if promotions.CatalogID == "" {
		return promotions, configError("promotions.yaml", "catalog_id", errors.New("is required"))
	}
	seen := make(map[string]struct{}, len(raw.Programs))
	for index, rawProgram := range raw.Programs {
		field := fmt.Sprintf("programs[%d]", index)
		programID := strings.TrimSpace(rawProgram.ID)
		if programID == "" {
			return promotions, configError("promotions.yaml", field+".id", errors.New("is required"))
		}
		if _, exists := seen[programID]; exists {
			return promotions, configError("promotions.yaml", field+".id", fmt.Errorf("duplicate referral program id %q", programID))
		}
		seen[programID] = struct{}{}
		trigger := strings.TrimSpace(rawProgram.Trigger)
		if trigger != "invitee_first_paid_topup" {
			return promotions, configError("promotions.yaml", field+".trigger", fmt.Errorf("unsupported value %q", trigger))
		}
		minimum, err := ParseMicroCNY(string(rawProgram.MinimumTopUpCNY))
		if err != nil {
			return promotions, configError("promotions.yaml", field+".minimum_topup_cny", err)
		}
		if minimum <= 0 {
			return promotions, configError("promotions.yaml", field+".minimum_topup_cny", errors.New("must be positive"))
		}
		expiresAfter, err := parseCatalogDuration(rawProgram.ExpiresAfter)
		if err != nil || expiresAfter <= 0 {
			return promotions, configError("promotions.yaml", field+".expires_after", errors.New("must be a positive duration"))
		}
		if rawProgram.InviterCredits < 0 || rawProgram.InviteeCredits < 0 || rawProgram.MaxInviterRewards < 0 {
			return promotions, configError("promotions.yaml", field, errors.New("credit values and max_inviter_rewards must be non-negative"))
		}
		promotions.Programs = append(promotions.Programs, ReferralProgram{
			ID: programID, Trigger: trigger, MinimumTopUpCNY: minimum,
			InviterCredits: rawProgram.InviterCredits, InviteeCredits: rawProgram.InviteeCredits,
			ExpiresAfter: expiresAfter, MaxInviterRewards: rawProgram.MaxInviterRewards,
			CanRepayDebt: rawProgram.CanRepayDebt,
		})
	}
	return promotions, nil
}

func configError(file, field string, err error) error {
	return &ConfigError{File: file, Field: field, Err: err}
}

func parseCatalogDuration(value string) (time.Duration, error) {
	if strings.HasSuffix(value, "d") {
		days, err := strconv.ParseInt(strings.TrimSuffix(value, "d"), 10, 64)
		if err != nil || days <= 0 || days > int64((1<<63-1)/(24*time.Hour)) {
			return 0, errors.New("invalid day duration")
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(value)
}
