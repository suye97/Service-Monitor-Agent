package main

import (
	"os"
	"strings"
	"testing"
)

func TestMakeNotifierHonorsChannelSwitches(t *testing.T) {
	settings := Settings{
		FeishuAppID: "feishu-id", FeishuAppSecret: "feishu-secret", FeishuTargetID: "chat-id",
		QQAppID: "qq-id", QQAppSecret: "qq-secret", QQTargetType: "group", QQTargetID: "group-id",
	}
	channel, err := makeNotifier(settings)
	if err != nil {
		t.Fatal(err)
	}
	if channel != nil {
		t.Fatal("disabled channels produced a notifier")
	}
	settings.FeishuEnabled = true
	channel, err = makeNotifier(settings)
	if err != nil {
		t.Fatal(err)
	}
	channels, ok := channel.(interface{ ChannelNames() []string })
	if !ok {
		t.Fatal("multi-channel notifier does not expose channel names")
	}
	names := channels.ChannelNames()
	if len(names) != 1 || names[0] != "feishu" {
		t.Fatalf("enabled channels: %v", names)
	}
}

func TestLegacyWebhookSettingsRequireOfficialCredentials(t *testing.T) {
	dir := t.TempDir()
	store, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.put("settings", map[string]any{
		"feishuWebhook": "https://example.com/legacy",
		"qqWebhook":     "https://example.com/disabled-by-default",
	}); err != nil {
		t.Fatal(err)
	}
	if err = store.db.Close(); err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	settings := app.GetState().Settings
	if settings.FeishuEnabled || settings.QQEnabled || settings.NotificationsVersion != 2 {
		t.Fatalf("legacy webhook channels must be disabled until official app credentials are configured: %+v", settings)
	}
	if settings.FeishuWebhook != "" || settings.QQWebhook != "" {
		t.Fatal("legacy webhook secrets were exposed in application state")
	}
	if settings.Threshold != 80 || settings.SpikeDelta != 20 || settings.SustainedMinutes != 5 {
		t.Fatalf("default monitoring settings were not restored: %+v", settings)
	}
}

func TestEnabledChannelRequiresOfficialCredentials(t *testing.T) {
	app, err := NewApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if err = app.InitializeVault("test-passphrase-123", nil); err != nil {
		t.Fatal(err)
	}
	settings := defaultSettings()
	settings.FeishuEnabled = true
	if err = app.SaveSettings(settings); err == nil || !strings.Contains(err.Error(), "AppID") {
		t.Fatalf("enabled channel without AppID was accepted: %v", err)
	}
}

func TestNotificationUsesCurrentFormCredentials(t *testing.T) {
	app, err := NewApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	app.settings.QQAppID, app.settings.QQAppSecret = "old-id", "old-secret"
	submitted := Settings{QQAppID: "new-id", QQAppSecret: "new-secret", QQTargetType: "invalid", QQTargetID: "target"}
	err = app.TestNotification("qq", submitted)
	if err == nil || !strings.Contains(err.Error(), "目标类型") {
		t.Fatalf("current form credentials were not used: %v", err)
	}
	submitted.QQAppID, submitted.QQAppSecret = "old-id", ""
	err = app.TestNotification("qq", submitted)
	if err == nil || !strings.Contains(err.Error(), "目标类型") {
		t.Fatalf("saved secret was not retained for the same AppID: %v", err)
	}
	submitted.QQAppID = "different-id"
	err = app.TestNotification("qq", submitted)
	if err == nil || !strings.Contains(err.Error(), "请填写") {
		t.Fatalf("old secret was incorrectly reused for another AppID: %v", err)
	}
}

func TestSecretsAndAnalysesSurviveRestartWithoutExposure(t *testing.T) {
	dir := t.TempDir()
	const password = "testing-vault-password-123"
	app, err := NewApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.InitializeVault(password, nil); err != nil {
		t.Fatal(err)
	}
	settings := defaultSettings()
	settings.FeishuAppID, settings.FeishuAppSecret = "feishu-id", "feishu-secret"
	settings.QQAppID, settings.QQAppSecret, settings.QQTargetType = "qq-id", "qq-secret", "group"
	settings.ModelAPIKey = "model-secret"
	if err = app.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	saved := AnalysisResult{ProjectID: "project-1", Question: "发生了什么？", Answer: "请排查超时", GeneratedAt: nowISO(), LogExcerpt: "sensitive log excerpt"}
	if err = app.recordAnalysis(app.sessionCtx, saved); err != nil {
		t.Fatal(err)
	}
	assertRedacted := func(state AppState) {
		t.Helper()
		if state.Settings.FeishuAppSecret != "" || state.Settings.QQAppSecret != "" || state.Settings.ModelAPIKey != "" {
			t.Fatal("secret was exposed through GetState")
		}
		if !state.Settings.FeishuSecretSet || !state.Settings.QQSecretSet || !state.Settings.ModelKeySet {
			t.Fatal("configured-secret indicators were not preserved")
		}
	}
	assertRedacted(app.GetState())
	var stored Settings
	if err = app.store.get("settings", &stored); err != nil {
		t.Fatal(err)
	}
	if stored.FeishuAppSecret != "" || stored.QQAppSecret != "" || stored.ModelAPIKey != "" {
		t.Fatal("secret was persisted in SQLite plaintext")
	}
	for _, filename := range []string{"private-key.enc", "private-data.enc"} {
		blob, readErr := os.ReadFile(dir + string(os.PathSeparator) + filename)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, secret := range []string{"feishu-secret", "qq-secret", "model-secret", saved.LogExcerpt} {
			if strings.Contains(string(blob), secret) {
				t.Fatalf("%s contains plaintext %q", filename, secret)
			}
		}
	}
	app.Close()

	reopened, err := NewApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	locked := reopened.GetState()
	if locked.Unlocked || len(locked.Analyses) != 0 || locked.Settings.ModelKeySet {
		t.Fatal("locked application leaked private data")
	}
	if err = reopened.UnlockVault("incorrect-passphrase"); err == nil {
		t.Fatal("incorrect password unlocked private data")
	}
	if err = reopened.UnlockVault(password); err != nil {
		t.Fatal(err)
	}
	state := reopened.GetState()
	assertRedacted(state)
	if len(state.Analyses) != 1 || state.Analyses[0] != saved {
		t.Fatalf("analysis record was not restored: %+v", state.Analyses)
	}
	if reopened.settings.FeishuAppSecret != "feishu-secret" || reopened.settings.QQAppSecret != "qq-secret" || reopened.settings.ModelAPIKey != "model-secret" {
		t.Fatal("encrypted settings were not restored")
	}
	if err = reopened.SaveSettings(state.Settings); err != nil {
		t.Fatal(err)
	}
	if reopened.settings.ModelAPIKey != "model-secret" || reopened.settings.FeishuAppSecret != "feishu-secret" {
		t.Fatal("blank key form failed to retain saved keys")
	}
	reopened.LockVault()
	locked = reopened.GetState()
	if locked.Unlocked || len(locked.Analyses) != 0 || locked.Settings.ModelKeySet || locked.Settings.FeishuSecretSet || locked.Settings.QQSecretSet {
		t.Fatal("locking did not clear private state")
	}
	if err = reopened.SaveSettings(settings); err == nil {
		t.Fatal("settings could be saved while locked")
	}
	if err = reopened.recordAnalysis(app.sessionCtx, saved); err == nil {
		t.Fatal("old session could save an analysis after locking")
	}
	if err = reopened.UnlockVault(password); err != nil {
		t.Fatal(err)
	}
	changed := reopened.GetState().Settings
	changed.FeishuAppID = "new-feishu-id"
	changed.FeishuEnabled = true
	if err = reopened.SaveSettings(changed); err == nil || !strings.Contains(err.Error(), "AppSecret") {
		t.Fatalf("old secret was reused with a different AppID: %v", err)
	}
	changed.FeishuEnabled = false
	changed.FeishuAppID = ""
	if err = reopened.SaveSettings(changed); err != nil {
		t.Fatal(err)
	}
	if reopened.GetState().Settings.FeishuSecretSet {
		t.Fatal("clearing app configuration retained the secret")
	}
}
