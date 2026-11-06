package main

import "time"

type ServerInput struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
}

type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ServerID    string `json:"serverId"`
	ProjectPath string `json:"projectPath"`
	LogPath     string `json:"logPath"`
}

type Settings struct {
	PollSeconds          int     `json:"pollSeconds"`
	Threshold            float64 `json:"threshold"`
	SpikeDelta           float64 `json:"spikeDelta"`
	SustainedMinutes     int     `json:"sustainedMinutes"`
	NotificationsVersion int     `json:"notificationsVersion"`
	FeishuEnabled        bool    `json:"feishuEnabled"`
	FeishuAppID          string  `json:"feishuAppID"`
	FeishuAppSecret      string  `json:"feishuAppSecret"`
	FeishuSecretSet      bool    `json:"feishuSecretSet"`
	FeishuTargetID       string  `json:"feishuTargetID"`
	FeishuWebhook        string  `json:"feishuWebhook"` // legacy transport, no longer used
	QQEnabled            bool    `json:"qqEnabled"`
	QQAppID              string  `json:"qqAppID"`
	QQAppSecret          string  `json:"qqAppSecret"`
	QQSecretSet          bool    `json:"qqSecretSet"`
	QQTargetType         string  `json:"qqTargetType"`
	QQTargetID           string  `json:"qqTargetID"`
	QQWebhook            string  `json:"qqWebhook"` // legacy transport, no longer used
	ModelPlatform        string  `json:"modelPlatform"`
	ModelBaseURL         string  `json:"modelBaseURL"`
	ModelEndpoint        string  `json:"modelEndpoint"` // legacy; use ModelBaseURL
	ModelName            string  `json:"modelName"`
	ModelAPIKey          string  `json:"modelAPIKey"`
	ModelKeySet          bool    `json:"modelKeySet"`
}

type DiskMetric struct {
	Mount   string  `json:"mount"`
	Percent float64 `json:"percent"`
}

type Metric struct {
	ServerID          string       `json:"serverId"`
	CPU               float64      `json:"cpu"`
	Memory            float64      `json:"memory"`
	Disks             []DiskMetric `json:"disks"`
	Timestamp         string       `json:"timestamp"`
	Error             string       `json:"error"`
	NotificationError string       `json:"notificationError"`
}

type Alert struct {
	ID       string  `json:"id"`
	ServerID string  `json:"serverId"`
	Kind     string  `json:"kind"`
	Metric   string  `json:"metric"`
	Value    float64 `json:"value"`
	Message  string  `json:"message"`
	Active   bool    `json:"active"`
	Since    string  `json:"since"`
}

type BotTarget struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type NotificationStatus struct {
	Channel string      `json:"channel"`
	Status  string      `json:"status"`
	Message string      `json:"message"`
	Targets []BotTarget `json:"targets"`
}

type AppState struct {
	Initialized   bool                 `json:"initialized"`
	Unlocked      bool                 `json:"unlocked"`
	Servers       []ServerInput        `json:"servers"`
	Projects      []Project            `json:"projects"`
	Settings      Settings             `json:"settings"`
	Metrics       []Metric             `json:"metrics"`
	Alerts        []Alert              `json:"alerts"`
	Notifications []NotificationStatus `json:"notifications"`
	Analyses      []AnalysisResult     `json:"analyses"`
}

type AnalysisTurn struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type AnalysisResult struct {
	ProjectID   string `json:"projectId"`
	Question    string `json:"question"`
	Answer      string `json:"answer"`
	GeneratedAt string `json:"generatedAt"`
	Source      string `json:"source"`
	LogExcerpt  string `json:"logExcerpt"`
}

func defaultSettings() Settings {
	return Settings{PollSeconds: 30, Threshold: 80, SpikeDelta: 20, SustainedMinutes: 5, NotificationsVersion: 2}
}

func nowISO() string { return time.Now().Format(time.RFC3339) }
