package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"servicemonitoragent/internal/credentials"
	"servicemonitoragent/internal/monitor"
	"servicemonitoragent/internal/sampler"
	"servicemonitoragent/internal/sshclient"
)

type App struct {
	mu            sync.Mutex
	ctx           context.Context
	sessionCtx    context.Context
	sessionCancel context.CancelFunc
	dir           string
	store         *storage
	servers       []ServerInput
	projects      []Project
	settings      Settings
	privateKey    string
	analyses      []AnalysisResult
	metrics       map[string]Metric
	unlocked      bool
	initialized   bool
	monitor       *monitor.Scheduler
	bots          *botRuntime
	wakePoll      chan struct{}
}

func NewApp(dir string) (*App, error) {
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(base, "ServiceMonitorAgent")
	}
	store, err := openStorage(dir)
	if err != nil {
		return nil, err
	}
	app := &App{dir: dir, store: store, settings: Settings{}, metrics: map[string]Metric{}, projects: []Project{}, servers: []ServerInput{}, bots: newBotRuntime(), wakePoll: make(chan struct{}, 1)}
	if err = store.get("settings", &app.settings); err != nil {
		store.db.Close()
		return nil, err
	}
	if err = store.get("projects", &app.projects); err != nil {
		store.db.Close()
		return nil, err
	}
	if app.settings.PollSeconds <= 0 {
		app.settings.PollSeconds = 30
	}
	if app.settings.Threshold == 0 {
		app.settings.Threshold = 80
	}
	if app.settings.SpikeDelta == 0 {
		app.settings.SpikeDelta = 20
	}
	if app.settings.SustainedMinutes == 0 {
		app.settings.SustainedMinutes = 5
	}
	// Version 0 is the pre-toggle format: preserve existing behavior by enabling
	// channels that already have a webhook, then persist explicit choices later.
	if app.settings.NotificationsVersion == 0 {
		app.settings.FeishuEnabled = app.settings.FeishuWebhook != ""
		app.settings.QQEnabled = app.settings.QQWebhook != ""
	}
	if app.settings.NotificationsVersion < 2 {
		// Webhook configurations cannot be converted into official app credentials.
		// Disable both channels until the administrator supplies AppID/AppSecret.
		app.settings.FeishuEnabled = false
		app.settings.QQEnabled = false
		app.settings.NotificationsVersion = 2
	}
	if app.settings.ModelBaseURL == "" {
		app.settings.ModelBaseURL = app.settings.ModelEndpoint
	}
	if app.settings.ModelPlatform == "" {
		app.settings.ModelPlatform = "custom"
		if strings.Contains(app.settings.ModelBaseURL, "deepseek.com") {
			app.settings.ModelPlatform = "deepseek"
		}
	}
	_, err = os.Stat(app.vaultPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		store.db.Close()
		return nil, err
	}
	app.initialized = err == nil
	return app, nil
}
func (a *App) Close() {
	a.LockVault()
	a.bots.Close()
	a.store.db.Close()
}
func (a *App) startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
	go a.pollLoop(ctx)
}
func (a *App) shutdown(context.Context) {
	a.LockVault()
	a.bots.Close()
}
func (a *App) vaultPath() string { return filepath.Join(a.dir, "credentials.enc") }

func publicServer(s ServerInput) ServerInput { s.Password = ""; return s }
func (a *App) GetState() AppState {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := AppState{Initialized: a.initialized, Unlocked: a.unlocked, Servers: []ServerInput{}, Projects: append([]Project{}, a.projects...), Settings: a.settings, Metrics: []Metric{}, Alerts: []Alert{}, Notifications: a.bots.Snapshot()}
	out.Settings.ModelAPIKey = ""
	out.Settings.FeishuAppSecret = ""
	out.Settings.QQAppSecret = ""
	out.Settings.FeishuWebhook = ""
	out.Settings.QQWebhook = ""
	if a.unlocked {
		out.Analyses = append([]AnalysisResult{}, a.analyses...)
		for _, s := range a.servers {
			out.Servers = append(out.Servers, publicServer(s))
		}
	}
	for _, m := range a.metrics {
		out.Metrics = append(out.Metrics, m)
	}
	if a.monitor != nil {
		for server, states := range a.monitor.Snapshot().Hosts {
			for name, st := range states {
				if !st.ThresholdStarted.IsZero() {
					out.Alerts = append(out.Alerts, Alert{ID: server + "/" + name + "/threshold", ServerID: server, Kind: "threshold", Metric: name, Value: st.LastValue, Message: "使用率超过阈值", Active: true, Since: st.ThresholdStarted.Format(time.RFC3339)})
				}
				if !st.SpikeStarted.IsZero() && st.SpikeDelivered {
					out.Alerts = append(out.Alerts, Alert{ID: server + "/" + name + "/spike", ServerID: server, Kind: "spike", Metric: name, Value: st.LastValue, Message: "突增后持续超限", Active: true, Since: st.SpikeStarted.Format(time.RFC3339)})
				}
			}
		}
	}
	return out
}

func validateServer(s ServerInput) error {
	if strings.TrimSpace(s.ID) == "" || strings.TrimSpace(s.Name) == "" || strings.TrimSpace(s.Host) == "" || strings.TrimSpace(s.User) == "" {
		return errors.New("服务器 ID、名称、地址和 SSH 用户不能为空")
	}
	if strings.ContainsAny(s.Host, "/\r\n\x00 ") || s.Port < 1 || s.Port > 65535 {
		return errors.New("服务器地址或 SSH 端口无效")
	}
	return nil
}
func uniqueServers(servers []ServerInput) error {
	seen := map[string]bool{}
	for _, s := range servers {
		if err := validateServer(s); err != nil {
			return err
		}
		if seen[s.ID] {
			return errors.New("服务器 ID 重复")
		}
		seen[s.ID] = true
		if s.Password == "" {
			return errors.New("初始服务器 SSH 密码不能为空")
		}
	}
	return nil
}

func (a *App) InitializeVault(passphrase string, servers []ServerInput) error {
	if len([]rune(passphrase)) < 12 {
		return errors.New("管理员口令至少需要 12 个字符")
	}
	if err := uniqueServers(servers); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initialized {
		return errors.New("加密凭据文件已存在，不能重复初始化")
	}
	data, err := json.Marshal(servers)
	if err != nil {
		return err
	}
	if err = credentials.Save(a.vaultPath(), data, passphrase); err != nil {
		return err
	}
	a.initialized = true
	key, private, err := a.openPrivate(passphrase)
	if err != nil {
		return err
	}
	a.privateKey = key
	a.analyses = private.Analyses
	a.settings.FeishuAppSecret = private.FeishuAppSecret
	a.settings.QQAppSecret = private.QQAppSecret
	a.settings.ModelAPIKey = private.ModelAPIKey
	a.settings.FeishuSecretSet = private.FeishuAppSecret != ""
	a.settings.QQSecretSet = private.QQAppSecret != ""
	a.settings.ModelKeySet = private.ModelAPIKey != ""
	if a.sessionCancel != nil {
		a.sessionCancel()
	}
	a.sessionCtx, a.sessionCancel = context.WithCancel(context.Background())
	a.unlocked = true
	a.servers = append([]ServerInput{}, servers...)
	if err = a.configureMonitorLocked(); err != nil {
		return err
	}
	a.bots.Sync(a.settings)
	a.requestPoll()
	return nil
}
func (a *App) UnlockVault(passphrase string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.initialized {
		return errors.New("请先创建加密凭据文件")
	}
	data, err := credentials.Load(a.vaultPath(), passphrase)
	if err != nil {
		return fmt.Errorf("解锁失败: %w", err)
	}
	var servers []ServerInput
	if err = json.Unmarshal(data, &servers); err != nil {
		return errors.New("凭据文件内容无效")
	}
	if err = uniqueServers(servers); err != nil {
		return err
	}
	key, private, err := a.openPrivate(passphrase)
	if err != nil {
		return fmt.Errorf("解锁加密配置失败: %w", err)
	}
	if a.sessionCancel != nil {
		a.sessionCancel()
	}
	a.sessionCtx, a.sessionCancel = context.WithCancel(context.Background())
	a.servers = servers
	a.privateKey = key
	a.analyses = private.Analyses
	a.settings.FeishuAppSecret = private.FeishuAppSecret
	a.settings.QQAppSecret = private.QQAppSecret
	a.settings.ModelAPIKey = private.ModelAPIKey
	a.settings.FeishuSecretSet = private.FeishuAppSecret != ""
	a.settings.QQSecretSet = private.QQAppSecret != ""
	a.settings.ModelKeySet = private.ModelAPIKey != ""
	a.unlocked = true
	if err = a.configureMonitorLocked(); err != nil {
		return err
	}
	a.bots.Sync(a.settings)
	a.requestPoll()
	return nil
}
func (a *App) LockVault() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sessionCancel != nil {
		a.sessionCancel()
		a.sessionCancel = nil
		a.sessionCtx = nil
	}
	a.unlocked = false
	a.servers = nil
	a.privateKey = ""
	a.analyses = nil
	a.settings.FeishuAppSecret = ""
	a.settings.QQAppSecret = ""
	a.settings.ModelAPIKey = ""
	a.settings.FeishuSecretSet = false
	a.settings.QQSecretSet = false
	a.settings.ModelKeySet = false
	a.metrics = map[string]Metric{}
	a.monitor = nil
	a.bots.Sync(Settings{})
}
func (a *App) UpsertServer(server ServerInput, passphrase string) error {
	if err := validateServer(server); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.unlocked {
		return errors.New("请先解锁凭据")
	}
	// Verify the administrator passphrase for each credential write; it is not retained by the app.
	if _, err := credentials.Load(a.vaultPath(), passphrase); err != nil {
		return errors.New("管理员口令错误")
	}
	updated := append([]ServerInput{}, a.servers...)
	found := false
	for i, s := range updated {
		if s.ID == server.ID {
			if server.Password == "" {
				server.Password = s.Password
			}
			updated[i] = server
			found = true
			break
		}
	}
	if !found {
		if server.Password == "" {
			return errors.New("新服务器需要 SSH 密码")
		}
		updated = append(updated, server)
	}
	data, err := json.Marshal(updated)
	if err != nil {
		return err
	}
	if err = credentials.Save(a.vaultPath(), data, passphrase); err != nil {
		return err
	}
	a.servers = updated
	return nil
}
func (a *App) DeleteServer(id, passphrase string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.unlocked {
		return errors.New("请先解锁凭据")
	}
	if _, err := credentials.Load(a.vaultPath(), passphrase); err != nil {
		return errors.New("管理员口令错误")
	}
	updated := make([]ServerInput, 0, len(a.servers))
	for _, s := range a.servers {
		if s.ID != id {
			updated = append(updated, s)
		}
	}
	if len(updated) == len(a.servers) {
		return errors.New("服务器不存在")
	}
	data, err := json.Marshal(updated)
	if err != nil {
		return err
	}
	if err = credentials.Save(a.vaultPath(), data, passphrase); err != nil {
		return err
	}
	a.servers = updated
	delete(a.metrics, id)
	return nil
}
func (a *App) client(id string) (*sshclient.Client, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.unlocked {
		return nil, errors.New("请先解锁凭据")
	}
	for _, s := range a.servers {
		if s.ID == id {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			return sshclient.New(net.JoinHostPort(s.Host, fmt.Sprint(s.Port)), s.User, s.Password, filepath.Join(home, ".ssh", "known_hosts"))
		}
	}
	return nil, errors.New("服务器不存在")
}
func (a *App) TestConnection(id string) (string, error) {
	c, err := a.client(id)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	session := a.sessionCtx
	a.mu.Unlock()
	if session == nil {
		return "", errors.New("凭据已锁定")
	}
	ctx, cancel := context.WithTimeout(session, 15*time.Second)
	defer cancel()
	_, err = c.Run(ctx, "printf connected")
	if err != nil {
		return "", err
	}
	// A successful probe establishes connectivity; immediately collect this host
	// so the management page can replace "not collected" with a real state.
	a.collect(id)
	return "SSH 连接成功，服务器身份已通过 known_hosts 校验", nil
}
func (a *App) RefreshMetrics() ([]Metric, error) {
	a.mu.Lock()
	if !a.unlocked {
		a.mu.Unlock()
		return nil, errors.New("请先解锁凭据")
	}
	servers := append([]ServerInput{}, a.servers...)
	a.mu.Unlock()
	out := make([]Metric, len(servers))
	var group sync.WaitGroup
	limit := make(chan struct{}, 4)
	for i, server := range servers {
		group.Add(1)
		go func(i int, id string) {
			defer group.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			out[i] = a.collect(id)
		}(i, server.ID)
	}
	group.Wait()
	return out, nil
}
func (a *App) collect(id string) Metric {
	m := Metric{ServerID: id, Timestamp: nowISO(), Disks: []DiskMetric{}}
	client, err := a.client(id)
	if err == nil {
		a.mu.Lock()
		session := a.sessionCtx
		a.mu.Unlock()
		if session == nil {
			m.Error = "凭据已锁定"
			return m
		}
		ctx, cancel := context.WithTimeout(session, 25*time.Second)
		defer cancel()
		var sample sampler.Metrics
		sample, err = sampler.Collect(ctx, client)
		if err == nil {
			m.CPU = sample.CPUPercent
			m.Memory = sample.MemoryPercent
			for _, d := range sample.Disks {
				m.Disks = append(m.Disks, DiskMetric{Mount: d.Mount, Percent: d.Percent})
			}
			a.mu.Lock()
			schedule := a.monitor
			a.mu.Unlock()
			if schedule != nil {
				ctx2, cancel2 := context.WithTimeout(ctx, 10*time.Second)
				defer cancel2()
				if alertErr := schedule.Evaluate(ctx2, id, sample, sample.CollectedAt); alertErr != nil {
					m.NotificationError = "告警发送失败: " + alertErr.Error()
				}
			}
		}
	}
	if err != nil {
		m.Error = err.Error()
	}
	a.mu.Lock()
	if a.unlocked {
		a.metrics[id] = m
	}
	a.mu.Unlock()
	return m
}
func (a *App) requestPoll() {
	select {
	case a.wakePoll <- struct{}{}:
	default:
	}
}
func (a *App) pollLoop(ctx context.Context) {
	for {
		a.mu.Lock()
		seconds := a.settings.PollSeconds
		unlocked := a.unlocked
		a.mu.Unlock()
		if seconds < 5 {
			seconds = 30
		}
		if unlocked {
			_, _ = a.RefreshMetrics()
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, "metrics-updated")
			}
		}
		timer := time.NewTimer(time.Duration(seconds) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-a.wakePoll:
			timer.Stop()
		case <-timer.C:
		}
	}
}
func (a *App) configureMonitorLocked() error {
	cfg := monitor.Config{Threshold: a.settings.Threshold, SpikeDelta: a.settings.SpikeDelta, SpikeDuration: time.Duration(a.settings.SustainedMinutes) * time.Minute, Interval: time.Duration(a.settings.PollSeconds) * time.Second}
	channel, err := makeNotifier(a.settings)
	if err != nil {
		return err
	}
	sched, err := monitor.New(cfg, channel, filepath.Join(a.dir, "alerts.json"))
	if err != nil {
		return err
	}
	a.monitor = sched
	return nil
}
