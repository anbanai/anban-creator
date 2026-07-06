package handler

import (
	"crypto/subtle"
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/service"
)

// CreditHandler handles credit-related HTTP endpoints.
type CreditHandler struct {
	service     *service.CreditService
	cfg         *config.CreditsConfig
	fullCfg     *config.Config
	imageCfg    *config.ImageAPIConfig
	adminAPIKey string
	logger      *zerolog.Logger
}

// NewCreditHandler creates a new CreditHandler.
func NewCreditHandler(svc *service.CreditService, cfg *config.Config, adminAPIKey string, logger *zerolog.Logger) *CreditHandler {
	var credits *config.CreditsConfig
	var imageCfg *config.ImageAPIConfig
	if cfg != nil {
		credits = &cfg.Credits
		imageCfg = &cfg.ImageAPI
	}
	return &CreditHandler{service: svc, cfg: credits, fullCfg: cfg, imageCfg: imageCfg, adminAPIKey: adminAPIKey, logger: logger}
}

// Balance handles GET /api/v1/credits/balance.
func (h *CreditHandler) Balance(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	balance, err := h.service.GetBalance(c.Context(), userID)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("get balance failed")
		return Error(c, fiber.StatusInternalServerError, "failed to get balance")
	}

	return Success(c, fiber.Map{"balance": balance})
}

// SignInStatus handles GET /api/v1/credits/sign-in/status.
func (h *CreditHandler) SignInStatus(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	signedIn, err := h.service.GetSignInStatus(c.Context(), userID)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("get sign-in status failed")
		return Error(c, fiber.StatusInternalServerError, "failed to get sign-in status")
	}

	return Success(c, fiber.Map{"signed_in_today": signedIn})
}

// SignIn handles POST /api/v1/credits/sign-in.
func (h *CreditHandler) SignIn(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	balance, err := h.service.SignIn(c.Context(), userID)
	if err != nil {
		if errors.Is(err, service.ErrAlreadySignedIn) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"code": 40900,
				"msg":  "already_signed_in",
			})
		}
		h.logger.Error().Err(err).Str("user_id", userID).Msg("sign-in failed")
		return Error(c, fiber.StatusInternalServerError, "sign-in failed")
	}

	return Success(c, fiber.Map{"balance": balance})
}

// Transactions handles GET /api/v1/credits/transactions.
func (h *CreditHandler) Transactions(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	page, _ := strconv.Atoi(c.Query("page", "1"))
	pageSize, _ := strconv.Atoi(c.Query("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	offset := (page - 1) * pageSize
	transactions, total, err := h.service.ListTransactions(c.Context(), userID, offset, pageSize)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("list transactions failed")
		return Error(c, fiber.StatusInternalServerError, "failed to list transactions")
	}

	return Success(c, fiber.Map{
		"items": transactions,
		"total": total,
	})
}

// AdminGrant handles POST /api/v1/admin/credits/grant.
// This endpoint is authenticated via X-Admin-API-Key header (not JWT).
func (h *CreditHandler) AdminGrant(c fiber.Ctx) error {
	// Validate admin API key.
	apiKey := c.Get("X-Admin-API-Key")
	if apiKey == "" {
		apiKey = c.Get("Authorization")
		if len(apiKey) > 7 && apiKey[:7] == "Bearer " {
			apiKey = apiKey[7:]
		}
	}
	if h.adminAPIKey == "" || subtle.ConstantTimeCompare([]byte(apiKey), []byte(h.adminAPIKey)) != 1 {
		return Error(c, fiber.StatusUnauthorized, "invalid admin api key")
	}

	var req struct {
		UserID      string `json:"user_id"`
		Amount      int    `json:"amount"`
		Description string `json:"description"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if req.UserID == "" {
		return Error(c, fiber.StatusBadRequest, "user_id is required")
	}
	if req.Amount <= 0 {
		return Error(c, fiber.StatusBadRequest, "amount must be positive")
	}
	if req.Description == "" {
		req.Description = "管理员充值"
	}

	if err := h.service.AdminGrant(c.Context(), req.UserID, req.Amount, req.Description); err != nil {
		h.logger.Error().Err(err).Str("user_id", req.UserID).Msg("admin grant failed")
		return Error(c, fiber.StatusInternalServerError, "failed to grant credits")
	}

	return Success(c, fiber.Map{"granted": true})
}

// Pricing handles GET /api/v1/credits/pricing.
func (h *CreditHandler) Pricing(c fiber.Ctx) error {
	modelCosts := h.service.ModelCosts()
	var modelPrices config.ModelPricesConfig
	var billing config.BillingConfig
	var rechargeTiers []config.RechargeTierConfig
	if h.fullCfg != nil {
		modelPrices = h.fullCfg.ModelPrices
		billing = h.fullCfg.Billing
		rechargeTiers = h.fullCfg.EnabledRechargeTiers()
	}
	income := fiber.Map{}
	if h.cfg != nil {
		income = fiber.Map{
			"daily_sign_in":  h.cfg.DailySignIn,
			"register_bonus": h.cfg.RegisterBonus,
			"invite_reward":  h.cfg.InviteReward,
		}
	}

	// Synthesize only legacy fixed image_gen pricing from ImageAPI config
	// entries. Dynamic providers such as GPT Image 2 are represented by
	// model_prices.image_generation and must not be exposed as a fixed unit cost.
	imageGenCosts := map[string]int{}
	if h.imageCfg != nil {
		if h.imageCfg.Cover != nil && h.imageCfg.Cover.Credits > 0 && !h.isDynamicImageRoute("image_generation.cover", h.imageCfg.Cover.Model) {
			imageGenCosts[h.imageCfg.Cover.Provider+"/"+h.imageCfg.Cover.Model] = h.imageCfg.Cover.Credits
		}
		if h.imageCfg.Content != nil && h.imageCfg.Content.Credits > 0 && !h.isDynamicImageRoute("image_generation.content", h.imageCfg.Content.Model) {
			key := h.imageCfg.Content.Provider + "/" + h.imageCfg.Content.Model
			imageGenCosts[key] = h.imageCfg.Content.Credits
		}
		for id, d := range h.imageCfg.Designer {
			if d != nil && d.Credits > 0 && !h.isDynamicImageRoute("image_generation.designer."+id, d.Model) {
				imageGenCosts[d.Provider+"/"+d.Model] = d.Credits
			}
		}
	}
	if len(imageGenCosts) > 0 {
		if modelCosts == nil {
			modelCosts = map[string]map[string]int{}
		}
		modelCosts["image_gen"] = imageGenCosts
	}

	return Success(c, fiber.Map{
		"task_costs":              h.service.TaskCosts(),
		"model_costs":             modelCosts,
		"model_prices":            modelPrices,
		"billing":                 billing,
		"recharge_tiers":          rechargeTiers,
		"ecommerce_module_prices": h.service.EcommerceModulePrices(),
		"income":                  income,
	})
}

func (h *CreditHandler) isDynamicImageRoute(routeName, fallbackModel string) bool {
	if h.fullCfg == nil {
		return false
	}
	route, ok := h.creditImageGenerationRoute(routeName)
	if !ok {
		return false
	}
	modelName := route.Model
	if modelName == "" {
		modelName = fallbackModel
	}
	if route.Provider == "" || modelName == "" {
		return false
	}
	price, ok := h.fullCfg.ModelPrices.ImageGeneration[route.Provider+"/"+modelName]
	if !ok {
		return false
	}
	return price.PricingType == config.ImagePricingTypeOpenAIUsage
}

func (h *CreditHandler) creditImageGenerationRoute(routeName string) (config.ImageGenerationRouteConfig, bool) {
	if h.fullCfg == nil {
		return config.ImageGenerationRouteConfig{}, false
	}
	switch routeName {
	case "image_generation.cover":
		return h.fullCfg.ModelRoutes.ImageGeneration.Cover, true
	case "image_generation.content":
		return h.fullCfg.ModelRoutes.ImageGeneration.Content, true
	default:
		const prefix = "image_generation.designer."
		if len(routeName) <= len(prefix) || routeName[:len(prefix)] != prefix {
			return config.ImageGenerationRouteConfig{}, false
		}
		route, ok := h.fullCfg.ModelRoutes.ImageGeneration.Designer[routeName[len(prefix):]]
		return route, ok
	}
}
