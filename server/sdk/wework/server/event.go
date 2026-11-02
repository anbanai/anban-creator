package server

import "encoding/xml"

// EventType 事件类型
type EventType string

// 所有事件类型
const (
	EventChangeExternalContact EventType = "change_external_contact" // 添加企业客户事件
)

// 添加企业客户事件
type AddEnterpriseCustomerEvent struct {
	CommonServerResult
	ChangeType     string `xml:"ChangeType"`     // 此时固定为add_half_external_contact
	UserID         string `xml:"UserID"`         // 企业服务人员的UserID
	ExternalUserID string `xml:"ExternalUserID"` // 外部联系人的userid，注意不是企业成员的账号
	State          string `xml:"State"`          // 添加此用户的「联系我」方式配置的state参数，或在获客链接中指定的customer_channel参数，可用于识别添加此用户的渠道
	WelcomeCode    string `xml:"WelcomeCode"`    // 欢迎语code，可用于发送欢迎语
}

type CommonServerResult struct {
	XMLName      xml.Name  `xml:"xml" json:"-"`
	ToUserName   string    `xml:"ToUserName"`   // 企业微信CorpID
	FromUserName string    `xml:"FromUserName"` // 此事件该值固定为sys，表示该消息由系统生成
	CreateTime   int64     `xml:"CreateTime"`   // 消息创建时间 （整型）
	MsgType      string    `xml:"MsgType"`      // 消息的类型，此时固定为event
	Event        EventType `xml:"Event"`        // 事件的类型，此时固定为change_external_contact
	MsgID        string    `xml:"MsgId"`        // 消息唯一 ID
}
