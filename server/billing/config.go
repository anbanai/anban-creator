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
	EffectiveAt        string
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
	if strings.TrimSpace(bundle.Policy.Version) == "" {
		return configError("policy.yaml", "version", errors.New("is required"))
	}
	if bundle.Policy.CreditsPerCNY <= 0 {
		return configError("policy.yaml", "credits_per_cny", errors.New("must be positive"))
	}
	seenReasons := make(map[string]struct{}, len(bundle.Policy.TaskFailureReversal.Reasons))
	for _, reason := range bundle.Policy.TaskFailureReversal.Reasons {
		if strings.TrimSpace(reason) == "" {
			return configError("policy.yaml", "task_failure_reversal.reasons", errors.New("must not contain an empty reason"))
		}
		if _, exists := seenReasons[reason]; exists {
			return configError("policy.yaml", "task_failure_reversal.reasons", fmt.Errorf("duplicate reason %q", reason))
		}
		seenReasons[reason] = struct{}{}
	}

	if strings.TrimSpace(bundle.Products.CatalogID) == "" {
		return configError("products.yaml", "catalog_id", errors.New("is required"))
	}
	if bundle.Products.Currency != "credits" {
		return configError("products.yaml", "currency", errors.New("must be credits"))
	}
	if len(bundle.Products.SKUs) == 0 {
		return configError("products.yaml", "skus", errors.New("must not be empty"))
	}
	seenSKUs := make(map[string]struct{}, len(bundle.Products.SKUs))
	for index, sku := range bundle.Products.SKUs {
		field := fmt.Sprintf("skus[%d]", index)
		if strings.TrimSpace(sku.ID) == "" || strings.TrimSpace(sku.Operation) == "" || strings.TrimSpace(sku.Delivery) == "" {
			return configError("products.yaml", field, errors.New("id, operation, and delivery are required"))
		}
		if _, exists := seenSKUs[sku.ID]; exists {
			return configError("products.yaml", field+".id", fmt.Errorf("duplicate SKU id %q", sku.ID))
		}
		seenSKUs[sku.ID] = struct{}{}
		if sku.PriceCredits < 0 {
			return configError("products.yaml", field+".price_credits", errors.New("must be non-negative"))
		}
		switch sku.ChargePolicy {
		case "task_admission", "accepted_task_operation", "standalone_operation":
		default:
			return configError("products.yaml", field+".charge_policy", fmt.Errorf("unsupported value %q", sku.ChargePolicy))
		}
	}
	for index, program := range bundle.Promotions.Programs {
		if program.CanRepayDebt && !bundle.Policy.Promotions.MayRepayDebt {
			return configError("promotions.yaml", fmt.Sprintf("programs[%d].can_repay_debt", index), errors.New("contradicts policy.promotions.may_repay_debt"))
		}
	}
	return nil
}

func validateCosts(raw rawCostCatalog) (CostCatalog, error) {
	costs := CostCatalog{
		CatalogID:     raw.CatalogID,
		CurrencyRates: make(map[string]MicroCNY, len(raw.CurrencyRates)),
		Models:        make(map[string]ModelCostConfig, len(raw.Models)),
	}
	if strings.TrimSpace(raw.CatalogID) == "" {
		return costs, configError("costs.yaml", "catalog_id", errors.New("is required"))
	}
	for currency, value := range raw.CurrencyRates {
		if strings.TrimSpace(currency) == "" {
			return costs, configError("costs.yaml", "currency_rates", errors.New("contains an empty currency"))
		}
		parsed, err := ParseMicroCNY(string(value))
		if err != nil || parsed <= 0 {
			if err == nil {
				err = errors.New("must be positive")
			}
			return costs, configError("costs.yaml", "currency_rates."+currency, err)
		}
		costs.CurrencyRates[currency] = parsed
	}
	for modelID, rawModel := range raw.Models {
		field := "models." + modelID
		if strings.TrimSpace(modelID) == "" {
			return costs, configError("costs.yaml", "models", errors.New("contains an empty model id"))
		}
		if _, exists := costs.CurrencyRates[rawModel.Currency]; !exists {
			return costs, configError("costs.yaml", field+".currency", fmt.Errorf("references missing currency rate %q", rawModel.Currency))
		}
		model := ModelCostConfig{
			PricingType: rawModel.PricingType, Currency: rawModel.Currency, Unit: rawModel.Unit,
			OperatorEvidence: rawModel.OperatorEvidence, EffectiveAt: rawModel.EffectiveAt,
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
		switch rawModel.PricingType {
		case "token":
			if rawModel.Unit <= 0 || rawModel.Input == "" || rawModel.Output == "" || len(rawModel.Tiers) != 0 {
				return costs, configError("costs.yaml", field, errors.New("token pricing requires positive unit, input, and output and forbids tiers"))
			}
		case "output_pixel_tier":
			if len(rawModel.Tiers) == 0 || rawModel.Unit != 0 || rawModel.Input != "" || rawModel.Output != "" || rawModel.CacheReadInput != "" || rawModel.CacheCreationInput != "" {
				return costs, configError("costs.yaml", field, errors.New("output_pixel_tier pricing requires tiers and forbids token fields"))
			}
			for index, rawTier := range rawModel.Tiers {
				parsed, err := ParseMicroCNY(string(rawTier.Price))
				if err != nil {
					return costs, configError("costs.yaml", fmt.Sprintf("%s.tiers[%d].price", field, index), err)
				}
				model.Tiers = append(model.Tiers, CostTier{MaxPixels: rawTier.MaxPixels, Price: parsed})
			}
		default:
			return costs, configError("costs.yaml", field+".pricing_type", fmt.Errorf("unsupported value %q", rawModel.PricingType))
		}
		costs.Models[modelID] = model
	}
	if len(costs.Models) == 0 {
		return costs, configError("costs.yaml", "models", errors.New("must not be empty"))
	}
	return costs, nil
}

func validatePromotions(raw rawPromotionCatalog) (PromotionCatalog, error) {
	promotions := PromotionCatalog{CatalogID: raw.CatalogID, Programs: make([]ReferralProgram, 0, len(raw.Programs))}
	if strings.TrimSpace(raw.CatalogID) == "" {
		return promotions, configError("promotions.yaml", "catalog_id", errors.New("is required"))
	}
	seen := make(map[string]struct{}, len(raw.Programs))
	for index, rawProgram := range raw.Programs {
		field := fmt.Sprintf("programs[%d]", index)
		if strings.TrimSpace(rawProgram.ID) == "" {
			return promotions, configError("promotions.yaml", field+".id", errors.New("is required"))
		}
		if _, exists := seen[rawProgram.ID]; exists {
			return promotions, configError("promotions.yaml", field+".id", fmt.Errorf("duplicate referral program id %q", rawProgram.ID))
		}
		seen[rawProgram.ID] = struct{}{}
		if rawProgram.Trigger != "invitee_first_paid_topup" {
			return promotions, configError("promotions.yaml", field+".trigger", fmt.Errorf("unsupported value %q", rawProgram.Trigger))
		}
		minimum, err := ParseMicroCNY(string(rawProgram.MinimumTopUpCNY))
		if err != nil {
			return promotions, configError("promotions.yaml", field+".minimum_topup_cny", err)
		}
		expiresAfter, err := parseCatalogDuration(rawProgram.ExpiresAfter)
		if err != nil || expiresAfter <= 0 {
			return promotions, configError("promotions.yaml", field+".expires_after", errors.New("must be a positive duration"))
		}
		if rawProgram.InviterCredits < 0 || rawProgram.InviteeCredits < 0 || rawProgram.MaxInviterRewards < 0 {
			return promotions, configError("promotions.yaml", field, errors.New("credit values and max_inviter_rewards must be non-negative"))
		}
		promotions.Programs = append(promotions.Programs, ReferralProgram{
			ID: rawProgram.ID, Trigger: rawProgram.Trigger, MinimumTopUpCNY: minimum,
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
