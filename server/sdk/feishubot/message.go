package feishubot

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// MessageType 消息类型
type MessageType string

const (
	// MessageTypeText 文本消息
	MessageTypeText MessageType = "text"
	// MessageTypePost 富文本消息
	MessageTypePost MessageType = "post"
	// MessageTypeInteractive 消息卡片
	MessageTypeInteractive MessageType = "interactive"
	// MessageTypeShareChat 群名片
	MessageTypeShareChat MessageType = "share_chat"
	// MessageTypeImage 图片消息
	MessageTypeImage MessageType = "image"
)

// Message 飞书机器人消息接口
type Message interface {
	Type() MessageType
	ToMap() map[string]any
}

// BaseMessage 基础消息结构
type BaseMessage struct {
	MsgType MessageType `json:"msg_type"`
}

// Type 获取消息类型
func (m *BaseMessage) Type() MessageType {
	return m.MsgType
}

// TextMessage 文本消息
type TextMessage struct {
	BaseMessage
	Content struct {
		Text string `json:"text"`
	} `json:"content"`
}

// NewTextMessage 创建文本消息
func NewTextMessage(text string) *TextMessage {
	msg := &TextMessage{}
	msg.MsgType = MessageTypeText
	msg.Content.Text = text
	return msg
}

// ToMap 转换为map用于JSON序列化
func (m *TextMessage) ToMap() map[string]any {
	return map[string]any{
		"msg_type": m.MsgType,
		"content": map[string]any{
			"text": m.Content.Text,
		},
	}
}

// PostElement 富文本元素接口
type PostElement interface {
	ElementType() string
	ToMap() map[string]any
}

// TextElement 文本元素
type TextElement struct {
	Text string
}

func (e *TextElement) ElementType() string {
	return "text"
}

func (e *TextElement) ToMap() map[string]any {
	return map[string]any{
		"tag":  "text",
		"text": e.Text,
	}
}

// PostContent 富文本内容结构
type PostContent struct {
	Title   string          `json:"title"`
	Content [][]PostElement `json:"content"`
}

// PostMessage 富文本消息
type PostMessage struct {
	BaseMessage
	Content struct {
		Post map[string]*PostContent `json:"post"`
	} `json:"content"`
}

// NewZhCnPostMessage 创建中文富文本消息
func NewZhCnPostMessage(title string) *PostMessage {
	msg := &PostMessage{}
	msg.MsgType = MessageTypePost
	msg.Content.Post = map[string]*PostContent{
		"zh_cn": {
			Title:   title,
			Content: make([][]PostElement, 0),
		},
	}
	return msg
}

// LinkElement 链接元素
type LinkElement struct {
	Text string
	Href string
}

func (e *LinkElement) ElementType() string {
	return "a"
}

func (e *LinkElement) ToMap() map[string]any {
	return map[string]any{
		"tag":  "a",
		"text": e.Text,
		"href": e.Href,
	}
}

// ImageElement 图片元素
type ImageElement struct {
	ImageKey string
	Width    int
	Height   int
}

func (e *ImageElement) ElementType() string {
	return "img"
}

func (e *ImageElement) ToMap() map[string]any {
	return map[string]any{
		"tag":       "img",
		"image_key": e.ImageKey,
		"width":     e.Width,
		"height":    e.Height,
	}
}

// AtElement @用户元素
type AtElement struct {
	UserID string
}

func (e *AtElement) ElementType() string {
	return "at"
}

func (e *AtElement) ToMap() map[string]any {
	return map[string]any{
		"tag":     "at",
		"user_id": e.UserID,
	}
}

// AddText 添加文本内容
func (m *PostMessage) AddText(row int, text string) *PostMessage {
	if row < 0 {
		return m
	}

	content := m.Content.Post["zh_cn"].Content

	// 预分配足够容量的切片
	if len(content) <= row {
		newContent := make([][]PostElement, row+1)
		copy(newContent, content)
		content = newContent
	}

	// 确保当前行存在
	if len(content[row]) == 0 {
		content[row] = make([]PostElement, 0, 4) // 预分配容量
	}

	content[row] = append(content[row], &TextElement{Text: text})
	m.Content.Post["zh_cn"].Content = content
	return m
}

// AddLink 添加链接
func (m *PostMessage) AddLink(row int, text, url string) *PostMessage {
	if row < 0 {
		return m
	}

	content := m.Content.Post["zh_cn"].Content

	// 预分配足够容量的切片
	if len(content) <= row {
		newContent := make([][]PostElement, row+1)
		copy(newContent, content)
		content = newContent
	}

	// 确保当前行存在
	if len(content[row]) == 0 {
		content[row] = make([]PostElement, 0, 4) // 预分配容量
	}

	content[row] = append(content[row], &LinkElement{Text: text, Href: url})
	m.Content.Post["zh_cn"].Content = content
	return m
}

// AddImage 添加图片
func (m *PostMessage) AddImage(row int, imageKey string) *PostMessage {
	if row < 0 {
		return m
	}

	content := m.Content.Post["zh_cn"].Content

	// 预分配足够容量的切片
	if len(content) <= row {
		newContent := make([][]PostElement, row+1)
		copy(newContent, content)
		content = newContent
	}

	// 确保当前行存在
	if len(content[row]) == 0 {
		content[row] = make([]PostElement, 0, 4) // 预分配容量
	}

	content[row] = append(content[row], &ImageElement{ImageKey: imageKey, Width: 300, Height: 300})
	m.Content.Post["zh_cn"].Content = content
	return m
}

// AddAt 添加@用户
func (m *PostMessage) AddAt(row int, userId string) *PostMessage {
	if row < 0 {
		return m
	}

	content := m.Content.Post["zh_cn"].Content

	// 预分配足够容量的切片
	if len(content) <= row {
		newContent := make([][]PostElement, row+1)
		copy(newContent, content)
		content = newContent
	}

	// 确保当前行存在
	if len(content[row]) == 0 {
		content[row] = make([]PostElement, 0, 4) // 预分配容量
	}

	content[row] = append(content[row], &AtElement{UserID: userId})
	m.Content.Post["zh_cn"].Content = content
	return m
}

// ToMap 转换为map用于JSON序列化
func (m *PostMessage) ToMap() map[string]any {
	zhContent := m.Content.Post["zh_cn"]
	// 预分配足够容量的切片
	contentElements := make([][]map[string]any, 0, len(zhContent.Content))

	for _, row := range zhContent.Content {
		// 预分配行切片容量
		elements := make([]map[string]any, 0, len(row))
		for _, element := range row {
			elements = append(elements, element.ToMap())
		}
		contentElements = append(contentElements, elements)
	}

	// 复用基础map结构
	result := make(map[string]any, 3)
	result["msg_type"] = m.MsgType
	result["content"] = map[string]any{
		"post": map[string]any{
			"zh_cn": map[string]any{
				"title":   zhContent.Title,
				"content": contentElements,
			},
		},
	}

	return result
}

// InteractiveMessage 交互式卡片消息
type InteractiveMessage struct {
	BaseMessage
	Card map[string]any `json:"card"`
}

// NewInteractiveMessage 创建交互式卡片消息
func NewInteractiveMessage() *InteractiveMessage {
	msg := &InteractiveMessage{}
	msg.MsgType = MessageTypeInteractive
	msg.Card = map[string]any{
		"config": map[string]any{
			"wide_screen_mode": true,
		},
		"elements": []any{},
	}
	return msg
}

// SetHeader 设置卡片标题
func (m *InteractiveMessage) SetHeader(title string, color string) *InteractiveMessage {
	if color == "" {
		color = "blue" // 默认蓝色
	}

	m.Card["header"] = map[string]any{
		"title": map[string]any{
			"tag":     "plain_text",
			"content": title,
		},
		"template": color,
	}

	return m
}

// AddDivider 添加分割线
func (m *InteractiveMessage) AddDivider() *InteractiveMessage {
	elements := m.Card["elements"].([]any)
	elements = append(elements, map[string]any{
		"tag": "div",
		"fields": []map[string]any{
			{
				"is_short": false,
				"text": map[string]any{
					"tag":     "lark_md",
					"content": "---",
				},
			},
		},
	})
	m.Card["elements"] = elements
	return m
}

// AddText 添加文本内容
func (m *InteractiveMessage) AddText(content string) *InteractiveMessage {
	elements := m.Card["elements"].([]any)
	elements = append(elements, map[string]any{
		"tag": "div",
		"text": map[string]any{
			"tag":     "lark_md",
			"content": content,
		},
	})
	m.Card["elements"] = elements
	return m
}

// AddButton 添加按钮
func (m *InteractiveMessage) AddButton(text, url string) *InteractiveMessage {
	elements := m.Card["elements"].([]any)
	elements = append(elements, map[string]any{
		"tag": "action",
		"actions": []map[string]any{
			{
				"tag": "button",
				"text": map[string]any{
					"tag":     "plain_text",
					"content": text,
				},
				"url":  url,
				"type": "default",
			},
		},
	})
	m.Card["elements"] = elements
	return m
}

// ToMap 转换为map用于JSON序列化
func (m *InteractiveMessage) ToMap() map[string]any {
	return map[string]any{
		"msg_type": m.MsgType,
		"card":     m.Card,
	}
}

// ShareChatMessage 群名片消息
type ShareChatMessage struct {
	BaseMessage
	Content struct {
		ShareChatID string `json:"share_chat_id"`
	} `json:"content"`
}

// NewShareChatMessage 创建群名片消息
func NewShareChatMessage(chatID string) *ShareChatMessage {
	msg := &ShareChatMessage{}
	msg.MsgType = MessageTypeShareChat
	msg.Content.ShareChatID = chatID
	return msg
}

// ToMap 转换为map用于JSON序列化
func (m *ShareChatMessage) ToMap() map[string]any {
	return map[string]any{
		"msg_type": m.MsgType,
		"content": map[string]any{
			"share_chat_id": m.Content.ShareChatID,
		},
	}
}

// ImageMessage 图片消息
type ImageMessage struct {
	BaseMessage
	Content struct {
		ImageKey string `json:"image_key"`
	} `json:"content"`
}

// NewImageMessage 创建图片消息
func NewImageMessage(imageKey string) *ImageMessage {
	msg := &ImageMessage{}
	msg.MsgType = MessageTypeImage
	msg.Content.ImageKey = imageKey
	return msg
}

// ToMap 转换为map用于JSON序列化
func (m *ImageMessage) ToMap() map[string]any {
	return map[string]any{
		"msg_type": m.MsgType,
		"content": map[string]any{
			"image_key": m.Content.ImageKey,
		},
	}
}

// SendMessage 发送消息
func (c *Client) SendMessage(msg Message) (*Response, error) {
	return c.SendMessageContext(context.Background(), msg)
}

// SendMessageContext 使用指定上下文发送消息。
func (c *Client) SendMessageContext(ctx context.Context, msg Message) (*Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return c.sendMessageContext(ctx, msg.ToMap())
}

func (c *Client) sendMessageContext(ctx context.Context, requestBody map[string]any) (*Response, error) {
	if c.secretKey != "" {
		timestamp := time.Now().Unix()
		requestBody["timestamp"] = strconv.FormatInt(timestamp, 10)
		requestBody["sign"] = c.generateSignature(timestamp)
	}

	resp, err := c.httpClient.R().
		SetContext(ctx).
		SetBody(requestBody).
		Post(c.getWebhookURL())
	if err != nil {
		return nil, fmt.Errorf("failed to send message: %w", sanitizeRequestError(err, c.webhookID))
	}
	if !resp.IsStatusSuccess() {
		return nil, fmt.Errorf("failed to send message, status code: %d", resp.StatusCode())
	}

	response := &Response{}
	if err := decodeResponse(resp.String(), response); err != nil {
		return nil, fmt.Errorf("failed to send message: %w", sanitizeRequestError(err, c.webhookID))
	}
	return response, nil
}

// SendRawMessage 发送原始消息
// 用于发送自定义格式的消息，或者SDK未覆盖到的消息类型
func (c *Client) SendRawMessage(msgType MessageType, content any) (*Response, error) {
	return c.SendRawMessageContext(context.Background(), msgType, content)
}

// SendRawMessageContext 使用指定上下文发送原始消息。
func (c *Client) SendRawMessageContext(ctx context.Context, msgType MessageType, content any) (*Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	requestBody := map[string]any{"msg_type": msgType}
	if msgType == MessageTypeInteractive {
		requestBody["card"] = content
	} else {
		requestBody["content"] = content
	}
	return c.sendMessageContext(ctx, requestBody)
}
