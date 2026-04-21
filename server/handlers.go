package main

import (
	"net/http"
	"os"

	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/handler"
	"github.com/royalrick/anbanwriter/server/mcp"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/service"
)

// handlerInstances holds all HTTP handler instances.
type handlerInstances struct {
	authHandler     *handler.AuthHandler
	planHandler     *handler.PlanHandler
	taskHandler     *handler.TaskHandler
	agentHandler    *handler.AgentHandler
	channelHandler  *handler.ChannelHandler
	timelineHandler *handler.TimelineHandler
	creditHandler   *handler.CreditHandler
	apiKeyHandler   *handler.APIKeyHandler
	fileHandler     *handler.FileHandler
}

// setupHandlers creates all HTTP handler instances.
func setupHandlers(cfg *config.Config, core *coreServices, repo repository.Repository, log *zerolog.Logger) *handlerInstances {
	h := &handlerInstances{}

	if repo != nil {
		h.authHandler = handler.NewAuthHandler(core.jwtSvc, core.wechatSvc, repo, core.emailSvc, log, core.wsHub, cfg.Invitation.Enabled, cfg.Invitation.MaxPerUser)
		h.planHandler = handler.NewPlanHandler(core.planSvc, log)
		// Pass local dataDir so ServeLocalFile can serve files from disk.
		h.taskHandler = handler.NewTaskHandler(core.taskSvc, log, cfg.Storage.LocalDataDir)
		h.channelHandler = handler.NewChannelHandler(core.channelSvc, log)
		h.timelineHandler = handler.NewTimelineHandler(repo, log)
		if core.creditSvc != nil {
			h.creditHandler = handler.NewCreditHandler(core.creditSvc, cfg.Credits.AdminAPIKey, log)
		}
		if core.apiKeySvc != nil {
			h.apiKeyHandler = handler.NewAPIKeyHandler(core.apiKeySvc, log)
		}
		h.agentHandler = handler.NewAgentHandler(core.taskSvc, core.apiKeySvc, core.store, cfg.MCP.APIKey, log)
		if core.store != nil {
			h.fileHandler = handler.NewFileHandler(core.store, log)
		}
	}

	return h
}

// setupMCPHandler creates the MCP HTTP handler (using official MCP Go SDK).
func setupMCPHandler(cfg *config.Config, core *coreServices, repo repository.Repository, log *zerolog.Logger) http.Handler {
	if core.channelSvc != nil && core.taskSvc != nil && core.creditSvc != nil && core.planSvc != nil {
		// Create AI operation services for MCP tools.
		var imageSvc *service.ImageService
		var writingSvc *service.WritingService
		var publishingSvc *service.PublishingService

		if core.store != nil {
			imageSvc = service.NewImageService(&cfg.ImageAPI, core.store, repo, core.creditSvc, cfg.Claude.Docker.MCPBaseURL, cfg.Claude.Docker.WorkspaceDir, log)
		}
		if repo != nil && core.creditSvc != nil {
			// Create LLM client for writing operations.
			// Prefer config.yaml writing section; fall back to env vars.
			llmBaseURL := cfg.Writing.BaseURL
			llmAPIKey := cfg.Writing.Key
			llmModel := cfg.Writing.Model
			if llmBaseURL == "" {
				llmBaseURL = os.Getenv("ANTHROPIC_BASE_URL")
			}
			if llmAPIKey == "" {
				llmAPIKey = os.Getenv("ANTHROPIC_AUTH_TOKEN")
			}
			if llmModel == "" {
				llmModel = cfg.Claude.Model
				if llmModel == "" {
					llmModel = os.Getenv("ANTHROPIC_MODEL")
				}
			}
			if llmBaseURL != "" && llmAPIKey != "" && llmModel != "" {
				llmClient := service.NewOpenAILLMClient(llmBaseURL, llmAPIKey, llmModel)
				writingSvc = service.NewWritingService(repo, core.creditSvc, llmClient, log)
			} else {
				log.Warn().Msg("LLM client not configured (missing writing config or ANTHROPIC_BASE_URL/AUTH_TOKEN/MODEL), writing tools unavailable")
			}
			publishingSvc = service.NewPublishingService(repo, core.creditSvc, log)
		}

		mcp.SetServices(&mcp.Services{
			ChannelSvc:    core.channelSvc,
			TaskSvc:       core.taskSvc,
			CreditSvc:     core.creditSvc,
			PlanSvc:       core.planSvc,
			ImageSvc:      imageSvc,
			WritingSvc:    writingSvc,
			PublishingSvc: publishingSvc,
			WorkspaceSvc:  core.workspaceSvc,
		})

		log.Info().
			Bool("mcp_static_key_set", cfg.MCP.APIKey != "").
			Bool("image_tools", imageSvc != nil).
			Bool("writing_tools", writingSvc != nil).
			Bool("publishing_tools", publishingSvc != nil).
			Msg("MCP handler initialized with tools (official SDK)")
	} else {
		log.Info().Msg("MCP handler initialized (no tools, services unavailable)")
	}

	return mcp.NewMCPHandler(core.apiKeySvc, cfg.MCP.APIKey, log)
}
