package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"servicemonitoragent/internal/credentials"
)

var errPrivateTooLarge = errors.New("加密记录超过 8 MiB 容量限制")

type privateData struct {
	FeishuAppSecret string           `json:"feishuAppSecret"`
	QQAppSecret     string           `json:"qqAppSecret"`
	ModelAPIKey     string           `json:"modelAPIKey"`
	Analyses        []AnalysisResult `json:"analyses"`
}

func (a *App) privateKeyPath() string  { return filepath.Join(a.dir, "private-key.enc") }
func (a *App) privateDataPath() string { return filepath.Join(a.dir, "private-data.enc") }

// The random data key is wrapped with the administrator passphrase. Retaining
// only the data key while unlocked lets settings and analyses be saved without
// keeping the administrator passphrase in memory.
func (a *App) openPrivate(passphrase string) (string, privateData, error) {
	var data privateData
	blob, err := credentials.Load(a.privateKeyPath(), passphrase)
	if errors.Is(err, os.ErrNotExist) {
		if _, statErr := os.Stat(a.privateDataPath()); statErr == nil {
			return "", data, errors.New("加密数据的密钥文件丢失")
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", data, statErr
		}
		key := make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return "", data, err
		}
		encoded := base64.StdEncoding.EncodeToString(key)
		wrapped, _ := json.Marshal(encoded)
		if err = credentials.Save(a.privateKeyPath(), wrapped, passphrase); err != nil {
			return "", data, err
		}
		return encoded, data, nil
	}
	if err != nil {
		return "", data, err
	}
	var key string
	if err = json.Unmarshal(blob, &key); err != nil {
		return "", data, errors.New("加密数据密钥格式错误")
	}
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(decoded) != 32 {
		return "", data, errors.New("加密数据密钥格式错误")
	}
	blob, err = credentials.Load(a.privateDataPath(), key)
	if errors.Is(err, os.ErrNotExist) {
		return key, data, nil
	}
	if err != nil {
		return "", data, err
	}
	if err = json.Unmarshal(blob, &data); err != nil {
		return "", privateData{}, errors.New("加密数据内容错误")
	}
	return key, data, nil
}

func (a *App) savePrivate(key string, settings Settings, analyses []AnalysisResult) error {
	if key == "" {
		return errors.New("请先解锁保险库以保存密钥或分析记录")
	}
	blob, err := json.Marshal(privateData{
		FeishuAppSecret: settings.FeishuAppSecret,
		QQAppSecret:     settings.QQAppSecret,
		ModelAPIKey:     settings.ModelAPIKey,
		Analyses:        analyses,
	})
	if err != nil {
		return err
	}
	if len(blob) > 8<<20 {
		return errPrivateTooLarge
	}
	return credentials.Save(a.privateDataPath(), blob, key)
}

func (a *App) recordAnalysis(session context.Context, result AnalysisResult) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.unlocked || session == nil || a.sessionCtx != session {
		return errors.New("分析期间保险库已锁定，请重新解锁")
	}
	history := append(append([]AnalysisResult{}, a.analyses...), result)
	if len(history) > 100 {
		history = history[len(history)-100:]
	}
	for {
		err := a.savePrivate(a.privateKey, a.settings, history)
		if err == nil {
			a.analyses = history
			return nil
		}
		if !errors.Is(err, errPrivateTooLarge) || len(history) == 1 {
			return fmt.Errorf("保存分析记录失败: %w", err)
		}
		history = history[1:]
	}
}
