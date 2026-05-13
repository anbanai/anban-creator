package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/royalrick/anbanwriter/server/xhs"
)

func registerXHSTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "search_xhs_feeds",
		Description: "搜索小红书内容（需要已登录）。按关键词搜索笔记，返回包含互动数据的笔记列表。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"keyword": map[string]any{"type": "string", "description": "搜索关键词"},
				"sort_by": map[string]any{"type": "string", "description": "排序依据: 综合|最新|最多点赞|最多评论|最多收藏"},
				"note_type": map[string]any{"type": "string", "description": "笔记类型: 不限|视频|图文"},
				"publish_time": map[string]any{"type": "string", "description": "发布时间: 不限|一天内|一周内|半年内"},
			},
			"required": []any{"keyword"},
		},
	}, searchXHSFeedsHandler)

	server.AddTool(&mcp.Tool{
		Name:        "get_xhs_feed_detail",
		Description: "获取小红书笔记详情和评论。通过 feed_id 获取笔记的完整内容、图片和评论。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"feed_id": map[string]any{"type": "string", "description": "笔记ID"},
				"xsec_token": map[string]any{"type": "string", "description": "安全令牌"},
				"load_all_comments": map[string]any{"type": "boolean", "description": "是否加载所有评论"},
			},
			"required": []any{"feed_id", "xsec_token"},
		},
	}, getXHSFeedDetailHandler)

	server.AddTool(&mcp.Tool{
		Name:        "check_xhs_login_status",
		Description: "检查小红书 sidecar 是否已登录。返回登录状态信息。",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, checkXHSLoginStatusHandler)

	server.AddTool(&mcp.Tool{
		Name:        "get_xhs_login_qrcode",
		Description: "获取小红书登录二维码。返回 base64 编码的 PNG 图片，用手机小红书 App 扫描登录。",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, getXHSLoginQRCodeHandler)

	server.AddTool(&mcp.Tool{
		Name:        "get_xhs_user_profile",
		Description: "获取小红书用户公开资料，包含用户信息、互动数据和笔记列表。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"user_id":    map[string]any{"type": "string", "description": "用户ID"},
				"xsec_token": map[string]any{"type": "string", "description": "安全令牌"},
			},
			"required": []any{"user_id", "xsec_token"},
		},
	}, getXHSUserProfileHandler)
}

func xhsUnavailable() (*mcp.CallToolResult, error) {
	return textResult(map[string]any{
		"available": false,
		"message":  "XHS sidecar 未配置或不可用",
	})
}

func searchXHSFeedsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.XHSClient == nil {
		return xhsUnavailable()
	}

	args := parseArgs(req.Params.Arguments)
	keyword, _ := args["keyword"].(string)
	if keyword == "" {
		return errorResult("keyword is required"), nil
	}

	searchReq := &xhs.SearchRequest{Keyword: keyword}
	if v, _ := args["sort_by"].(string); v != "" {
		searchReq.Filters.SortBy = v
	}
	if v, _ := args["note_type"].(string); v != "" {
		searchReq.Filters.NoteType = v
	}
	if v, _ := args["publish_time"].(string); v != "" {
		searchReq.Filters.PublishTime = v
	}

	feeds, err := svcs.XHSClient.SearchFeeds(ctx, searchReq)
	if err != nil {
		return nil, fmt.Errorf("搜索失败: %w", err)
	}

	result := make([]map[string]any, 0, len(feeds))
	for _, f := range feeds {
		item := map[string]any{
			"id": f.ID, "title": f.NoteCard.DisplayTitle,
			"author": f.NoteCard.User.Nickname, "author_id": f.NoteCard.User.UserID,
			"like_count": f.NoteCard.InteractInfo.LikedCount,
			"collect_count": f.NoteCard.InteractInfo.CollectedCount,
			"comment_count": f.NoteCard.InteractInfo.CommentCount,
			"share_count": f.NoteCard.InteractInfo.SharedCount,
			"type": f.NoteCard.Type, "xsec_token": f.XsecToken,
		}
		if f.NoteCard.Cover.URLDefault != "" {
			item["cover_url"] = f.NoteCard.Cover.URLDefault
		}
		result = append(result, item)
	}
	return textResult(result)
}

func getXHSFeedDetailHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.XHSClient == nil {
		return xhsUnavailable()
	}

	args := parseArgs(req.Params.Arguments)
	feedID, _ := args["feed_id"].(string)
	xsecToken, _ := args["xsec_token"].(string)
	loadAll, _ := args["load_all_comments"].(bool)

	if feedID == "" {
		return errorResult("feed_id is required"), nil
	}

	detailReq := &xhs.FeedDetailRequest{
		FeedID: feedID, XsecToken: xsecToken,
	}
	if loadAll {
		detailReq.LoadAllComments = true
	}

	detail, err := svcs.XHSClient.GetFeedDetail(ctx, detailReq)
	if err != nil {
		return nil, fmt.Errorf("获取笔记详情失败: %w", err)
	}

	note := map[string]any{
		"note_id": detail.Note.NoteID, "title": detail.Note.Title,
		"desc": detail.Note.Desc, "type": detail.Note.Type,
		"author": detail.Note.User.Nickname, "author_id": detail.Note.User.UserID,
		"like_count": detail.Note.InteractInfo.LikedCount,
		"collect_count": detail.Note.InteractInfo.CollectedCount,
		"comment_count": detail.Note.InteractInfo.CommentCount,
		"share_count": detail.Note.InteractInfo.SharedCount,
		"image_urls": extractDetailImageURLs(detail.Note.ImageList),
	}

	result := map[string]any{
		"note":               note,
		"comments_count":     len(detail.Comments.List),
		"has_more_comments":  detail.Comments.HasMore,
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
		result["comments"] = comments
	}
	return textResult(result)
}

func checkXHSLoginStatusHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.XHSClient == nil {
		return textResult(map[string]any{"available": false, "logged_in": false, "message": "XHS sidecar 未配置"})
	}

	loggedIn, err := svcs.XHSClient.CheckLoginStatus(ctx)
	if err != nil {
		return textResult(map[string]any{"available": true, "logged_in": false, "message": err.Error()})
	}

	msg := "已登录"
	if !loggedIn {
		msg = "未登录，请使用 get_xhs_login_qrcode 获取二维码扫描登录"
	}
	return textResult(map[string]any{"available": true, "logged_in": loggedIn, "message": msg})
}

func getXHSLoginQRCodeHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.XHSClient == nil {
		return xhsUnavailable()
	}

	qrBase64, err := svcs.XHSClient.GetLoginQRCode(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取二维码失败: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.ImageContent{Data: []byte(qrBase64), MIMEType: "image/png"},
			&mcp.TextContent{Text: "请用小红书 App 扫描二维码登录。登录后使用 check_xhs_login_status 确认状态。"},
		},
	}, nil
}

func getXHSUserProfileHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.XHSClient == nil {
		return xhsUnavailable()
	}

	args := parseArgs(req.Params.Arguments)
	userID, _ := args["user_id"].(string)
	xsecToken, _ := args["xsec_token"].(string)

	if userID == "" {
		return errorResult("user_id is required"), nil
	}

	profile, err := svcs.XHSClient.GetUserProfile(ctx, userID, xsecToken)
	if err != nil {
		return nil, fmt.Errorf("获取用户资料失败: %w", err)
	}

	interactions := map[string]string{}
	for _, i := range profile.Interactions {
		interactions[i.Type] = i.Count
	}

	feeds := make([]map[string]any, 0, len(profile.Feeds))
	for _, f := range profile.Feeds {
		feed := map[string]any{
			"id": f.ID, "title": f.NoteCard.DisplayTitle,
			"author": f.NoteCard.User.Nickname,
			"like_count": f.NoteCard.InteractInfo.LikedCount,
			"collect_count": f.NoteCard.InteractInfo.CollectedCount,
			"comment_count": f.NoteCard.InteractInfo.CommentCount,
			"xsec_token": f.XsecToken,
		}
		if f.NoteCard.Cover.URLDefault != "" {
			feed["cover_url"] = f.NoteCard.Cover.URLDefault
		}
		feeds = append(feeds, feed)
	}

	return textResult(map[string]any{
		"nickname": profile.UserBasicInfo.Nickname,
		"red_id": profile.UserBasicInfo.RedID,
		"desc": profile.UserBasicInfo.Desc,
		"avatar_url": profile.UserBasicInfo.Avatar,
		"ip_location": profile.UserBasicInfo.IPLocation,
		"interactions": interactions,
		"feeds_count": len(feeds),
		"feeds": feeds,
	})
}

func extractDetailImageURLs(images []xhs.DetailImage) []string {
	urls := make([]string, 0, len(images))
	for _, img := range images {
		if img.URLDefault != "" {
			urls = append(urls, img.URLDefault)
		}
	}
	return urls
}
