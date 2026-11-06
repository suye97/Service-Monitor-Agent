package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

func defaultModelBaseURL(platform string) string {
	switch platform {
	case "deepseek":
		return "https://api.deepseek.com"
	case "openai":
		return "https://api.openai.com/v1"
	default:
		return ""
	}
}

func modelAPIURL(rawBaseURL, resource string) (string, error) {
	baseURL := strings.TrimSpace(rawBaseURL)
	if baseURL == "" {
		return "", errors.New("请填写模型 BaseURL")
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return "", errors.New("模型 BaseURL 无效")
	}
	scheme := strings.ToLower(u.Scheme)
	localhost := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.User != nil || u.Fragment != "" || u.RawQuery != "" || (scheme != "https" && !(scheme == "http" && localhost)) {
		return "", errors.New("模型 BaseURL 须使用 HTTPS，或本机 HTTP，且不能包含查询参数")
	}
	// Accept a legacy full chat endpoint while storing and displaying a base URL.
	u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/chat/completions")
	u.Path += "/" + strings.TrimLeft(resource, "/")
	return u.String(), nil
}

func (a *App) FetchModels(platform, baseURL, apiKey string) ([]string, error) {
	if baseURL == "" {
		baseURL = defaultModelBaseURL(platform)
	}
	a.mu.Lock()
	if apiKey == "" {
		apiKey = a.settings.ModelAPIKey
	}
	a.mu.Unlock()
	if apiKey == "" {
		return nil, errors.New("请先填写模型 API Key")
	}
	endpoint, err := modelAPIURL(baseURL, "models")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := (&http.Client{Timeout: 25 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("获取模型列表失败: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("模型列表接口返回 HTTP %d", resp.StatusCode)
	}
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("模型列表响应格式错误: %w", err)
	}
	seen := make(map[string]bool, len(result.Data))
	models := make([]string, 0, len(result.Data))
	for _, item := range result.Data {
		id := strings.TrimSpace(item.ID)
		if id != "" && !seen[id] {
			seen[id] = true
			models = append(models, id)
		}
	}
	if len(models) == 0 {
		return nil, errors.New("模型接口没有返回可用模型")
	}
	sort.Strings(models)
	return models, nil
}
