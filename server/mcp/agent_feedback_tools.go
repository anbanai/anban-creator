package mcp

import (
	"context"
	"fmt"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerAgentFeedbackTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "get_feedback_context",
		Description: "Read the frozen, execution-scoped feedback context for the current Feedback Job.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"task_id": map[string]any{"type": "string"}}, "required": []any{"task_id"}},
	}, feedbackContextHandler)
	server.AddTool(&mcp.Tool{
		Name:        "submit_agent_feedback",
		Description: "Submit post-execution feedback from an agent including scores, errors, and optimization suggestions.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id":       map[string]any{"type": "string", "description": "Task ID"},
				"agent_name":    map[string]any{"type": "string", "description": "Agent name (wechat-article, wechat-picture, ecommerce, live-slicer, moments, montage, hypit, or seednote)"},
				"scores":        map[string]any{"type": "string", "description": "JSON object with score dimensions, e.g. {\"quality\":8,\"completeness\":9,\"efficiency\":7}"},
				"errors":        map[string]any{"type": "string", "description": "Errors encountered during execution (optional)"},
				"optimizations": map[string]any{"type": "string", "description": "Optimization suggestions for future runs (optional)"},
				"summary":       map[string]any{"type": "string", "description": "Brief execution summary (optional)"},
			},
			"required": []any{"task_id", "agent_name"},
		},
	}, agentFeedbackSubmitHandler)
}

func feedbackContextHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return errorResult("task_id is required"), nil
	}
	if failure := requireMCPExecutionIdentity(ctx, "get_feedback_context", "", taskID, ""); failure != nil {
		return failure, nil
	}
	if svcs.TaskSvc == nil || svcs.TaskSvc.Repository() == nil {
		return errorResult("feedback context unavailable"), nil
	}
	repo := svcs.TaskSvc.Repository()
	task, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return errorResult("task not found"), nil
	}
	job, err := repo.FeedbackLoop().FindJobByIDOrFingerprint(ctx, taskID)
	if err != nil || job == nil {
		jobs, listErr := repo.FeedbackLoop().ListJobs(ctx, task.ProjectID, "", 200)
		if listErr != nil {
			return errorResult("feedback job not found"), nil
		}
		for _, candidate := range jobs {
			if candidate != nil && candidate.TaskID == taskID {
				job = candidate
				break
			}
		}
	}
	if job == nil {
		return errorResult("feedback job not found"), nil
	}
	identity, _ := getMCPExecutionIdentity(ctx)
	currentExecution, err := repo.TaskExecutions().FindCurrentByTaskID(ctx, taskID)
	if err != nil || currentExecution == nil || currentExecution.ID != identity.ExecutionID || job.ExecutionID != identity.ExecutionID {
		return errorResult("feedback context is not bound to the current execution"), nil
	}
	db := repo.Analytics().DB().WithContext(ctx)
	var content model.AnalyticsContent
	if job.TargetContentID != "" {
		if err := db.Where("id = ? AND project_id = ?", job.TargetContentID, job.ProjectID).First(&content).Error; err != nil {
			return errorResult("feedback target content unavailable"), nil
		}
	}
	var observations []model.AnalyticsObservation
	if job.TargetContentID != "" {
		if err := db.Where("project_id = ? AND content_id = ? AND revoked_at IS NULL", job.ProjectID, job.TargetContentID).Order("stat_date asc").Find(&observations).Error; err != nil {
			return errorResult("feedback observations unavailable"), nil
		}
	}
	insights, err := repo.FeedbackLoop().ListInsightsByTarget(ctx, job.ProjectID, job.TargetContentID)
	if err != nil {
		return errorResult("feedback insights unavailable"), nil
	}
	var strategy *model.StrategySnapshot
	if job.StrategyRevision > 0 {
		var snapshot model.StrategySnapshot
		platforms := []string{job.Platform}
		switch job.Platform {
		case model.ChannelArticle, model.PlatformWechat:
			platforms = append(platforms, model.ChannelArticle, model.TaskTypeWechatArticle, model.PlatformWechat)
		case model.ChannelSeednote:
			platforms = append(platforms, model.PlatformSeednote)
		case model.ChannelWechatPicture:
			platforms = append(platforms, model.TaskTypeWechatPicture)
		}
		if err := db.Where("project_id = ? AND platform IN ? AND revision = ?", job.ProjectID, platforms, job.StrategyRevision).First(&snapshot).Error; err != nil {
			return errorResult("frozen feedback strategy unavailable"), nil
		}
		strategy = &snapshot
	}
	var baselineContents []model.AnalyticsContent
	if job.TargetContentID != "" {
		q := db.Where("project_id = ? AND channel = ? AND content_type = ? AND id <> ?", job.ProjectID, content.Channel, content.ContentType, content.ID)
		if content.Date != nil {
			q = q.Where("date < ?", *content.Date)
		}
		if err := q.Order("date desc").Limit(100).Find(&baselineContents).Error; err != nil {
			return errorResult("feedback baseline content unavailable"), nil
		}
	} else {
		q := db.Where("project_id = ? AND (channel = ? OR platform = ?)", job.ProjectID, job.Platform, job.Platform)
		if err := q.Order("date desc").Limit(500).Find(&baselineContents).Error; err != nil {
			return errorResult("feedback content set unavailable"), nil
		}
	}
	baselineIDs := make([]string, 0, len(baselineContents))
	for _, item := range baselineContents {
		baselineIDs = append(baselineIDs, item.ID)
	}
	var baselineObservations []model.AnalyticsObservation
	if err := db.Where("project_id = ? AND revoked_at IS NULL AND (content_id = '' OR content_id IN ?)", job.ProjectID, baselineIDs).Order("stat_date asc").Find(&baselineObservations).Error; err != nil {
		return errorResult("feedback baseline observations unavailable"), nil
	}
	result, err := textResult(map[string]any{"job": job, "target_content": content, "observations": observations, "baseline_scope": "account_platform_content_type", "baseline_contents": baselineContents, "baseline_observations": baselineObservations, "insights": insights, "strategy": strategy, "analytics_revision": job.AnalyticsRevision})
	return result, err
}

func agentFeedbackSubmitHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	agentName, _ := args["agent_name"].(string)
	scores, _ := args["scores"].(string)
	errors, _ := args["errors"].(string)
	optimizations, _ := args["optimizations"].(string)
	summary, _ := args["summary"].(string)

	if taskID == "" {
		return errorResult("task_id is required"), nil
	}
	if agentName == "" {
		return errorResult("agent_name is required"), nil
	}
	if failure := requireMCPExecutionIdentity(ctx, "submit_agent_feedback", "", taskID, ""); failure != nil {
		return failure, nil
	}
	if svcs.AgentFeedbackSvc == nil {
		return errorResult("agent feedback service not available"), nil
	}

	feedback, err := svcs.AgentFeedbackSvc.Create(ctx, taskID, agentName, scores, errors, optimizations, summary)
	if err != nil {
		return errorResult(fmt.Sprintf("submit feedback: %v", err)), nil
	}
	return textResult(map[string]any{"id": feedback.ID, "task_id": taskID, "submitted": true})
}
