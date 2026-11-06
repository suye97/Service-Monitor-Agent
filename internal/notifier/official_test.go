package notifier

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	"servicemonitoragent/internal/monitor"
)

func TestQQWebSocketDiscoversGroupAndC2CTargets(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/getAppAccessToken":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token", "expires_in": "7200"})
		case "/wss":
			_ = json.NewEncoder(w).Encode(map[string]string{"url": "ws" + strings.TrimPrefix(server.URL, "http") + "/socket"})
		case "/socket":
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			_ = conn.WriteJSON(map[string]any{"op": 10, "d": map[string]float64{"heartbeat_interval": 45000}})
			var identify qqPayload
			if err := conn.ReadJSON(&identify); err != nil || identify.Op != 2 {
				return
			}
			var identity struct {
				Token   string `json:"token"`
				Intents int    `json:"intents"`
			}
			if json.Unmarshal(identify.Data, &identity) != nil || identity.Token != "QQBot token" || identity.Intents != qqGroupIntent {
				return
			}
			_ = conn.WriteJSON(map[string]any{"op": 0, "t": "READY", "s": 1, "d": map[string]string{"session_id": "session"}})
			_ = conn.WriteJSON(map[string]any{"op": 0, "t": "GROUP_AT_MESSAGE_CREATE", "s": 2, "d": map[string]string{"group_openid": "group-openid"}})
			_ = conn.WriteJSON(map[string]any{"op": 0, "t": "C2C_MESSAGE_CREATE", "s": 3, "d": map[string]any{"author": map[string]string{"user_openid": "user-openid"}}})
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ready := make(chan struct{}, 1)
	targets := make(chan Target, 2)
	q, err := NewQQ("app-id", "secret", "", "", Callbacks{Ready: func() { ready <- struct{}{} }, Target: func(target Target) { targets <- target }})
	if err != nil {
		t.Fatal(err)
	}
	q.client, q.tokenURL, q.gatewayURL = server.Client(), server.URL+"/app/getAppAccessToken", server.URL+"/wss"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- q.runOnce(ctx) }()
	isReady := false
	received := map[string]Target{}
	for !isReady || received["group"].ID == "" || received["c2c"].ID == "" {
		select {
		case <-ready:
			isReady = true
		case target := <-targets:
			received[target.Kind] = target
		case err := <-done:
			t.Fatalf("QQ WebSocket exited before verification: %v", err)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for QQ events: ready=%v targets=%v", isReady, received)
		}
	}
	if received["group"].ID != "group-openid" || received["c2c"].ID != "user-openid" {
		t.Fatalf("unexpected targets: %v", received)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("QQ WebSocket did not stop after cancellation")
	}
}

func TestFeishuOfficialMessagePayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "ok", "tenant_access_token": "tenant-token", "expire": 7200})
			return
		}
		if r.URL.Path != "/open-apis/im/v1/messages" || r.URL.Query().Get("receive_id_type") != "chat_id" {
			t.Errorf("unexpected Feishu endpoint: %s", r.URL.String())
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["receive_id"] != "chat-id" || body["msg_type"] != "text" || body["content"] == "" {
			t.Errorf("unexpected Feishu payload: %v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "success", "data": map[string]string{"message_id": "message-id"}})
	}))
	defer server.Close()
	f, err := NewFeishu("app-id", "secret", "chat-id", Callbacks{})
	if err != nil {
		t.Fatal(err)
	}
	f.client = lark.NewClient("app-id", "secret", lark.WithOpenBaseUrl(server.URL))
	if err := f.Send(context.Background(), monitor.Alert{Host: "host", Metric: "cpu", Value: 82}); err != nil {
		t.Fatal(err)
	}
}
func TestQQUsesCurrentGatewayEndpoint(t *testing.T) {
	q, err := NewQQ("app-id", "secret", "", "", Callbacks{})
	if err != nil {
		t.Fatal(err)
	}
	if q.gatewayURL != "https://api.sgroup.qq.com/gateway" {
		t.Fatalf("unexpected QQ gateway endpoint: %s", q.gatewayURL)
	}
}

func TestQQOfficialTokenAndC2CDelivery(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/app/getAppAccessToken":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["appId"] != "app-id" || body["clientSecret"] != "secret" {
				t.Errorf("unexpected credentials: %v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token", "expires_in": "7200"})
		case "/v2/users/user-openid/messages":
			if r.Header.Get("Authorization") != "QQBot token" || r.Header.Get("X-Union-Appid") != "app-id" {
				t.Errorf("missing official authorization: %v", r.Header)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["msg_type"] != float64(0) || body["content"] == "" {
				t.Errorf("unexpected message: %v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "message-id"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	q, err := NewQQ("app-id", "secret", "c2c", "user-openid", Callbacks{})
	if err != nil {
		t.Fatal(err)
	}
	q.client, q.tokenURL, q.apiURL = server.Client(), server.URL+"/app/getAppAccessToken", server.URL
	alert := monitor.Alert{Host: "host", Metric: "cpu", Kind: "threshold", Value: 82, StartedAt: time.Now()}
	if err := q.Send(context.Background(), alert); err != nil {
		t.Fatal(err)
	}
	if err := q.Send(context.Background(), alert); err != nil {
		t.Fatal(err)
	}
	if requests != 3 {
		t.Fatalf("token was not cached: got %d requests", requests)
	}
}

func TestQQRejectsInvalidTargetAndSurfacesDeliveryErrors(t *testing.T) {
	q, err := NewQQ("app-id", "secret", "", "", Callbacks{})
	if err != nil {
		t.Fatalf("connection configuration should not require a target: %v", err)
	}
	if err := q.Send(context.Background(), monitor.Alert{}); err == nil {
		t.Fatal("send accepted an empty QQ target type")
	}
	q.targetType = "c2c"
	if err := q.Send(context.Background(), monitor.Alert{}); err == nil {
		t.Fatal("send accepted an empty QQ OpenID")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/getAppAccessToken" {
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token", "expires_in": "7200"})
			return
		}
		http.Error(w, "active message quota exceeded", http.StatusForbidden)
	}))
	defer server.Close()
	q, err = NewQQ("app-id", "secret", "group", "group-openid", Callbacks{})
	if err != nil {
		t.Fatal(err)
	}
	q.client, q.tokenURL, q.apiURL = server.Client(), server.URL+"/app/getAppAccessToken", server.URL
	if err := q.Send(context.Background(), monitor.Alert{}); err == nil {
		t.Fatal("QQ API rejection was not returned")
	}
}
