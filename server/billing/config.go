package billing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	Economics  EconomicsConfig
	Policy     PolicySnapshot
	Products   ProductCatalog
	Costs      CostCatalog
	Promotions PromotionCatalog
}

type EconomicsConfig struct {
	CreditsPerCNY int64 `yaml:"credits_per_cny" json:"credits_per_cny"`
}

type PolicySnapshot struct {
	TaskAdmission       TaskAdmissionPolicy       `json:"task_admission"`
	AcceptedTask        AcceptedTaskPolicy        `json:"accepted_task"`
	TopUp               TopUpPolicy               `json:"top_up"`
	Promotions          PromotionsPolicy          `json:"promotions"`
	TaskFailureReversal TaskFailureReversalPolicy `json:"task_failure_reversal"`
}

type TaskAdmissionPolicy struct {
	RequireZeroDebt  bool `json:"require_zero_debt"`
	RequireFullPrice bool `json:"require_full_price"`
}

type AcceptedTaskPolicy struct {
	ContinueWhenBalanceNegative  bool `json:"continue_when_balance_negative"`
	OperationChargeMayCreateDebt bool `json:"operation_charge_may_create_debt"`
}

type TopUpPolicy struct {
	RepayDebtFirst bool `json:"repay_debt_first"`
}

type PromotionsPolicy struct {
	MayRepayDebt bool `json:"may_repay_debt"`
}

type TaskFailureReversalPolicy struct {
	Enabled bool     `json:"enabled"`
	Reasons []string `json:"reasons"`
}

type ProductCatalog struct {
	CatalogID        string           `yaml:"-" json:"catalog_id"`
	Currency         string           `yaml:"currency" json:"currency"`
	TierRatesPercent map[string]int64 `yaml:"tier_rates_percent" json:"tier_rates_percent"`
	SKUs             []SKUConfig      `yaml:"skus" json:"skus"`
}

type SKUConfig struct {
	ID               string `yaml:"id" json:"id"`
	Operation        string `yaml:"operation" json:"operation"`
	ExecutionProfile string `yaml:"execution_profile" json:"execution_profile,omitempty"`
	ChargePolicy     string `yaml:"charge_policy" json:"charge_policy"`
	PriceCredits     int64  `yaml:"price_credits" json:"price_credits"`
	Route            string `yaml:"route" json:"route,omitempty"`
	Delivery         string `yaml:"delivery" json:"delivery"`
}

func (c ProductCatalog) FindSKUByExecutionProfile(operation, profile string) (SKUConfig, bool) {
	for _, sku := range c.SKUs {
		if sku.Operation == operation && sku.ExecutionProfile == profile {
			return sku, true
		}
	}
	return SKUConfig{}, false
}

func (c ProductCatalog) PriceForTier(listPrice int64, tier string) (int64, bool) {
	rate, ok := c.TierRatesPercent[tier]
	if !ok || listPrice < 0 || rate < 0 || rate > 100 {
		return 0, false
	}
	whole, remainder := listPrice/100, listPrice%100
	return whole*rate + remainder*rate/100, true
}

var RequiredPricingTiers = []string{"free", "pro", "enterprise"}

type rawCostCatalog struct {
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
	TextInput          decimalString `yaml:"text_input"`
	TextCachedInput    decimalString `yaml:"text_cached_input"`
	ImageInput         decimalString `yaml:"image_input"`
	ImageCachedInput   decimalString `yaml:"image_cached_input"`
	ImageOutput        decimalString `yaml:"image_output"`
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
	TextInput          MicroCNY
	TextCachedInput    MicroCNY
	ImageInput         MicroCNY
	ImageCachedInput   MicroCNY
	ImageOutput        MicroCNY
	Tiers              []CostTier
	OperatorEvidence   string
	EffectiveAt        time.Time
}

type CostTier struct {
	MaxPixels int64
	Price     MicroCNY
}

type rawPromotionCatalog struct {
	Programs []rawReferralProgram `yaml:"programs"`
}

type rawReferralProgram struct {
	ID                string        `yaml:"id"`
	Trigger           string        `yaml:"trigger"`
	MinimumTopUpCNY   decimalString `yaml:"minimum_topup_cny"`
	InviterCredits    int64         `yaml:"inviter_credits"`
	InviteeCredits    int64         `yaml:"invitee_credits"`
	ExpiresAfter      string        `yaml:"expires_after"`
	MaxInviterRewards int64         `yaml:"max_inviter_rewards"`
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
}

const ReferralFirstTopUpProgramID = "referral-first-topup-v1"

type decimalString string

func (d *decimalString) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return errors.New("must be a quoted decimal string")
	}
	*d = decimalString(node.Value)
	return nil
}

func LoadBundle(dir string) (*Bundle, error) {
	if _, err := os.Stat(filepath.Join(dir, "policy.yaml")); err == nil {
		return nil, configError("policy.yaml", "", errors.New("is no longer supported; use economics.yaml"))
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, configError("policy.yaml", "", err)
	}
	var economics EconomicsConfig
	if err := decodeRequired(filepath.Join(dir, "economics.yaml"), &economics); err != nil {
		return nil, err
	}
	if err := validateEconomics(economics); err != nil {
		return nil, err
	}
	policy := fixedPolicySnapshot()
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
	promotions, err := validatePromotions(rawPromotions, economics, policy)
	if err != nil {
		return nil, err
	}
	bundle := &Bundle{Economics: economics, Policy: policy, Products: products, Costs: costs, Promotions: promotions}
	if err := validateBundle(bundle); err != nil {
		return nil, err
	}
	return bundle, nil
}

func fixedPolicySnapshot() PolicySnapshot {
	return PolicySnapshot{
		TaskAdmission: TaskAdmissionPolicy{RequireZeroDebt: true, RequireFullPrice: true},
		AcceptedTask:  AcceptedTaskPolicy{ContinueWhenBalanceNegative: true, OperationChargeMayCreateDebt: true},
		TopUp:         TopUpPolicy{RepayDebtFirst: true},
		Promotions:    PromotionsPolicy{MayRepayDebt: false},
		TaskFailureReversal: TaskFailureReversalPolicy{
			Enabled: true,
			Reasons: []string{"platform_error", "provider_error", "execution_timeout", "infrastructure_cancelled"},
		},
	}
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
	bundle.Products.Currency = strings.TrimSpace(bundle.Products.Currency)
	if bundle.Products.Currency != "credits" {
		return configError("products.yaml", "currency", errors.New("must be credits"))
	}
	if len(bundle.Products.TierRatesPercent) != len(RequiredPricingTiers) {
		return configError("products.yaml", "tier_rates_percent", errors.New("must define exactly free, pro, and enterprise"))
	}
	for _, tier := range RequiredPricingTiers {
		rate, exists := bundle.Products.TierRatesPercent[tier]
		if !exists {
			return configError("products.yaml", "tier_rates_percent", fmt.Errorf("missing tier %q", tier))
		}
		if rate < 0 || rate > 100 {
			return configError("products.yaml", "tier_rates_percent."+tier, errors.New("must be between 0 and 100"))
		}
	}
	for tier := range bundle.Products.TierRatesPercent {
		if tier != "free" && tier != "pro" && tier != "enterprise" {
			return configError("products.yaml", "tier_rates_percent", fmt.Errorf("unsupported tier %q", tier))
		}
	}
	if bundle.Products.TierRatesPercent["free"] != 100 {
		return configError("products.yaml", "tier_rates_percent.free", errors.New("must equal 100"))
	}
	if bundle.Products.TierRatesPercent["free"] < bundle.Products.TierRatesPercent["pro"] || bundle.Products.TierRatesPercent["pro"] < bundle.Products.TierRatesPercent["enterprise"] {
		return configError("products.yaml", "tier_rates_percent", errors.New("must satisfy free >= pro >= enterprise"))
	}
	if len(bundle.Products.SKUs) == 0 {
		return configError("products.yaml", "skus", errors.New("must not be empty"))
	}
	seenSKUs := make(map[string]struct{}, len(bundle.Products.SKUs))
	type billableIdentity struct {
		operation        string
		chargePolicy     string
		route            string
		executionProfile string
	}
	seenBillableIdentities := make(map[billableIdentity]string, len(bundle.Products.SKUs))
	for index, sku := range bundle.Products.SKUs {
		field := fmt.Sprintf("skus[%d]", index)
		sku.ID = strings.TrimSpace(sku.ID)
		sku.Operation = strings.TrimSpace(sku.Operation)
		sku.ChargePolicy = strings.TrimSpace(sku.ChargePolicy)
		sku.ExecutionProfile = strings.TrimSpace(sku.ExecutionProfile)
		sku.Route = strings.TrimSpace(sku.Route)
		sku.Delivery = strings.TrimSpace(sku.Delivery)
		bundle.Products.SKUs[index] = sku
		if sku.ID == "" || sku.Operation == "" || sku.ChargePolicy == "" || sku.Delivery == "" {
			return configError("products.yaml", field, errors.New("id, operation, charge_policy, and delivery are required"))
		}
		if hasVersionSuffix(sku.ID) {
			return configError("products.yaml", field+".id", errors.New("must not end in a version suffix"))
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
			if sku.ExecutionProfile != "effective" && sku.ExecutionProfile != "balanced" && sku.ExecutionProfile != "quality" {
				return configError("products.yaml", field+".execution_profile", errors.New("must be effective, balanced, or quality"))
			}
			if sku.Route != "" {
				return configError("products.yaml", field+".route", errors.New("must be empty for task_admission"))
			}
		case "accepted_task_operation", "standalone_operation":
			if sku.ExecutionProfile != "" {
				return configError("products.yaml", field+".execution_profile", errors.New("must be empty for operation SKUs"))
			}
			if sku.Route == "" {
				return configError("products.yaml", field+".route", fmt.Errorf("route is required for %s", sku.ChargePolicy))
			}
		default:
			return configError("products.yaml", field+".charge_policy", fmt.Errorf("unsupported value %q", sku.ChargePolicy))
		}
		identity := billableIdentity{
			operation:        sku.Operation,
			chargePolicy:     sku.ChargePolicy,
			route:            sku.Route,
			executionProfile: sku.ExecutionProfile,
		}
		if existingID, exists := seenBillableIdentities[identity]; exists {
			return configError("products.yaml", field, fmt.Errorf("SKU %q duplicates billable identity of %q", sku.ID, existingID))
		}
		seenBillableIdentities[identity] = sku.ID
	}
	catalogID, err := retailCatalogID(bundle)
	if err != nil {
		return configError("products.yaml", "", err)
	}
	bundle.Products.CatalogID = catalogID
	return nil
}

func validateEconomics(economics EconomicsConfig) error {
	if economics.CreditsPerCNY <= 0 {
		return configError("economics.yaml", "credits_per_cny", errors.New("must be positive"))
	}
	if 1_000_000%economics.CreditsPerCNY != 0 {
		return configError("economics.yaml", "credits_per_cny", errors.New("must divide 1000000 exactly for micro-CNY accounting"))
	}
	return nil
}

func hasVersionSuffix(value string) bool {
	index := strings.LastIndex(value, ".v")
	if index < 0 || index+2 >= len(value) {
		return false
	}
	for _, digit := range value[index+2:] {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func retailCatalogID(bundle *Bundle) (string, error) {
	type retailProductsSnapshot struct {
		Currency         string           `json:"currency"`
		TierRatesPercent map[string]int64 `json:"tier_rates_percent"`
		SKUs             []SKUConfig      `json:"skus"`
	}
	snapshot := struct {
		Products  retailProductsSnapshot `json:"products"`
		Economics EconomicsConfig        `json:"economics"`
		Policy    PolicySnapshot         `json:"policy"`
	}{
		Products: retailProductsSnapshot{
			Currency: bundle.Products.Currency, TierRatesPercent: bundle.Products.TierRatesPercent, SKUs: bundle.Products.SKUs,
		},
		Economics: bundle.Economics,
		Policy:    bundle.Policy,
	}
	return canonicalSnapshotID("retail-sha256-", snapshot)
}

func validateCosts(raw rawCostCatalog) (CostCatalog, error) {
	costs := CostCatalog{
		CurrencyRates: make(map[string]MicroCNY, len(raw.CurrencyRates)),
		Models:        make(map[string]ModelCostConfig, len(raw.Models)),
	}
	for currency, value := range raw.CurrencyRates {
		canonicalCurrency := strings.TrimSpace(currency)
		if canonicalCurrency == "" {
			return costs, configError("costs.yaml", "currency_rates", errors.New("contains an empty currency"))
		}
		if _, exists := costs.CurrencyRates[canonicalCurrency]; exists {
			return costs, configError("costs.yaml", "currency_rates", fmt.Errorf("duplicate canonical currency %q", canonicalCurrency))
		}
		parsed, err := ParseMicroCNY(strings.TrimSpace(string(value)))
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
		effectiveAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(rawModel.EffectiveAt))
		if err != nil {
			return costs, configError("costs.yaml", field+".effective_at", errors.New("effective_at must be RFC3339"))
		}
		pricingType := strings.TrimSpace(rawModel.PricingType)
		model := ModelCostConfig{
			PricingType: pricingType, Currency: canonicalCurrency, Unit: rawModel.Unit,
			OperatorEvidence: strings.TrimSpace(rawModel.OperatorEvidence), EffectiveAt: effectiveAt.UTC(),
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
			{name: "text_input", raw: rawModel.TextInput, value: &model.TextInput},
			{name: "text_cached_input", raw: rawModel.TextCachedInput, value: &model.TextCachedInput},
			{name: "image_input", raw: rawModel.ImageInput, value: &model.ImageInput},
			{name: "image_cached_input", raw: rawModel.ImageCachedInput, value: &model.ImageCachedInput},
			{name: "image_output", raw: rawModel.ImageOutput, value: &model.ImageOutput},
		}
		for _, price := range prices {
			if price.raw == "" {
				continue
			}
			parsed, err := ParseMicroCNY(strings.TrimSpace(string(price.raw)))
			if err != nil {
				return costs, configError("costs.yaml", field+"."+price.name, err)
			}
			*price.value = parsed
		}
		switch pricingType {
		case "token":
			if rawModel.Unit <= 0 || rawModel.Input == "" || rawModel.CacheReadInput == "" || rawModel.CacheCreationInput == "" || rawModel.Output == "" || len(rawModel.Tiers) != 0 || rawModel.TextInput != "" || rawModel.TextCachedInput != "" || rawModel.ImageInput != "" || rawModel.ImageCachedInput != "" || rawModel.ImageOutput != "" {
				return costs, configError("costs.yaml", field, errors.New("token pricing requires positive unit and input, cache_read_input, cache_creation_input, and output prices and forbids tiers"))
			}
			if model.Input <= 0 || model.CacheReadInput <= 0 || model.CacheCreationInput <= 0 || model.Output <= 0 {
				return costs, configError("costs.yaml", field, errors.New("token prices must be positive"))
			}
		case "output_pixel_tier":
			if len(rawModel.Tiers) == 0 || rawModel.Unit != 0 || rawModel.Input != "" || rawModel.Output != "" || rawModel.CacheReadInput != "" || rawModel.CacheCreationInput != "" || rawModel.TextInput != "" || rawModel.TextCachedInput != "" || rawModel.ImageInput != "" || rawModel.ImageCachedInput != "" || rawModel.ImageOutput != "" {
				return costs, configError("costs.yaml", field, errors.New("output_pixel_tier pricing requires tiers and forbids token fields"))
			}
			var previousMaxPixels int64
			for index, rawTier := range rawModel.Tiers {
				parsed, err := ParseMicroCNY(strings.TrimSpace(string(rawTier.Price)))
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
		case "openai_image_usage":
			if rawModel.Unit <= 0 || rawModel.TextInput == "" || rawModel.TextCachedInput == "" || rawModel.ImageInput == "" || rawModel.ImageCachedInput == "" || rawModel.ImageOutput == "" || rawModel.Input != "" || rawModel.CacheReadInput != "" || rawModel.CacheCreationInput != "" || rawModel.Output != "" || len(rawModel.Tiers) != 0 {
				return costs, configError("costs.yaml", field, errors.New("openai_image_usage pricing requires positive unit and all five image usage prices and forbids token and tier fields"))
			}
			if model.TextInput <= 0 || model.TextCachedInput <= 0 || model.ImageInput <= 0 || model.ImageCachedInput <= 0 || model.ImageOutput <= 0 {
				return costs, configError("costs.yaml", field, errors.New("openai image usage prices must be positive"))
			}
		default:
			return costs, configError("costs.yaml", field+".pricing_type", fmt.Errorf("unsupported value %q", pricingType))
		}
		costs.Models[canonicalModelID] = model
	}
	if len(costs.Models) == 0 {
		return costs, configError("costs.yaml", "models", errors.New("must not be empty"))
	}
	catalogID, err := costCatalogID(costs)
	if err != nil {
		return costs, configError("costs.yaml", "", err)
	}
	costs.CatalogID = catalogID
	return costs, nil
}

func validatePromotions(raw rawPromotionCatalog, economics EconomicsConfig, policy PolicySnapshot) (PromotionCatalog, error) {
	promotions := PromotionCatalog{Programs: make([]ReferralProgram, 0, len(raw.Programs))}
	seen := make(map[string]struct{}, len(raw.Programs))
	seenTriggers := make(map[string]struct{}, len(raw.Programs))
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
		if _, exists := seenTriggers[trigger]; exists {
			return promotions, configError("promotions.yaml", field+".trigger", fmt.Errorf("duplicate trigger %q", trigger))
		}
		seenTriggers[trigger] = struct{}{}
		if programID != ReferralFirstTopUpProgramID {
			return promotions, configError("promotions.yaml", field+".id", fmt.Errorf("must equal %q for stable referral history", ReferralFirstTopUpProgramID))
		}
		minimum, err := ParseMicroCNY(strings.TrimSpace(string(rawProgram.MinimumTopUpCNY)))
		if err != nil {
			return promotions, configError("promotions.yaml", field+".minimum_topup_cny", err)
		}
		if minimum <= 0 {
			return promotions, configError("promotions.yaml", field+".minimum_topup_cny", errors.New("must be positive"))
		}
		expiresAfter, err := parseCatalogDuration(strings.TrimSpace(rawProgram.ExpiresAfter))
		if err != nil || expiresAfter <= 0 {
			return promotions, configError("promotions.yaml", field+".expires_after", errors.New("must be a positive duration"))
		}
		if rawProgram.InviterCredits <= 0 {
			return promotions, configError("promotions.yaml", field+".inviter_credits", errors.New("must be positive"))
		}
		if rawProgram.InviteeCredits <= 0 {
			return promotions, configError("promotions.yaml", field+".invitee_credits", errors.New("must be positive"))
		}
		if rawProgram.MaxInviterRewards < 0 {
			return promotions, configError("promotions.yaml", field+".max_inviter_rewards", errors.New("must be non-negative"))
		}
		promotions.Programs = append(promotions.Programs, ReferralProgram{
			ID: programID, Trigger: trigger, MinimumTopUpCNY: minimum,
			InviterCredits: rawProgram.InviterCredits, InviteeCredits: rawProgram.InviteeCredits,
			ExpiresAfter: expiresAfter, MaxInviterRewards: rawProgram.MaxInviterRewards,
		})
	}
	catalogID, err := promotionCatalogID(promotions, economics, policy)
	if err != nil {
		return promotions, configError("promotions.yaml", "", err)
	}
	promotions.CatalogID = catalogID
	return promotions, nil
}

type costTierSnapshot struct {
	MaxPixels int64    `json:"max_pixels"`
	Price     MicroCNY `json:"price_micro_cny"`
}

type costModelSnapshot struct {
	PricingType        string             `json:"pricing_type"`
	Currency           string             `json:"currency"`
	Unit               int64              `json:"unit"`
	Input              MicroCNY           `json:"input_micro_cny"`
	CacheReadInput     MicroCNY           `json:"cache_read_input_micro_cny"`
	CacheCreationInput MicroCNY           `json:"cache_creation_input_micro_cny"`
	Output             MicroCNY           `json:"output_micro_cny"`
	TextInput          MicroCNY           `json:"text_input_micro_cny"`
	TextCachedInput    MicroCNY           `json:"text_cached_input_micro_cny"`
	ImageInput         MicroCNY           `json:"image_input_micro_cny"`
	ImageCachedInput   MicroCNY           `json:"image_cached_input_micro_cny"`
	ImageOutput        MicroCNY           `json:"image_output_micro_cny"`
	Tiers              []costTierSnapshot `json:"tiers"`
	OperatorEvidence   string             `json:"operator_evidence"`
	EffectiveAt        string             `json:"effective_at"`
}

func costCatalogID(costs CostCatalog) (string, error) {
	models := make(map[string]costModelSnapshot, len(costs.Models))
	for modelID, model := range costs.Models {
		tiers := make([]costTierSnapshot, len(model.Tiers))
		for index, tier := range model.Tiers {
			tiers[index] = costTierSnapshot{MaxPixels: tier.MaxPixels, Price: tier.Price}
		}
		models[modelID] = costModelSnapshot{
			PricingType: model.PricingType, Currency: model.Currency, Unit: model.Unit,
			Input: model.Input, CacheReadInput: model.CacheReadInput, CacheCreationInput: model.CacheCreationInput, Output: model.Output,
			TextInput: model.TextInput, TextCachedInput: model.TextCachedInput, ImageInput: model.ImageInput,
			ImageCachedInput: model.ImageCachedInput, ImageOutput: model.ImageOutput, Tiers: tiers,
			OperatorEvidence: model.OperatorEvidence, EffectiveAt: model.EffectiveAt.UTC().Format(time.RFC3339Nano),
		}
	}
	snapshot := struct {
		CurrencyRates map[string]MicroCNY          `json:"currency_rates_micro_cny"`
		Models        map[string]costModelSnapshot `json:"models"`
	}{CurrencyRates: costs.CurrencyRates, Models: models}
	return canonicalSnapshotID("provider-cost-sha256-", snapshot)
}

type promotionProgramSnapshot struct {
	ID                     string   `json:"id"`
	Trigger                string   `json:"trigger"`
	MinimumTopUpMicroCNY   MicroCNY `json:"minimum_top_up_micro_cny"`
	InviterCredits         int64    `json:"inviter_credits"`
	InviteeCredits         int64    `json:"invitee_credits"`
	ExpiresAfterNanosecond int64    `json:"expires_after_nanoseconds"`
	MaxInviterRewards      int64    `json:"max_inviter_rewards"`
}

func promotionCatalogID(promotions PromotionCatalog, economics EconomicsConfig, policy PolicySnapshot) (string, error) {
	programs := make([]promotionProgramSnapshot, len(promotions.Programs))
	for index, program := range promotions.Programs {
		programs[index] = promotionProgramSnapshot{
			ID: program.ID, Trigger: program.Trigger, MinimumTopUpMicroCNY: program.MinimumTopUpCNY,
			InviterCredits: program.InviterCredits, InviteeCredits: program.InviteeCredits,
			ExpiresAfterNanosecond: int64(program.ExpiresAfter), MaxInviterRewards: program.MaxInviterRewards,
		}
	}
	snapshot := struct {
		Programs   []promotionProgramSnapshot `json:"programs"`
		Economics  EconomicsConfig            `json:"economics"`
		Promotions PromotionsPolicy           `json:"promotions_policy"`
	}{Programs: programs, Economics: economics, Promotions: policy.Promotions}
	return canonicalSnapshotID("promotion-sha256-", snapshot)
}

func canonicalSnapshotID(prefix string, snapshot any) (string, error) {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("marshal canonical snapshot: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return prefix + hex.EncodeToString(sum[:]), nil
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
