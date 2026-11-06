// Package monitor schedules host sampling and persists threshold and spike alerts.
package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"servicemonitoragent/internal/sampler"
)

type Alert struct {
	ID          string    `json:"id"`
	Host        string    `json:"host"`
	Metric      string    `json:"metric"`
	Kind        string    `json:"kind"` // threshold or spike
	Value       float64   `json:"value"`
	StartedAt   time.Time `json:"started_at"`
	TriggeredAt time.Time `json:"triggered_at"`
}
type Notifier interface {
	Send(context.Context, Alert) error
}
type Config struct {
	Threshold     float64       `json:"threshold"`      // percent; zero defaults to 80
	SpikeDelta    float64       `json:"spike_delta"`    // absolute percentage points; zero defaults to 20
	SpikeDuration time.Duration `json:"spike_duration"` // zero defaults to five minutes
	Interval      time.Duration `json:"interval"`       // polling interval; zero defaults to 30 seconds
}
type Target struct {
	Host   string
	Runner sampler.Runner
}
type MetricState struct {
	LastValue          float64         `json:"last_value"`
	Seen               bool            `json:"seen"`
	ThresholdStarted   time.Time       `json:"threshold_started,omitempty"`
	ThresholdDelivered bool            `json:"threshold_delivered"`
	ThresholdChannels  map[string]bool `json:"threshold_channels,omitempty"`
	ThresholdFailures  map[string]int  `json:"threshold_failures,omitempty"`
	SpikeStarted       time.Time       `json:"spike_started,omitempty"`
	SpikeDelivered     bool            `json:"spike_delivered"`
	SpikeChannels      map[string]bool `json:"spike_channels,omitempty"`
	SpikeFailures      map[string]int  `json:"spike_failures,omitempty"`
}
type State struct {
	Version int                               `json:"version"`
	Hosts   map[string]map[string]MetricState `json:"hosts"`
}
type Scheduler struct {
	mu       sync.Mutex
	cfg      Config
	notifier Notifier
	path     string
	state    State
}

// New loads previously persisted state. A corrupt file fails closed instead of
// discarding active alerts. Empty statePath keeps state only in memory.
func New(cfg Config, notifier Notifier, statePath string) (*Scheduler, error) {
	if cfg.Threshold == 0 {
		cfg.Threshold = 80
	}
	if cfg.SpikeDelta == 0 {
		cfg.SpikeDelta = 20
	}
	if cfg.SpikeDuration == 0 {
		cfg.SpikeDuration = 5 * time.Minute
	}
	if cfg.Interval == 0 {
		cfg.Interval = 30 * time.Second
	}
	if cfg.Threshold <= 0 || cfg.Threshold > 100 || cfg.SpikeDelta <= 0 || cfg.SpikeDelta > 100 || cfg.SpikeDuration <= 0 || cfg.Interval <= 0 {
		return nil, errors.New("invalid monitoring configuration")
	}
	s := &Scheduler{cfg: cfg, notifier: notifier, path: statePath, state: State{Version: 1, Hosts: make(map[string]map[string]MetricState)}}
	if statePath != "" {
		f, err := os.Open(statePath)
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		if err != nil {
			return nil, err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, 4<<20+1))
		if err != nil {
			return nil, err
		}
		if len(b) > 4<<20 {
			return nil, errors.New("monitor state too large")
		}
		if err = json.Unmarshal(b, &s.state); err != nil {
			return nil, fmt.Errorf("decode monitor state: %w", err)
		}
		if s.state.Version != 1 || s.state.Hosts == nil {
			return nil, errors.New("unsupported monitor state")
		}
	}
	return s, nil
}

// SetNotifier updates the delivery destination. Pending alerts remain pending
// while notifier is nil and are retried on the next Evaluate after configuration.
func (s *Scheduler) SetNotifier(notifier Notifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifier = notifier
}

// Snapshot gives a deep copy suitable for JSON encoding or a user interface.
func (s *Scheduler) Snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := State{Version: 1, Hosts: make(map[string]map[string]MetricState, len(s.state.Hosts))}
	for host, metrics := range s.state.Hosts {
		out.Hosts[host] = make(map[string]MetricState, len(metrics))
		for k, v := range metrics {
			v.ThresholdChannels = cloneDeliveries(v.ThresholdChannels)
			v.ThresholdFailures = cloneFailures(v.ThresholdFailures)
			v.SpikeChannels = cloneDeliveries(v.SpikeChannels)
			v.SpikeFailures = cloneFailures(v.SpikeFailures)
			out.Hosts[host][k] = v
		}
	}
	return out
}

type pendingDelivery struct {
	alert   Alert
	channel string
}

const maxDeliveryFailures = 3

type channelNotifier interface {
	ChannelNames() []string
	SendChannel(context.Context, string, Alert) error
}

func cloneDeliveries(in map[string]bool) map[string]bool {
	if in == nil {
		return nil
	}
	out := make(map[string]bool, len(in))
	for channel, delivered := range in {
		out[channel] = delivered
	}
	return out
}

func cloneFailures(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for channel, failures := range in {
		out[channel] = failures
	}
	return out
}

func notifierChannels(n Notifier) []string {
	if n == nil {
		return nil
	}
	if channels, ok := n.(channelNotifier); ok {
		return channels.ChannelNames()
	}
	return []string{"default"}
}

func allDelivered(channels []string, deliveries map[string]bool) bool {
	if len(channels) == 0 {
		return false
	}
	for _, channel := range channels {
		if !deliveries[channel] {
			return false
		}
	}
	return true
}

// Evaluate records all three metrics and delivers each threshold crossing once.
// Threshold and spike alerts start only above Threshold. Once active, they remain
// active at Threshold and recover only below it. Each channel is retried
// independently and abandoned for that alert after three failed attempts.
func (s *Scheduler) Evaluate(ctx context.Context, host string, m sampler.Metrics, at time.Time) error {
	if host == "" || at.IsZero() {
		return errors.New("host and sample time required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Hosts[host] == nil {
		s.state.Hosts[host] = make(map[string]MetricState)
	}
	channels := notifierChannels(s.notifier)
	values := map[string]float64{"cpu": m.CPUPercent, "memory": m.MemoryPercent, "disk": m.DiskPercent}
	var pending []pendingDelivery
	queue := func(alert Alert, deliveries map[string]bool, failedAttempts map[string]int) {
		for _, channel := range channels {
			if !deliveries[channel] && failedAttempts[channel] < maxDeliveryFailures {
				pending = append(pending, pendingDelivery{alert: alert, channel: channel})
			}
		}
	}
	for name, value := range values {
		if value < 0 || value > 100 {
			return fmt.Errorf("invalid %s percent", name)
		}
		prior := s.state.Hosts[host][name]
		next := prior
		if value < s.cfg.Threshold {
			next.ThresholdStarted = time.Time{}
			next.ThresholdDelivered = false
			next.ThresholdChannels = nil
			next.ThresholdFailures = nil
			next.SpikeStarted = time.Time{}
			next.SpikeDelivered = false
			next.SpikeChannels = nil
			next.SpikeFailures = nil
		} else {
			if next.ThresholdStarted.IsZero() && value > s.cfg.Threshold {
				next.ThresholdStarted = at
				next.ThresholdDelivered = false
				next.ThresholdChannels = nil
				next.ThresholdFailures = nil
			}
			if allDelivered(channels, next.ThresholdChannels) {
				next.ThresholdDelivered = true
			}
			if !next.ThresholdStarted.IsZero() && !next.ThresholdDelivered {
				queue(Alert{ID: alertID(host, name, "threshold", next.ThresholdStarted), Host: host, Metric: name, Kind: "threshold", Value: value, StartedAt: next.ThresholdStarted, TriggeredAt: at}, next.ThresholdChannels, next.ThresholdFailures)
			}
			if value > s.cfg.Threshold && next.SpikeStarted.IsZero() && prior.Seen && value-prior.LastValue >= s.cfg.SpikeDelta {
				next.SpikeStarted = at
				next.SpikeDelivered = false
				next.SpikeChannels = nil
				next.SpikeFailures = nil
			}
			if allDelivered(channels, next.SpikeChannels) {
				next.SpikeDelivered = true
			}
			if !next.SpikeStarted.IsZero() && !next.SpikeDelivered && at.Sub(next.SpikeStarted) >= s.cfg.SpikeDuration {
				queue(Alert{ID: alertID(host, name, "spike", next.SpikeStarted), Host: host, Metric: name, Kind: "spike", Value: value, StartedAt: next.SpikeStarted, TriggeredAt: at}, next.SpikeChannels, next.SpikeFailures)
			}
		}
		next.Seen = true
		next.LastValue = value
		s.state.Hosts[host][name] = next
	}
	if err := s.persist(); err != nil {
		return err
	}
	var failures []error
	for _, delivery := range pending {
		a := delivery.alert
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		if channel, ok := s.notifier.(channelNotifier); ok {
			err = channel.SendChannel(ctx, delivery.channel, a)
		} else {
			err = s.notifier.Send(ctx, a)
		}
		st := s.state.Hosts[host][a.Metric]
		if err != nil {
			attempts := 0
			if a.Kind == "threshold" {
				if st.ThresholdFailures == nil {
					st.ThresholdFailures = make(map[string]int)
				}
				st.ThresholdFailures[delivery.channel]++
				attempts = st.ThresholdFailures[delivery.channel]
			} else {
				if st.SpikeFailures == nil {
					st.SpikeFailures = make(map[string]int)
				}
				st.SpikeFailures[delivery.channel]++
				attempts = st.SpikeFailures[delivery.channel]
			}
			s.state.Hosts[host][a.Metric] = st
			if persistErr := s.persist(); persistErr != nil {
				return persistErr
			}
			if attempts >= maxDeliveryFailures {
				failures = append(failures, fmt.Errorf("%s [%s]: 已失败 %d 次，本次告警停止向该渠道投递: %w", a.ID, delivery.channel, attempts, err))
			} else {
				failures = append(failures, fmt.Errorf("%s [%s] 第 %d 次投递失败: %w", a.ID, delivery.channel, attempts, err))
			}
			continue
		}
		if a.Kind == "threshold" {
			if st.ThresholdChannels == nil {
				st.ThresholdChannels = make(map[string]bool)
			}
			st.ThresholdChannels[delivery.channel] = true
			st.ThresholdDelivered = allDelivered(channels, st.ThresholdChannels)
		} else {
			if st.SpikeChannels == nil {
				st.SpikeChannels = make(map[string]bool)
			}
			st.SpikeChannels[delivery.channel] = true
			st.SpikeDelivered = allDelivered(channels, st.SpikeChannels)
		}
		s.state.Hosts[host][a.Metric] = st
		if err = s.persist(); err != nil {
			return err
		}
	}
	return errors.Join(failures...)
}
func alertID(host, metric, kind string, at time.Time) string {
	return fmt.Sprintf("%s/%s/%s/%d", host, metric, kind, at.UnixNano())
}
func (s *Scheduler) persist() error {
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".monitor-state-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.path)
}

// Run samples targets immediately, then every Interval, until context cancel.
// Sampling/delivery errors are sent to onError and do not stop other hosts.
func (s *Scheduler) Run(ctx context.Context, targets []Target, onError func(string, error)) error {
	if len(targets) == 0 {
		return errors.New("at least one target required")
	}
	tick := time.NewTicker(s.cfg.Interval)
	defer tick.Stop()
	collect := func() {
		for _, target := range targets {
			if err := ctx.Err(); err != nil {
				return
			}
			m, err := sampler.Collect(ctx, target.Runner)
			if err == nil {
				err = s.Evaluate(ctx, target.Host, m, m.CollectedAt)
			}
			if err != nil && onError != nil {
				onError(target.Host, err)
			}
		}
	}
	collect()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			collect()
		}
	}
}
