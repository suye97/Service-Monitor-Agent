package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct{ command string }

func (f *fakeRunner) Run(_ context.Context, command string) (string, error) {
	f.command = command
	return "2026-11-05 ERROR timeout\n", nil
}

func TestVaultLifecycleAndPersistence(t *testing.T) {
	dir := t.TempDir()
	app, err := NewApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	if app.GetState().Initialized {
		t.Fatal("new vault incorrectly initialized")
	}
	server := ServerInput{ID: "srv", Name: "测试", Host: "127.0.0.1", Port: 22, User: "operator", Password: "test-secret"}
	if err = app.InitializeVault("a-long-admin-passphrase", []ServerInput{server}); err != nil {
		t.Fatal(err)
	}
	if app.GetState().Servers[0].Password != "" {
		t.Fatal("GetState exposed SSH secret")
	}
	data, err := os.ReadFile(filepath.Join(dir, "credentials.enc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), server.Password) {
		t.Fatal("encrypted file contains SSH password")
	}
	server.Name = "更新后"
	server.Password = ""
	if err = app.UpsertServer(server, "a-long-admin-passphrase"); err != nil {
		t.Fatal(err)
	}
	app.LockVault()
	if app.GetState().Unlocked {
		t.Fatal("vault remains unlocked")
	}
	if err = app.UnlockVault("wrong-passphrase"); err == nil {
		t.Fatal("incorrect passphrase accepted")
	}
	if err = app.UnlockVault("a-long-admin-passphrase"); err != nil {
		t.Fatal(err)
	}
	if app.GetState().Servers[0].Name != "更新后" {
		t.Fatal("updated vault not persisted")
	}
	app.Close()
	app, err = NewApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if !app.GetState().Initialized || app.GetState().Unlocked {
		t.Fatal("restart should require unlock")
	}
	if err = app.UnlockVault("a-long-admin-passphrase"); err != nil {
		t.Fatal(err)
	}
}
func TestLogPathShellQuoting(t *testing.T) {
	f := new(fakeRunner)
	_, err := readLog(context.Background(), f, "/var/log/a'; touch /tmp/x; echo '.log")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(f.command, "tail -c 262144 -- '") || !strings.Contains(f.command, "'\\''") {
		t.Fatalf("path not shell quoted: %q", f.command)
	}
	if _, err = readLog(context.Background(), f, "relative/log"); err == nil {
		t.Fatal("relative path accepted")
	}
}

func TestInitializeVaultCountsUnicodeCharacters(t *testing.T) {
	app, err := NewApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if err = app.InitializeVault(strings.Repeat("密", 11), nil); err == nil {
		t.Fatal("11-character passphrase accepted")
	}
	if err = app.InitializeVault(strings.Repeat("密", 12), nil); err != nil {
		t.Fatalf("12-character Unicode passphrase rejected: %v", err)
	}
}

func TestGenerateAnalysisIncludesBoundedConversation(t *testing.T) {
	var request struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"current answer"}}]}`))
	}))
	defer server.Close()
	history := make([]AnalysisTurn, 8)
	for i := range history {
		history[i] = AnalysisTurn{Question: fmt.Sprintf("question-%d", i), Answer: fmt.Sprintf("answer-%d", i)}
	}
	answer, err := generateAnalysis(context.Background(), Settings{ModelEndpoint: server.URL, ModelName: "test-model"}, "current question", "ERROR timeout", history)
	if err != nil {
		t.Fatal(err)
	}
	if answer != "current answer" {
		t.Fatalf("unexpected answer %q", answer)
	}
	// System, one log message, six historical exchanges, and the current question.
	if len(request.Messages) != 15 {
		t.Fatalf("message count %d, want 15", len(request.Messages))
	}
	if !strings.Contains(request.Messages[1].Content, "ERROR timeout") {
		t.Fatal("log context missing")
	}
	if request.Messages[2].Content != "question-2" || request.Messages[13].Content != "answer-7" || request.Messages[14].Content != "current question" {
		t.Fatalf("conversation history was not ordered and bounded correctly: %+v", request.Messages)
	}
}
