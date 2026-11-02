package dingtalkbot

// MessageType 消息类型
type MessageType string

const (
	// MessageTypeText 文本消息
	MessageTypeText MessageType = "text"
	// MessageTypeLink 链接消息
	MessageTypeLink MessageType = "link"
	// MessageTypeMarkdown markdown消息
	MessageTypeMarkdown MessageType = "markdown"
	// MessageTypeActionCard 整体跳转ActionCard消息
	MessageTypeActionCard MessageType = "actionCard"
	// MessageTypeFeedCard FeedCard消息
	MessageTypeFeedCard MessageType = "feedCard"
)

// Message 钉钉机器人消息接口
type Message interface {
	Type() MessageType
}

// BaseMessage 基础消息结构
type BaseMessage struct {
	MsgType MessageType `json:"msgtype"`
}

// Type 获取消息类型
func (m *BaseMessage) Type() MessageType {
	return m.MsgType
}

// TextMessage 文本消息
type TextMessage struct {
	BaseMessage
	Text struct {
		Content string `json:"content"`
	} `json:"text"`
	At struct {
		AtMobiles []string `json:"atMobiles,omitempty"`
		AtUserIds []string `json:"atUserIds,omitempty"`
		IsAtAll   bool     `json:"isAtAll,omitempty"`
	} `json:"at,omitempty"`
}

// NewTextMessage 创建文本消息
func NewTextMessage(text string) *TextMessage {
	msg := &TextMessage{}
	msg.MsgType = MessageTypeText
	msg.Text.Content = text
	// 预分配At字段的切片容量
	msg.At.AtMobiles = make([]string, 0, 4)
	msg.At.AtUserIds = make([]string, 0, 4)
	return msg
}

// LinkMessage 链接消息
type LinkMessage struct {
	BaseMessage
	Link struct {
		Title      string `json:"title"`
		Text       string `json:"text"`
		MessageURL string `json:"messageUrl"`
		PicURL     string `json:"picUrl,omitempty"`
	} `json:"link"`
}

// NewLinkMessage 创建链接消息
func NewLinkMessage(title, text, messageURL string) *LinkMessage {
	msg := &LinkMessage{}
	msg.MsgType = MessageTypeLink
	msg.Link.Title = title
	msg.Link.Text = text
	msg.Link.MessageURL = messageURL
	return msg
}

// MarkdownMessage markdown消息
type MarkdownMessage struct {
	BaseMessage
	Markdown struct {
		Title string `json:"title"`
		Text  string `json:"text"`
	} `json:"markdown"`
	At struct {
		AtMobiles []string `json:"atMobiles,omitempty"`
		AtUserIds []string `json:"atUserIds,omitempty"`
		IsAtAll   bool     `json:"isAtAll,omitempty"`
	} `json:"at,omitempty"`
}

// NewMarkdownMessage 创建markdown消息
func NewMarkdownMessage(title, text string) *MarkdownMessage {
	msg := &MarkdownMessage{}
	msg.MsgType = MessageTypeMarkdown
	msg.Markdown.Title = title
	msg.Markdown.Text = text
	// 预分配At字段的切片容量
	msg.At.AtMobiles = make([]string, 0, 4)
	msg.At.AtUserIds = make([]string, 0, 4)
	return msg
}

// ActionCardMessage 整体跳转ActionCard消息
type ActionCardMessage struct {
	BaseMessage
	ActionCard struct {
		Title          string `json:"title"`
		Text           string `json:"text"`
		SingleTitle    string `json:"singleTitle"`
		SingleURL      string `json:"singleURL"`
		BtnOrientation string `json:"btnOrientation,omitempty"`
		HideAvatar     string `json:"hideAvatar,omitempty"`
	} `json:"actionCard"`
}

// NewActionCardMessage 创建整体跳转ActionCard消息
func NewActionCardMessage(title, text, singleTitle, singleURL string) *ActionCardMessage {
	msg := &ActionCardMessage{}
	msg.MsgType = MessageTypeActionCard
	msg.ActionCard.Title = title
	msg.ActionCard.Text = text
	msg.ActionCard.SingleTitle = singleTitle
	msg.ActionCard.SingleURL = singleURL
	return msg
}

// FeedCardLink FeedCard消息链接
type FeedCardLink struct {
	Title      string `json:"title"`
	MessageURL string `json:"messageURL"`
	PicURL     string `json:"picURL"`
}

// FeedCardMessage FeedCard消息
type FeedCardMessage struct {
	BaseMessage
	FeedCard struct {
		Links []FeedCardLink `json:"links"`
	} `json:"feedCard"`
}

// NewFeedCardMessage 创建FeedCard消息
func NewFeedCardMessage(links []FeedCardLink) *FeedCardMessage {
	msg := &FeedCardMessage{}
	msg.MsgType = MessageTypeFeedCard // 修复消息类型错误
	// 预分配Links切片容量
	if links == nil {
		msg.FeedCard.Links = make([]FeedCardLink, 0, 4)
	} else {
		msg.FeedCard.Links = links
	}
	return msg
}
