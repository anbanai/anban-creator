package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/seednote"
	"github.com/anbanai/anban-creator/server/service"
)

func registerSeednoteTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "search_seednote_feeds",
		Description: "搜索种草笔记内容（需要已登录）。按关键词搜索笔记，返回包含互动数据的笔记列表。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"keyword":      map[string]any{"type": "string", "description": "搜索关键词"},
				"sort_by":      map[string]any{"type": "string", "description": "排序依据: 综合|最新|最多点赞|最多评论|最多收藏"},
				"note_type":    map[string]any{"type": "string", "description": "笔记类型: 不限|视频|图文"},
				"publish_time": map[string]any{"type": "string", "description": "发布时间: 不限|一天内|一周内|半年内"},
			},
			"required": []any{"keyword"},
		},
	}, searchSeednoteFeedsHandler)

	server.AddTool(&mcp.Tool{
		Name:        "get_seednote_feed_detail",
		Description: "获取种草笔记详情和评论。通过 feed_id 获取笔记的完整内容、图片和评论。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"feed_id":           map[string]any{"type": "string", "description": "笔记ID"},
				"xsec_token":        map[string]any{"type": "string", "description": "安全令牌"},
				"load_all_comments": map[string]any{"type": "boolean", "description": "是否加载所有评论"},
			},
			"required": []any{"feed_id", "xsec_token"},
		},
	}, getSeednoteFeedDetailHandler)

	server.AddTool(&mcp.Tool{
		Name:        "get_seednote_user_profile",
		Description: "获取种草笔记用户公开资料，包含用户信息、互动数据和笔记列表。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"user_id":    map[string]any{"type": "string", "description": "用户ID"},
				"xsec_token": map[string]any{"type": "string", "description": "安全令牌"},
			},
			"required": []any{"user_id", "xsec_token"},
		},
	}, getSeednoteUserProfileHandler)
}

func seednoteUnavailable() (*mcp.CallToolResult, error) {
	return textResult(map[string]any{
		"available": false,
		"message":   "Seednote sidecar 未配置或不可用",
	})
}

func seednoteUnavailableMessage(message string) (*mcp.CallToolResult, error) {
	return textResult(map[string]any{"available": false, "message": message})
}

func searchSeednoteFeedsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.SeednoteCapabilitySvc == nil {
		return seednoteUnavailable()
	}
	args := parseArgs(req.Params.Arguments)
	result, err := svcs.SeednoteCapabilitySvc.SearchFeeds(ctx, service.SeednoteSearchFeedsRequest{
		Keyword: stringArg(args, "keyword"), SortBy: stringArg(args, "sort_by"),
		NoteType: stringArg(args, "note_type"), PublishTime: stringArg(args, "publish_time"),
	})
	if err != nil {
		if err.Error() == "keyword is required" {
			return errorResult(err.Error()), nil
		}
		return nil, fmt.Errorf("搜索失败: %w", err)
	}
	if !result.Available {
		return seednoteUnavailableMessage(result.Message)
	}

	feeds := make([]map[string]any, 0, len(result.Value))
	for _, f := range result.Value {
		item := map[string]any{
			"id": f.ID, "title": f.NoteCard.DisplayTitle,
			"author": f.NoteCard.User.Nickname, "author_id": f.NoteCard.User.UserID,
			"like_count":    f.NoteCard.InteractInfo.LikedCount,
			"collect_count": f.NoteCard.InteractInfo.CollectedCount,
			"comment_count": f.NoteCard.InteractInfo.CommentCount,
			"share_count":   f.NoteCard.InteractInfo.SharedCount,
			"type":          f.NoteCard.Type, "xsec_token": f.XsecToken,
		}
		if f.NoteCard.Cover.URLDefault != "" {
			item["cover_url"] = f.NoteCard.Cover.URLDefault
		}
		feeds = append(feeds, item)
	}
	return textResult(feeds)
}

func getSeednoteFeedDetailHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.SeednoteCapabilitySvc == nil {
		return seednoteUnavailable()
	}
	args := parseArgs(req.Params.Arguments)
	loadAll, _ := args["load_all_comments"].(bool)
	result, err := svcs.SeednoteCapabilitySvc.FeedDetail(ctx, service.SeednoteFeedDetailRequest{
		FeedID: stringArg(args, "feed_id"), XsecToken: stringArg(args, "xsec_token"), LoadAllComments: loadAll,
	})
	if err != nil {
		if err.Error() == "feed_id is required" {
			return errorResult(err.Error()), nil
		}
		return nil, fmt.Errorf("获取笔记详情失败: %w", err)
	}
	if !result.Available {
		return seednoteUnavailableMessage(result.Message)
	}
	detail := result.Value

	note := map[string]any{
		"note_id": detail.Note.NoteID, "title": detail.Note.Title,
		"desc": detail.Note.Desc, "type": detail.Note.Type,
		"author": detail.Note.User.Nickname, "author_id": detail.Note.User.UserID,
		"like_count":    detail.Note.InteractInfo.LikedCount,
		"collect_count": detail.Note.InteractInfo.CollectedCount,
		"comment_count": detail.Note.InteractInfo.CommentCount,
		"share_count":   detail.Note.InteractInfo.SharedCount,
		"image_urls":    extractDetailImageURLs(detail.Note.ImageList),
	}

	payload := map[string]any{
		"note":              note,
		"comments_count":    len(detail.Comments.List),
		"has_more_comments": detail.Comments.HasMore,
	}

	if len(detail.Comments.List) > 0 {
		comments := make([]map[string]any, 0, len(detail.Comments.List))
		for _, c := range detail.Comments.List {
			comments = append(comments, map[string]any{
				"id": c.ID, "content": c.Content,
				"like_count": c.LikeCount, "author": c.UserInfo.Nickname,
				"sub_comments": c.SubCommentCount,
			})
		}
		payload["comments"] = comments
	}
	return textResult(payload)
}

func getSeednoteUserProfileHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.SeednoteCapabilitySvc == nil {
		return seednoteUnavailable()
	}
	args := parseArgs(req.Params.Arguments)
	result, err := svcs.SeednoteCapabilitySvc.UserProfile(ctx, service.SeednoteUserProfileRequest{
		UserID: stringArg(args, "user_id"), XsecToken: stringArg(args, "xsec_token"),
	})
	if err != nil {
		if err.Error() == "user_id is required" {
			return errorResult(err.Error()), nil
		}
		return nil, fmt.Errorf("获取用户资料失败: %w", err)
	}
	if !result.Available {
		return seednoteUnavailableMessage(result.Message)
	}
	profile := result.Value

	interactions := map[string]string{}
	for _, i := range profile.Interactions {
		interactions[i.Type] = i.Count
	}

	feeds := make([]map[string]any, 0, len(profile.Feeds))
	for _, f := range profile.Feeds {
		feed := map[string]any{
			"id": f.ID, "title": f.NoteCard.DisplayTitle,
			"author":        f.NoteCard.User.Nickname,
			"like_count":    f.NoteCard.InteractInfo.LikedCount,
			"collect_count": f.NoteCard.InteractInfo.CollectedCount,
			"comment_count": f.NoteCard.InteractInfo.CommentCount,
			"xsec_token":    f.XsecToken,
		}
		if f.NoteCard.Cover.URLDefault != "" {
			feed["cover_url"] = f.NoteCard.Cover.URLDefault
		}
		feeds = append(feeds, feed)
	}

	return textResult(map[string]any{
		"nickname":     profile.UserBasicInfo.Nickname,
		"red_id":       profile.UserBasicInfo.RedID,
		"desc":         profile.UserBasicInfo.Desc,
		"avatar_url":   profile.UserBasicInfo.Avatar,
		"ip_location":  profile.UserBasicInfo.IPLocation,
		"interactions": interactions,
		"feeds_count":  len(feeds),
		"feeds":        feeds,
	})
}

func extractDetailImageURLs(images []seednote.DetailImage) []string {
	urls := make([]string, 0, len(images))
	for _, img := range images {
		if img.URLDefault != "" {
			urls = append(urls, img.URLDefault)
		}
	}
	return urls
}
