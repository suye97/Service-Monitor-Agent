package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"servicemonitoragent/internal/monitor"
)

const qqGroupIntent = 1 << 25

type QQ struct {
	appID      string
	appSecret  string
	targetType string
	targetID   string
	callbacks  Callbacks
	client     *http.Client
	tokenURL   string
	gatewayURL string
	apiURL     string
	dialer     *websocket.Dialer
	mu         sync.Mutex
	token      string
	expires    time.Time
}

func NewQQ(appID, appSecret, targetType, targetID string, callbacks Callbacks) (*QQ, error) {
	appID, appSecret = strings.TrimSpace(appID), strings.TrimSpace(appSecret)
	if err := validateCredentials(appID, appSecret); err != nil {
		return nil, err
	}
	return &QQ{appID: appID, appSecret: appSecret, targetType: strings.TrimSpace(targetType), targetID: strings.TrimSpace(targetID), callbacks: callbacks, client: &http.Client{Timeout: 10 * time.Second}, tokenURL: "https://bots.qq.com/app/getAppAccessToken", gatewayURL: "https://api.sgroup.qq.com/gateway", apiURL: "https://api.sgroup.qq.com", dialer: websocket.DefaultDialer}, nil
}

func (q *QQ) accessToken(ctx context.Context) (string, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.token != "" && time.Until(q.expires) > time.Minute {
		return q.token, nil
	}
	body, _ := json.Marshal(map[string]string{"appId": q.appID, "clientSecret": q.appSecret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, q.tokenURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := q.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("QQ access token 请求失败: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   string `json:"expires_in"`
		Message     string `json:"message"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&result); err != nil {
		return "", errors.New("QQ access token 响应无效")
	}
	if resp.StatusCode != http.StatusOK || result.AccessToken == "" {
		return "", fmt.Errorf("QQ 鉴权失败: HTTP %d %s", resp.StatusCode, result.Message)
	}
	seconds, err := strconv.ParseInt(result.ExpiresIn, 10, 64)
	if err != nil || seconds <= 60 {
		return "", errors.New("QQ access token 有效期无效")
	}
	q.token, q.expires = result.AccessToken, time.Now().Add(time.Duration(seconds)*time.Second)
	return q.token, nil
}

func (q *QQ) Send(ctx context.Context, a monitor.Alert) error {
	if q.targetType != "group" && q.targetType != "c2c" {
		return errors.New("QQ 目标类型必须为群聊或单聊")
	}
	if q.targetID == "" {
		return errors.New("请选择 QQ 目标会话或填写目标 OpenID")
	}
	token, err := q.accessToken(ctx)
	if err != nil {
		return err
	}
	endpoint := q.apiURL + "/v2/groups/" + url.PathEscape(q.targetID) + "/messages"
	if q.targetType == "c2c" {
		endpoint = q.apiURL + "/v2/users/" + url.PathEscape(q.targetID) + "/messages"
	}
	body, _ := json.Marshal(map[string]any{"content": alertText(a), "msg_type": 0})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "QQBot "+token)
	req.Header.Set("X-Union-Appid", q.appID)
	resp, err := q.client.Do(req)
	if err != nil {
		return fmt.Errorf("QQ 消息发送失败: %w", err)
	}
	defer resp.Body.Close()
	reply, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("QQ 消息发送失败: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(reply)))
	}
	return nil
}

type qqPayload struct {
	Op   int             `json:"op"`
	Seq  int64           `json:"s"`
	Type string          `json:"t"`
	Data json.RawMessage `json:"d"`
}

func (q *QQ) writeJSON(conn *websocket.Conn, value any) error {
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteJSON(value)
}

func (q *QQ) runOnce(ctx context.Context) error {
	token, err := q.accessToken(ctx)
	if err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, q.gatewayURL, nil)
	req.Header.Set("Authorization", "QQBot "+token)
	req.Header.Set("X-Union-Appid", q.appID)
	resp, err := q.client.Do(req)
	if err != nil {
		return fmt.Errorf("QQ 网关请求失败: %w", err)
	}
	var gateway struct {
		URL     string `json:"url"`
		Message string `json:"message"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&gateway)
	resp.Body.Close()
	if decodeErr != nil || resp.StatusCode != http.StatusOK || gateway.URL == "" {
		return fmt.Errorf("QQ 网关响应无效: HTTP %d %s", resp.StatusCode, gateway.Message)
	}
	conn, _, err := q.dialer.DialContext(ctx, gateway.URL, nil)
	if err != nil {
		return fmt.Errorf("QQ WebSocket 连接失败: %w", err)
	}
	defer conn.Close()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stop:
		}
	}()
	var hello struct {
		HeartbeatInterval float64 `json:"heartbeat_interval"`
	}
	for {
		var payload qqPayload
		if err := conn.ReadJSON(&payload); err != nil {
			return fmt.Errorf("QQ WebSocket 接收失败: %w", err)
		}
		if payload.Op == 10 {
			if err := json.Unmarshal(payload.Data, &hello); err != nil || hello.HeartbeatInterval <= 0 {
				return errors.New("QQ WebSocket Hello 数据无效")
			}
			break
		}
	}
	identity := map[string]any{"token": "QQBot " + token, "intents": qqGroupIntent, "shard": []int{0, 1}, "properties": map[string]string{"$os": "windows", "$browser": "service-monitor-agent", "$device": "service-monitor-agent"}}
	if err := q.writeJSON(conn, map[string]any{"op": 2, "d": identity}); err != nil {
		return err
	}
	heartbeatDone := make(chan struct{})
	var lastSeq atomic.Int64
	defer close(heartbeatDone)
	go func() {
		ticker := time.NewTicker(time.Duration(hello.HeartbeatInterval * float64(time.Millisecond)))
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				var sequence any
				if value := lastSeq.Load(); value > 0 {
					sequence = value
				}
				_ = q.writeJSON(conn, map[string]any{"op": 1, "d": sequence})
			case <-heartbeatDone:
				return
			}
		}
	}()
	for {
		var payload qqPayload
		if err := conn.ReadJSON(&payload); err != nil {
			return fmt.Errorf("QQ WebSocket 接收失败: %w", err)
		}
		if payload.Seq > 0 {
			lastSeq.Store(payload.Seq)
		}
		switch payload.Op {
		case 7:
			return errors.New("QQ WebSocket 服务端要求重连")
		case 9:
			return errors.New("QQ WebSocket 鉴权失败或会话已失效")
		case 0:
			switch payload.Type {
			case "READY":
				q.callbacks.ready()
			case "GROUP_AT_MESSAGE_CREATE":
				var event struct {
					GroupID string `json:"group_openid"`
				}
				if json.Unmarshal(payload.Data, &event) == nil && event.GroupID != "" {
					q.callbacks.target(Target{ID: event.GroupID, Name: "QQ 群聊", Kind: "group"})
				}
			case "C2C_MESSAGE_CREATE":
				var event struct {
					Author struct {
						OpenID string `json:"user_openid"`
					} `json:"author"`
				}
				if json.Unmarshal(payload.Data, &event) == nil && event.Author.OpenID != "" {
					q.callbacks.target(Target{ID: event.Author.OpenID, Name: "QQ 单聊", Kind: "c2c"})
				}
			}
		}
	}
}

// Run uses the legacy QQ WebSocket event transport. QQ has announced its
// retirement, so a bot without access to that transport will receive an error.
func (q *QQ) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		err := q.runOnce(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		q.callbacks.failed(err)
		timer := time.NewTimer(10 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return ctx.Err()
}
