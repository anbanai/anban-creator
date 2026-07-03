package service

import (
	"context"
	"errors"
	"strings"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/wcf"
)

type ilinkSender interface {
	SendText(ctx context.Context, accountID, toUserID, text string) error
}

type ilinkConversation interface {
	Handle(ctx context.Context, binding *model.IlinkBinding, event wcf.Event)
}

type IlinkGateway struct {
	repo         repository.Repository
	bindings     *IlinkBindingService
	conversation ilinkConversation
	sender       ilinkSender
	logger       *zerolog.Logger
}

func NewIlinkGateway(repo repository.Repository, bindings *IlinkBindingService, conversation ilinkConversation, sender ilinkSender, logger *zerolog.Logger) *IlinkGateway {
	return &IlinkGateway{repo: repo, bindings: bindings, conversation: conversation, sender: sender, logger: logger}
}

func (g *IlinkGateway) ProcessEvent(ctx context.Context, ev wcf.Event) {
	if g == nil || g.repo == nil || !ev.IsInboundText() || ev.AccountID == "" || ev.FromUserID == "" {
		return
	}
	binding, err := g.repo.IlinkBindings().FindByContact(ctx, ev.AccountID, ev.FromUserID)
	if err == nil && binding != nil && binding.Status == model.IlinkBindingStatusActive {
		if g.conversation != nil {
			g.conversation.Handle(ctx, binding, ev)
		}
		return
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) && !strings.Contains(err.Error(), gorm.ErrRecordNotFound.Error()) {
		if g.logger != nil {
			g.logger.Warn().Err(err).Str("account_id", ev.AccountID).Msg("ilink contact lookup failed")
		}
	}

	code := strings.TrimSpace(ev.BodyText)
	if g.bindings == nil {
		g.reply(ctx, ev.AccountID, ev.FromUserID, "请先在 Studio 设置页获取绑定码，然后把绑定码发送给我。")
		return
	}
	binding, err = g.bindings.CompleteBindByCode(ctx, code, ev.AccountID, ev.FromUserID)
	if err != nil {
		g.reply(ctx, ev.AccountID, ev.FromUserID, "请先在 Studio 设置页获取绑定码，然后把绑定码发送给我。")
		return
	}
	g.reply(ctx, stringValue(binding.PlatformAccountID), stringValue(binding.ExternalUserID), "已绑定微信助手。之后可以直接发消息创建任务、查询状态或接收任务完成提醒。")
}

func (g *IlinkGateway) reply(ctx context.Context, accountID, toUserID, text string) {
	if g == nil || g.sender == nil || accountID == "" || toUserID == "" || text == "" {
		return
	}
	if err := g.sender.SendText(ctx, accountID, toUserID, text); err != nil && g.logger != nil {
		g.logger.Warn().Err(err).Str("account_id", accountID).Str("to_user_id", toUserID).Msg("ilink reply failed")
	}
}
