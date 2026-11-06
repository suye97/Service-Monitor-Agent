package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"servicemonitoragent/internal/monitor"
	"servicemonitoragent/internal/notifier"
)

func (a *App) SaveProject(project Project) error {
	if project.ID == "" || strings.TrimSpace(project.Name) == "" || project.ServerID == "" || project.ProjectPath == "" || project.LogPath == "" {
		return errors.New("请填写项目 ID、名称、服务器和两项路径")
	}
	if _, err := validateLinuxPath(project.ProjectPath); err != nil {
		return err
	}
	if _, err := validateLinuxPath(project.LogPath); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	updated := append([]Project{}, a.projects...)
	found := false
	for i, p := range updated {
		if p.ID == project.ID {
			updated[i] = project
			found = true
			break
		}
	}
	if !found {
		updated = append(updated, project)
	}
	if err := a.store.put("projects", updated); err != nil {
		return err
	}
	a.projects = updated
	return nil
}
func (a *App) DeleteProject(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Project, 0, len(a.projects))
	for _, p := range a.projects {
		if p.ID != id {
			out = append(out, p)
		}
	}
	if len(out) == len(a.projects) {
		return errors.New("项目不存在")
	}
	if err := a.store.put("projects", out); err != nil {
		return err
	}
	a.projects = out
	return nil
}
func validateLinuxPath(s string) (string, error) {
	if !strings.HasPrefix(s, "/") || path.Clean(s) != s || strings.ContainsAny(s, "\x00\r\n") {
		return "", errors.New("项目路径须为 Linux 绝对路径")
	}
	return s, nil
}

func (a *App) SaveSettings(settings Settings) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.unlocked {
		return errors.New("请先解锁保险库")
	}
	if settings.PollSeconds < 5 || settings.PollSeconds > 3600 || settings.Threshold <= 0 || settings.Threshold > 100 || settings.SpikeDelta <= 0 || settings.SpikeDelta > 100 || settings.SustainedMinutes < 1 || settings.SustainedMinutes > 1440 {
		return errors.New("监控周期、阈值或持续时长超出允许范围")
	}
	settings.NotificationsVersion = 2
	if settings.FeishuEnabled && settings.FeishuAppID == "" {
		return errors.New("启用飞书通知前请配置 AppID 和 AppSecret")
	}
	if settings.QQEnabled && settings.QQAppID == "" {
		return errors.New("启用 QQ 通知前请配置 AppID 和 AppSecret")
	}
	if settings.QQTargetType == "" {
		settings.QQTargetType = "group"
	}
	settings.FeishuWebhook = ""
	settings.QQWebhook = ""
	if settings.ModelBaseURL == "" {
		settings.ModelBaseURL = settings.ModelEndpoint
	}
	if settings.ModelPlatform == "" {
		settings.ModelPlatform = "custom"
	}
	if settings.ModelBaseURL != "" {
		if _, err := modelAPIURL(settings.ModelBaseURL, "chat/completions"); err != nil {
			return err
		}
	}
	settings.ModelEndpoint = ""
	if settings.ModelAPIKey == "" {
		settings.ModelAPIKey = a.settings.ModelAPIKey
	}
	if settings.FeishuAppSecret == "" && settings.FeishuAppID == a.settings.FeishuAppID {
		settings.FeishuAppSecret = a.settings.FeishuAppSecret
	}
	if settings.QQAppSecret == "" && settings.QQAppID == a.settings.QQAppID {
		settings.QQAppSecret = a.settings.QQAppSecret
	}
	if settings.FeishuEnabled && settings.FeishuAppSecret == "" {
		return errors.New("启用飞书通知前请输入 AppSecret")
	}
	if settings.QQEnabled && settings.QQAppSecret == "" {
		return errors.New("启用 QQ 通知前请输入 AppSecret")
	}
	if settings.FeishuAppID == "" {
		settings.FeishuAppSecret = ""
	}
	if settings.QQAppID == "" {
		settings.QQAppSecret = ""
	}
	settings.FeishuSecretSet = settings.FeishuAppSecret != ""
	settings.QQSecretSet = settings.QQAppSecret != ""
	settings.ModelKeySet = settings.ModelAPIKey != ""
	public := settings
	public.ModelAPIKey = ""
	public.ModelKeySet = false
	public.FeishuAppSecret = ""
	public.FeishuSecretSet = false
	public.QQAppSecret = ""
	public.QQSecretSet = false
	if settings.FeishuSecretSet || settings.QQSecretSet || settings.ModelKeySet || a.privateKey != "" {
		if err := a.savePrivate(a.privateKey, settings, a.analyses); err != nil {
			return err
		}
	}
	if err := a.store.put("settings", public); err != nil {
		if a.privateKey != "" {
			_ = a.savePrivate(a.privateKey, a.settings, a.analyses)
		}
		return err
	}
	a.settings = settings
	if a.unlocked {
		if err := a.configureMonitorLocked(); err != nil {
			return err
		}
	}
	a.bots.Sync(settings)
	return nil
}

type namedNotifier struct {
	name     string
	notifier monitor.Notifier
}
type multiNotifier []namedNotifier

func (n multiNotifier) ChannelNames() []string {
	names := make([]string, len(n))
	for i, channel := range n {
		names[i] = channel.name
	}
	return names
}
func (n multiNotifier) SendChannel(ctx context.Context, name string, alert monitor.Alert) error {
	for _, channel := range n {
		if channel.name == name {
			return channel.notifier.Send(ctx, alert)
		}
	}
	return errors.New("通知渠道不存在: " + name)
}
func (n multiNotifier) Send(ctx context.Context, alert monitor.Alert) error {
	var failures []error
	for _, channel := range n {
		if err := channel.notifier.Send(ctx, alert); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", channel.name, err))
		}
	}
	return errors.Join(failures...)
}
func makeNotifier(s Settings) (monitor.Notifier, error) {
	var channels multiNotifier
	if s.FeishuEnabled && s.FeishuAppID != "" && s.FeishuAppSecret != "" && s.FeishuTargetID != "" {
		n, err := notifier.NewFeishu(s.FeishuAppID, s.FeishuAppSecret, s.FeishuTargetID, notifier.Callbacks{})
		if err != nil {
			return nil, err
		}
		channels = append(channels, namedNotifier{name: "feishu", notifier: n})
	}
	if s.QQEnabled && s.QQAppID != "" && s.QQAppSecret != "" && s.QQTargetID != "" {
		n, err := notifier.NewQQ(s.QQAppID, s.QQAppSecret, s.QQTargetType, s.QQTargetID, notifier.Callbacks{})
		if err != nil {
			return nil, err
		}
		channels = append(channels, namedNotifier{name: "qq", notifier: n})
	}
	if len(channels) == 0 {
		return nil, nil
	}
	return channels, nil
}
func (a *App) TestNotification(channel string, submitted Settings) error {
	a.mu.Lock()
	settings := a.settings
	a.mu.Unlock()
	if submitted.FeishuAppSecret == "" && submitted.FeishuAppID == settings.FeishuAppID {
		submitted.FeishuAppSecret = settings.FeishuAppSecret
	}
	if submitted.QQAppSecret == "" && submitted.QQAppID == settings.QQAppID {
		submitted.QQAppSecret = settings.QQAppSecret
	}
	if submitted.QQTargetType == "" {
		submitted.QQTargetType = "group"
	}
	var n monitor.Notifier
	var err error
	switch channel {
	case "feishu":
		n, err = notifier.NewFeishu(submitted.FeishuAppID, submitted.FeishuAppSecret, submitted.FeishuTargetID, notifier.Callbacks{})
	case "qq":
		n, err = notifier.NewQQ(submitted.QQAppID, submitted.QQAppSecret, submitted.QQTargetType, submitted.QQTargetID, notifier.Callbacks{})
	default:
		return errors.New("未知通知渠道")
	}
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	return n.Send(ctx, monitor.Alert{Host: "test", Metric: "connection", Kind: "test", StartedAt: time.Now(), Value: 0})
}
func (a *App) AnalyzeLog(projectID, question string, history []AnalysisTurn) (AnalysisResult, error) {
	var result AnalysisResult
	if strings.TrimSpace(question) == "" || len([]rune(question)) > 2000 {
		return result, errors.New("请输入 1 到 2000 字的问题")
	}
	a.mu.Lock()
	var project Project
	found := false
	for _, p := range a.projects {
		if p.ID == projectID {
			project = p
			found = true
			break
		}
	}
	settings := a.settings
	session := a.sessionCtx
	a.mu.Unlock()
	if session == nil {
		return result, errors.New("请先解锁凭据")
	}
	if !found {
		return result, errors.New("项目不存在")
	}
	client, err := a.client(project.ServerID)
	if err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(session, 60*time.Second)
	defer cancel()
	log, err := readLog(ctx, client, project.LogPath)
	if err != nil {
		return result, err
	}
	answer, err := generateAnalysis(ctx, settings, question, log, history)
	if err != nil {
		return result, err
	}
	excerpt := log
	if len(excerpt) > 8000 {
		excerpt = excerpt[len(excerpt)-8000:]
	}
	result = AnalysisResult{ProjectID: project.ID, Question: question, Answer: answer, GeneratedAt: nowISO(), Source: project.ServerID + ":" + project.LogPath, LogExcerpt: excerpt}
	if err = a.recordAnalysis(session, result); err != nil {
		return AnalysisResult{}, err
	}
	return result, nil
}
func (a *App) ExportAnalysis(result AnalysisResult, destination string) (string, error) {
	if strings.TrimSpace(result.Answer) == "" || strings.TrimSpace(result.Question) == "" {
		return "", errors.New("没有可导出的分析结果")
	}
	if destination == "" {
		a.mu.Lock()
		ctx := a.ctx
		a.mu.Unlock()
		if ctx == nil {
			return "", errors.New("桌面保存对话框不可用")
		}
		var err error
		destination, err = runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{Title: "导出分析报告", DefaultFilename: "日志分析.md", Filters: []runtime.FileFilter{{DisplayName: "Markdown 文件", Pattern: "*.md"}}})
		if err != nil {
			return "", err
		}
		if destination == "" {
			return "", errors.New("已取消导出")
		}
	}
	if filepath.Ext(destination) != ".md" {
		destination += ".md"
	}
	if _, err := os.Stat(destination); err == nil {
		return "", errors.New("目标文件已存在，请选择其他文件名")
	}
	content := fmt.Sprintf("# 日志分析报告\n\n- 项目：%s\n- 时间：%s\n- 日志：%s\n\n## 问题\n\n%s\n\n## 分析\n\n%s\n\n## 日志摘录\n\n```text\n%s\n```\n", result.ProjectID, result.GeneratedAt, result.Source, result.Question, result.Answer, strings.ReplaceAll(result.LogExcerpt, "```", "` ` `"))
	f, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err = f.WriteString(content); err != nil {
		os.Remove(destination)
		return "", err
	}
	return destination, nil
}
