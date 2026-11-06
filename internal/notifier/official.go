// Package notifier delivers alerts through official bot OpenAPIs.
package notifier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"servicemonitoragent/internal/monitor"
)

type Target struct {
	ID   string
	Name string
	Kind string
}

type Callbacks struct {
	Ready  func()
	Failed func(error)
	Target func(Target)
}

func (c Callbacks) ready() {
	if c.Ready != nil {
		c.Ready()
	}
}
func (c Callbacks) failed(err error) {
	if c.Failed != nil {
		c.Failed(err)
	}
}
func (c Callbacks) target(t Target) {
	if c.Target != nil {
		c.Target(t)
	}
}

func alertText(a monitor.Alert) string {
	return fmt.Sprintf("[Service Monitor] %s %s %s: %.1f%% (since %s)", a.Host, a.Metric, a.Kind, a.Value, a.StartedAt.UTC().Format(time.RFC3339))
}

func validateCredentials(appID, appSecret string) error {
	if strings.TrimSpace(appID) == "" || strings.TrimSpace(appSecret) == "" {
		return errors.New("请填写 AppID 和 AppSecret")
	}
	if strings.ContainsAny(appID+appSecret, "\r\n\x00") {
		return errors.New("AppID 和 AppSecret 不能包含换行或空字符")
	}
	return nil
}

type Feishu struct {
	appID     string
	appSecret string
	targetID  string
	client    *lark.Client
	callbacks Callbacks
}

func NewFeishu(appID, appSecret, targetID string, callbacks Callbacks) (*Feishu, error) {
	appID, appSecret = strings.TrimSpace(appID), strings.TrimSpace(appSecret)
	if err := validateCredentials(appID, appSecret); err != nil {
		return nil, err
	}
	return &Feishu{appID: appID, appSecret: appSecret, targetID: strings.TrimSpace(targetID), client: lark.NewClient(appID, appSecret), callbacks: callbacks}, nil
}

func (f *Feishu) Send(ctx context.Context, a monitor.Alert) error {
	if f.targetID == "" {
		return errors.New("请选择飞书目标会话或填写 Chat ID")
	}
	content, err := json.Marshal(map[string]string{"text": alertText(a)})
	if err != nil {
		return err
	}
	req := larkim.NewCreateMessageReqBuilder().ReceiveIdType("chat_id").Body(
		larkim.NewCreateMessageReqBodyBuilder().ReceiveId(f.targetID).MsgType("text").Content(string(content)).Build(),
	).Build()
	resp, err := f.client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("飞书消息发送失败: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("飞书消息发送失败: %d %s", resp.Code, resp.Msg)
	}
	return nil
}

func (f *Feishu) Run(ctx context.Context) error {
	handler := dispatcher.NewEventDispatcher("", "").OnP2MessageReceiveV1(func(_ context.Context, event *larkim.P2MessageReceiveV1) error {
		if event.Event != nil && event.Event.Message != nil && event.Event.Message.ChatId != nil {
			chatType := ""
			if event.Event.Message.ChatType != nil {
				chatType = *event.Event.Message.ChatType
			}
			f.callbacks.target(Target{ID: *event.Event.Message.ChatId, Name: "飞书 " + chatType + " 会话", Kind: chatType})
		}
		return nil
	})
	client := larkws.NewClient(f.appID, f.appSecret,
		larkws.WithEventHandler(handler),
		larkws.WithLogLevel(larkcore.LogLevelError),
		larkws.WithOnReady(f.callbacks.ready),
		larkws.WithOnError(f.callbacks.failed),
	)
	return client.Start(ctx)
}
