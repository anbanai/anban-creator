package server

import (
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/sdk/wework/encrypt"
	"github.com/gofiber/fiber/v3"
)

// Server 微信通知服务处理器
type Server struct {
	appID  string // 小程序 ID
	token  string // 微信服务器验证令牌
	aesKey []byte // base64 解码后的消息加密密钥

	// 默认处理器
	// 没有自定义处理器的情况下调用该处理器
	handler func(map[string]any) map[string]any

	addEnterpriseCustomerHandler func(*AddEnterpriseCustomerEvent) error

	wxcpt  *encrypt.WXBizMsgCrypt
	now    func() time.Time
	replay *callbackReplayCache
}

// 当用户通过设置界面改变订阅消息事件内容
func (srv *Server) OnSubscribeAddEnterpriseCustomer(fn func(*AddEnterpriseCustomerEvent) error) {
	srv.addEnterpriseCustomerHandler = fn
}

// NewServer 返回经过初始化的Server。回调重放保护使用进程内缓存；
// 多副本部署必须将该端点限制到单个实例，或在外层提供共享幂等保护。
func NewServer(appID, token, aesKey string, handler func(map[string]any) map[string]any) (*Server, error) {
	key, err := base64.RawStdEncoding.DecodeString(aesKey)
	if err != nil {
		return nil, err
	}

	wxcpt := encrypt.NewWXBizMsgCrypt(token, aesKey, appID, encrypt.XMLType)

	server := Server{
		appID:   appID,
		token:   token,
		aesKey:  key,
		handler: handler,
		wxcpt:   wxcpt,
		now:     time.Now,
		replay:  newCallbackReplayCache(),
	}

	return &server, nil
}

// Serve 接收并处理微信通知服务
func (srv *Server) Serve(ctx fiber.Ctx) error {

	switch ctx.Method() {
	case "POST":

		_, err := srv.handleRequest(ctx)
		if err != nil {
			return fmt.Errorf("handle request content error: %w", err)
		}

		return ctx.Status(http.StatusOK).SendString("success")
	case "GET":
		raw, err := srv.isValid(ctx)
		if err != nil {
			return err
		}

		if _, err := ctx.Status(http.StatusOK).Write(raw); err != nil {
			return err
		}

		return nil
	default:
		return errors.New("invalid request method")
	}
}

func (srv *Server) isValid(ctx fiber.Ctx) ([]byte, error) {
	signature := ctx.Query("msg_signature")
	timestamp := ctx.Query("timestamp")
	nonce := ctx.Query("nonce")
	echostr := ctx.Query("echostr")
	if err := srv.validateTimestamp(timestamp); err != nil {
		return nil, err
	}

	raw, err := srv.wxcpt.VerifyURL(signature, timestamp, nonce, echostr)
	if err != nil {
		return nil, fmt.Errorf("verify url error, code:%v, msg: %s", err.ErrCode, err.ErrMsg)
	}

	return raw, nil
}

func (srv *Server) validateTimestamp(raw string) error {
	timestamp, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || timestamp <= 0 {
		return errors.New("invalid callback timestamp")
	}
	now := time.Now()
	if srv.now != nil {
		now = srv.now()
	}
	callbackTime := time.Unix(timestamp, 0)
	if delta := now.Sub(callbackTime); delta < -callbackTimestampWindow || delta > callbackTimestampWindow {
		return errors.New("callback timestamp expired")
	}
	return nil
}

// 处理消息体
func (srv *Server) handleRequest(ctx fiber.Ctx) (any, error) {

	nonce, signature, timestamp := ctx.Query("nonce"), ctx.Query("msg_signature"), ctx.Query("timestamp")
	if err := srv.validateTimestamp(timestamp); err != nil {
		return nil, err
	}

	msg, err := srv.wxcpt.DecryptMsg(signature, timestamp, nonce, ctx.Body())
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt message, error: %w", err)
	}

	result := new(CommonServerResult)
	if err := xml.Unmarshal(msg, result); nil != err {
		return nil, fmt.Errorf("failed to unmarshal message, error: %w", err)
	}

	keys := []string{"callback:" + signature + "\x00" + nonce}
	if result.MsgID != "" {
		keys = append(keys, "message:"+result.MsgID)
	}
	now := time.Now()
	if srv.now != nil {
		now = srv.now()
	}
	if !srv.replay.claim(keys, now) {
		return nil, nil
	}
	processed := false
	defer func() {
		if !processed {
			srv.replay.release(keys)
		}
	}()

	switch result.MsgType {

	case "event":
		switch result.Event {
		case EventChangeExternalContact:
			res := new(AddEnterpriseCustomerEvent)
			if err := xml.Unmarshal(msg, res); nil != err {
				return nil, fmt.Errorf("failed to unmarshal add enterprise customer event, error: %w", err)
			}

			if srv.addEnterpriseCustomerHandler != nil {
				if err := srv.addEnterpriseCustomerHandler(res); err != nil {
					return nil, fmt.Errorf("failed to handle add enterprise customer event, error: %w", err)
				}
			}
		}

	default:
		res, err := decodeXMLMap(msg)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal message, error: %w", err)
		}

		if srv.handler != nil {
			srv.handler(res)
		}
	}

	processed = true
	return nil, nil
}

func decodeXMLMap(data []byte) (map[string]any, error) {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("XML message has no root element")
			}
			return nil, err
		}
		root, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		value, err := decodeXMLElement(decoder, root)
		if err != nil {
			return nil, err
		}
		message, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("XML root element must contain child fields")
		}
		return message, nil
	}
}

func decodeXMLElement(decoder *xml.Decoder, start xml.StartElement) (any, error) {
	fields := make(map[string]any)
	var text strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch token := token.(type) {
		case xml.StartElement:
			value, err := decodeXMLElement(decoder, token)
			if err != nil {
				return nil, err
			}
			name := token.Name.Local
			if previous, exists := fields[name]; exists {
				if values, ok := previous.([]any); ok {
					fields[name] = append(values, value)
				} else {
					fields[name] = []any{previous, value}
				}
			} else {
				fields[name] = value
			}
		case xml.CharData:
			text.Write([]byte(token))
		case xml.EndElement:
			value := strings.TrimSpace(text.String())
			if len(fields) == 0 {
				return value, nil
			}
			if value != "" {
				fields["#text"] = value
			}
			return fields, nil
		}
	}
}
