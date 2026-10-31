package wework

import (
	"encoding/json"
	"fmt"
)

// 定义请求结构体
type GetExternalcontactRequest struct {
	Cursor         string `json:"cursor"`
	ExternalUserID string `json:"external_userid"`
}

// 外部联系人结构体
type GetExternalcontactResponse struct {
	CommonError
	ExternalContact struct {
		ExternalUserID string `json:"external_userid"` // 外部联系人的userid
		Name           string `json:"name"`            // 外部联系人的名称
		Avatar         string `json:"avatar"`          // 外部联系人头像
		Type           int    `json:"type"`            // 外部联系人的类型，1表示该外部联系人是微信用户，2表示该外部联系人是企业微信用户
		Gender         int    `json:"gender"`          // 外部联系人性别 0-未知 1-男性 2-女性
		UnionID        string `json:"unionid"`         // 外部联系人在微信开放平台的唯一身份标识（微信unionid）
	} `json:"external_contact"`

	Position        string `json:"position"`       // 外部联系人的职位
	CorpName        string `json:"corp_name"`      // 外部联系人所在企业的简称
	CorpFullName    string `json:"corp_full_name"` // 外部联系人所在企业的主体名称
	ExternalProfile struct {
		// 外部联系人的自定义展示信息，可以有多个字段和多种类型，包括文本，网页和小程序
		// 仅当联系人类型是企业微信用户时有此字段，字段详情见对外属性
	} `json:"external_profile"`
	FollowUser []FollowUser `json:"follow_user"`
	NextCursor string       `json:"next_cursor"` // 分页的cursor，当跟进人多于500人时返回
}

type FollowUser struct {
	UserID      string `json:"userid"`      // 添加了此外部联系人的企业成员userid
	Remark      string `json:"remark"`      // 该成员对此外部联系人的备注
	Description string `json:"description"` // 该成员对此外部联系人的描述
	CreateTime  int64  `json:"createtime"`  // 该成员添加此外部联系人的时间
	Tags        []struct {
		GroupName string `json:"group_name"` // 该成员添加此外部联系人所打标签的分组名称
		TagName   string `json:"tag_name"`   // 该成员添加此外部联系人所打标签名称
		Type      int    `json:"type"`       // 该成员添加此外部联系人所打标签类型, 1-企业设置，2-用户自定义，3-规则组标签
		TagID     string `json:"tag_id"`     // 该成员添加此外部联系人所打企业标签的id
	} `json:"tags"`
	RemarkCorpName string   `json:"remark_corp_name"` // 该成员对此微信客户备注的企业名称
	RemarkMobiles  []string `json:"remark_mobiles"`   // 该成员对此客户备注的手机号码
	AddWay         int      `json:"add_way"`          // 该成员添加此客户的来源
	WechatChannels struct {
		Nickname string `json:"nickname"` // 视频号名称
		Source   int    `json:"source"`   // 视频号添加场景，0-未知 1-视频号主页 2-视频号直播间 3-视频号留资服务
	} `json:"wechat_channels"`
	OperUserID string `json:"oper_userid"` // 发起添加的userid
	State      string `json:"state"`       // 企业自定义的state参数，用于区分客户具体是通过哪个「联系我」或获客链接添加
}

// doc: https://developer.work.weixin.qq.com/document/path/92265
func (w *Wework) GetExternalcontact(request *GetExternalcontactRequest) (*GetExternalcontactResponse, error) {

	token, err := w.AccessToken()
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	queries := map[string]string{
		"cursor":          request.Cursor,
		"access_token":    token.AccessToken,
		"external_userid": request.ExternalUserID,
	}

	mutex := w.userLockFor(request.ExternalUserID)
	mutex.Lock()
	defer mutex.Unlock()

	response := new(GetExternalcontactResponse)
	err = w.locking(func() error {
		url := "https://qyapi.weixin.qq.com/cgi-bin/externalcontact/get"
		resp, err := w.client.R().
			SetHeader("Content-Type", "application/json").
			SetQueryParams(queries).
			Get(url)
		if err != nil {
			return err
		}

		if resp.IsStatusFailure() {
			return fmt.Errorf("failed to request, status code: %d", resp.StatusCode())
		}

		if err := json.Unmarshal(resp.Bytes(), response); err != nil {
			return err
		}

		if err := response.GetResponseError(); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, sanitizeRequestError(err)
	}

	return response, nil
}
