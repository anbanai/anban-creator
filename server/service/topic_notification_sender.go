package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/notification"
	"github.com/anbanai/anban-creator/server/sdk/dingtalkbot"
	"github.com/anbanai/anban-creator/server/sdk/feishubot"
	"gorm.io/gorm"
)

func (s *TopicRadarService) SendTopicNotification(ctx context.Context, deliveryID string) error {
	delivery, err := s.repo.TopicNotificationDeliveries().FindByID(ctx, deliveryID)
	if err != nil {
		return err
	}
	if delivery.Status == model.TopicNotificationSent || delivery.Status == model.TopicNotificationInvalid || delivery.Attempt >= 8 {
		return nil
	}
	token := uuid.NewString()
	claimed, err := s.repo.TopicNotificationDeliveries().Claim(ctx, delivery.ID, token, s.now().Add(2*time.Minute))
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	// Reload the attempt increment committed with the lease, including after crash recovery.
	delivery, err = s.repo.TopicNotificationDeliveries().FindByID(ctx, delivery.ID)
	if err != nil {
		return err
	}
	if delivery.LeaseToken != token {
		return errors.New("notification delivery lease lost")
	}
	channel, err := s.repo.TopicNotificationChannels().FindByID(ctx, delivery.ChannelID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.failNotification(ctx, delivery, token, model.TopicNotificationInvalid, errors.New("notification channel no longer exists"))
	}
	if err != nil {
		return s.failNotification(ctx, delivery, token, model.TopicNotificationFailed, err)
	}
	digest, err := s.repo.TopicDigests().FindByID(ctx, delivery.DigestID)
	if err != nil {
		return s.failNotification(ctx, delivery, token, model.TopicNotificationFailed, err)
	}
	if digest.UserID != channel.UserID || digest.ProjectID != channel.ProjectID || digest.Status != model.TopicDigestStatusSucceeded {
		return s.failNotification(ctx, delivery, token, model.TopicNotificationInvalid, errors.New("notification delivery scope is invalid"))
	}
	if !channel.Enabled {
		return s.failNotification(ctx, delivery, token, model.TopicNotificationInvalid, errors.New("notification channel is disabled"))
	}
	var payload struct {
		Suggestions []topicNotificationPayload `json:"suggestions"`
	}
	if err := json.Unmarshal(delivery.Payload, &payload); err != nil {
		return s.failNotification(ctx, delivery, token, model.TopicNotificationInvalid, err)
	}
	text := renderTopicNotificationText(payload.Suggestions)
	status, sendErr := s.sendTopicNotificationText(ctx, channel, text)
	if sendErr != nil {
		return s.failNotification(ctx, delivery, token, status, sendErr)
	}
	delivery.NextAttemptAt = nil
	now := s.now()
	delivery.Status = model.TopicNotificationSent
	delivery.LastError = ""
	delivery.SentAt = &now
	return s.repo.TopicNotificationDeliveries().UpdateClaimed(ctx, delivery, token)
}

func renderTopicNotificationText(items []topicNotificationPayload) string {
	var b strings.Builder
	for i, item := range items {
		b.WriteString(fmt.Sprintf("\n## %d. %s\n%s\n来源：%s\n", i+1, item.Title, item.Introduction, item.SourceSummary))
		for _, ref := range item.SourceRefs {
			label := ref.Label
			if label == "" {
				label = ref.Title
			}
			if label == "" {
				label = ref.RefID
			}
			if ref.URL != "" {
				b.WriteString(fmt.Sprintf("- %s: %s\n", label, ref.URL))
			} else {
				b.WriteString(fmt.Sprintf("- %s\n", label))
			}
		}
		if item.VideoFit != "" {
			b.WriteString("短视频形式：" + item.VideoFit + "\n")
		}
	}
	return b.String()
}

func notificationRequestBody(provider, text string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case model.TopicNotificationProviderFeishu:
		return notification.MarkdownBody(notification.NotifyTypeLark, "选题雷达", text)
	case model.TopicNotificationProviderWeCom:
		return notification.MarkdownBody(notification.NotifyTypeWeWork, "选题雷达", text)
	case model.TopicNotificationProviderDingTalk:
		return notification.MarkdownBody(notification.NotifyTypeDingTalk, "选题雷达", text)
	default:
		return nil, fmt.Errorf("unsupported notification provider %q", provider)
	}
}

func (s *TopicRadarService) failNotification(ctx context.Context, delivery *model.TopicNotificationDelivery, token, status string, err error) error {
	delivery.Status = status
	delivery.LastError = err.Error()
	if status == model.TopicNotificationFailed {
		next := s.now().Add(time.Minute * time.Duration(1<<min(delivery.Attempt, 8)))
		delivery.NextAttemptAt = &next
	} else {
		delivery.NextAttemptAt = nil
	}
	if updateErr := s.repo.TopicNotificationDeliveries().UpdateClaimed(ctx, delivery, token); updateErr != nil {
		return errors.Join(err, updateErr)
	}
	return err
}

// Keep the cause for errors.Is without exposing webhook query secrets in logs.
type topicNotificationTransportError struct{ cause error }

func (e topicNotificationTransportError) Error() string { return "notification transport failed" }
func (e topicNotificationTransportError) Unwrap() error { return e.cause }

func (s *TopicRadarService) sendTopicNotificationText(ctx context.Context, channel *model.TopicNotificationChannel, text string) (string, error) {
	if err := validateTopicNotificationProvider(channel.Provider, channel.WebhookURL); err != nil {
		return model.TopicNotificationInvalid, err
	}
	if err := validateTopicNotificationWebhookURL(ctx, channel.WebhookURL); err != nil {
		var dnsError *net.DNSError
		if errors.As(err, &dnsError) || errors.Is(err, context.DeadlineExceeded) {
			return model.TopicNotificationFailed, topicNotificationTransportError{cause: err}
		}
		return model.TopicNotificationInvalid, err
	}
	body, err := notificationRequestBody(channel.Provider, text)
	if err != nil {
		return model.TopicNotificationInvalid, err
	}
	if strings.EqualFold(strings.TrimSpace(channel.Provider), model.TopicNotificationProviderWeCom) {
		// Validate the rendered markdown, not the caller text: the provider
		// envelope adds the title heading and a trailing newline.
		var payload struct {
			Markdown struct {
				Content string `json:"content"`
			} `json:"markdown"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return model.TopicNotificationInvalid, err
		}
		if len([]byte(payload.Markdown.Content)) > 4096 {
			return model.TopicNotificationInvalid, errors.New("wecom markdown exceeds 4096 UTF-8 bytes")
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, channel.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return model.TopicNotificationInvalid, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := s.notificationClient
	if client == nil {
		client = newTopicNotificationHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return model.TopicNotificationFailed, topicNotificationTransportError{cause: err}
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, topicNotificationMaxResponseBody+1))
	if readErr != nil {
		return model.TopicNotificationFailed, fmt.Errorf("read notification response: %w", readErr)
	}
	if len(responseBody) > topicNotificationMaxResponseBody {
		return model.TopicNotificationInvalid, errors.New("notification provider response exceeds maximum size")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status := model.TopicNotificationFailed
		if (resp.StatusCode >= 300 && resp.StatusCode < 400) || (resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests) {
			status = model.TopicNotificationInvalid
		}
		return status, fmt.Errorf("notification provider returned HTTP %d", resp.StatusCode)
	}
	var acknowledgement struct {
		Code       *int `json:"code"`
		ErrCode    *int `json:"errcode"`
		LegacyCode *int `json:"StatusCode"`
	}
	if err := json.Unmarshal(responseBody, &acknowledgement); err != nil {
		return model.TopicNotificationFailed, errors.New("notification provider returned invalid acknowledgement")
	}
	code := acknowledgement.ErrCode
	if strings.EqualFold(strings.TrimSpace(channel.Provider), model.TopicNotificationProviderFeishu) {
		code = acknowledgement.Code
		if code == nil {
			code = acknowledgement.LegacyCode
		}
	}
	if code == nil {
		return model.TopicNotificationFailed, errors.New("notification provider returned no acknowledgement code")
	}
	if *code != 0 {
		if topicNotificationProviderCodeRetryable(channel.Provider, *code) {
			return model.TopicNotificationFailed, fmt.Errorf("notification provider rate limited delivery (code %d)", *code)
		}
		// The provider accepted the HTTP request but rejected the webhook
		// configuration or payload; retrying the same request cannot succeed.
		return model.TopicNotificationInvalid, errors.New("notification provider rejected delivery")
	}
	return model.TopicNotificationSent, nil
}

func (s *TopicRadarService) TestNotificationChannel(ctx context.Context, userID, id string) error {
	channel, err := s.notificationChannelForUser(ctx, userID, id)
	if err != nil {
		return err
	}
	_, err = s.sendTopicNotificationText(ctx, channel, "选题雷达通知测试：连接成功。正式分析完成后，选题会自动进入选题库。")
	return err
}

// topicNotificationProviderCodeRetryable maps provider-level rate-limit codes
// returned with HTTP 200 to the retryable delivery state.
func topicNotificationProviderCodeRetryable(provider string, code int) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case model.TopicNotificationProviderFeishu:
		return code == feishubot.CodeFrequencyLimit
	case model.TopicNotificationProviderDingTalk:
		return code == dingtalkbot.CodeFrequencyLimit
	case model.TopicNotificationProviderWeCom:
		return code == 45009 || code == 45033
	}
	return false
}
