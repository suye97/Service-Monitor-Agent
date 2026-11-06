package main

import (
	"context"
	"sort"
	"sync"

	"servicemonitoragent/internal/notifier"
)

type botState struct {
	status     string
	message    string
	targets    map[string]BotTarget
	cancel     context.CancelFunc
	generation uint64
}

type botRuntime struct {
	mu       sync.Mutex
	channels map[string]*botState
}

func newBotRuntime() *botRuntime {
	return &botRuntime{channels: map[string]*botState{
		"feishu": {status: "disabled", targets: map[string]BotTarget{}},
		"qq":     {status: "disabled", targets: map[string]BotTarget{}},
	}}
}

func (r *botRuntime) Snapshot() []NotificationStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]NotificationStatus, 0, len(r.channels))
	for name, state := range r.channels {
		item := NotificationStatus{Channel: name, Status: state.status, Message: state.message, Targets: []BotTarget{}}
		for _, target := range state.targets {
			item.Targets = append(item.Targets, target)
		}
		sort.Slice(item.Targets, func(i, j int) bool { return item.Targets[i].ID < item.Targets[j].ID })
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Channel < out[j].Channel })
	return out
}

func (r *botRuntime) update(channel string, generation uint64, status, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if state := r.channels[channel]; state != nil && state.generation == generation {
		state.status, state.message = status, message
	}
}

func (r *botRuntime) target(channel string, generation uint64, target notifier.Target) {
	if target.ID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if state := r.channels[channel]; state != nil && state.generation == generation {
		state.targets[target.ID] = BotTarget{ID: target.ID, Name: target.Name, Kind: target.Kind}
	}
}

func (r *botRuntime) Sync(settings Settings) {
	r.mu.Lock()
	for _, state := range r.channels {
		if state.cancel != nil {
			state.cancel()
			state.cancel = nil
		}
		state.generation++
		state.status, state.message = "disabled", ""
	}
	generations := map[string]uint64{"feishu": r.channels["feishu"].generation, "qq": r.channels["qq"].generation}
	r.mu.Unlock()
	if settings.FeishuEnabled {
		generation := generations["feishu"]
		callbacks := notifier.Callbacks{
			Ready:  func() { r.update("feishu", generation, "online", "WebSocket 已连接") },
			Failed: func(err error) { r.update("feishu", generation, "error", err.Error()) },
			Target: func(target notifier.Target) { r.target("feishu", generation, target) },
		}
		client, err := notifier.NewFeishu(settings.FeishuAppID, settings.FeishuAppSecret, settings.FeishuTargetID, callbacks)
		if err != nil {
			r.update("feishu", generation, "error", err.Error())
		} else {
			r.start("feishu", generation, client)
		}
	}
	if settings.QQEnabled {
		generation := generations["qq"]
		callbacks := notifier.Callbacks{
			Ready:  func() { r.update("qq", generation, "online", "QQ Gateway WebSocket 已连接") },
			Failed: func(err error) { r.update("qq", generation, "error", err.Error()) },
			Target: func(target notifier.Target) { r.target("qq", generation, target) },
		}
		client, err := notifier.NewQQ(settings.QQAppID, settings.QQAppSecret, settings.QQTargetType, settings.QQTargetID, callbacks)
		if err != nil {
			r.update("qq", generation, "error", err.Error())
		} else {
			r.start("qq", generation, client)
		}
	}
}

type runningNotifier interface {
	Run(context.Context) error
}

func (r *botRuntime) start(channel string, generation uint64, client runningNotifier) {
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	state := r.channels[channel]
	if state.generation != generation {
		r.mu.Unlock()
		cancel()
		return
	}
	state.cancel, state.status, state.message = cancel, "connecting", "正在建立 WebSocket 连接"
	r.mu.Unlock()
	go func() {
		err := client.Run(ctx)
		if ctx.Err() == nil {
			message := "连接已断开"
			if err != nil {
				message = err.Error()
			}
			r.update(channel, generation, "error", message)
		}
	}()
}

func (r *botRuntime) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, state := range r.channels {
		if state.cancel != nil {
			state.cancel()
			state.cancel = nil
		}
	}
}
