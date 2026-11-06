package monitor

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"servicemonitoragent/internal/sampler"
)

type recorder struct {
	alerts []Alert
	fail   bool
}

func (r *recorder) Send(_ context.Context, a Alert) error {
	if r.fail {
		return errors.New("delivery failed")
	}
	r.alerts = append(r.alerts, a)
	return nil
}
func TestPersistedThresholdAndSustainedSpike(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "state.json")
	r := &recorder{}
	cfg := Config{Threshold: 80, SpikeDelta: 20, SpikeDuration: 5 * time.Minute}
	s, err := New(cfg, nil, path)
	if err != nil {
		t.Fatal(err)
	}
	eval := func(s *Scheduler, cpu float64, offset time.Duration) error {
		return s.Evaluate(context.Background(), "host", sampler.Metrics{CPUPercent: cpu, MemoryPercent: 10, DiskPercent: 10}, at.Add(offset))
	}
	if err := eval(s, 40, 0); err != nil {
		t.Fatal(err)
	}
	if err := eval(s, 85, time.Minute); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().Hosts["host"]["cpu"].ThresholdDelivered {
		t.Fatal("nil notifier marked delivered")
	}
	s, err = New(cfg, r, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := eval(s, 85, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	if len(r.alerts) != 1 || r.alerts[0].Kind != "threshold" {
		t.Fatalf("threshold alerts: %+v", r.alerts)
	}
	if err := eval(s, 85, 3*time.Minute); err != nil {
		t.Fatal(err)
	}
	if len(r.alerts) != 1 {
		t.Fatal("duplicate threshold")
	}
	// Recover and jump from 60 to 90; spike alert waits five minutes.
	if err := eval(s, 60, 4*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := eval(s, 90, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := eval(s, 90, 9*time.Minute); err != nil {
		t.Fatal(err)
	}
	if len(r.alerts) != 2 {
		t.Fatalf("premature spike: %+v", r.alerts)
	}
	if err := eval(s, 90, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if len(r.alerts) != 3 || r.alerts[2].Kind != "spike" {
		t.Fatalf("sustained spike: %+v", r.alerts)
	}
	s2, err := New(cfg, r, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := eval(s2, 90, 11*time.Minute); err != nil {
		t.Fatal(err)
	}
	if len(r.alerts) != 3 {
		t.Fatal("duplicate after restart")
	}
}
func TestRetryFailedDelivery(t *testing.T) {
	r := &recorder{fail: true}
	s, err := New(Config{}, r, "")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	m := sampler.Metrics{CPUPercent: 82}
	if err := s.Evaluate(context.Background(), "host", m, at); err == nil {
		t.Fatal("failed notification ignored")
	}
	r.fail = false
	if err := s.Evaluate(context.Background(), "host", m, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(r.alerts) != 1 {
		t.Fatalf("retry count %d", len(r.alerts))
	}
}

func TestThresholdRequiresValueAboveLimit(t *testing.T) {
	r := new(recorder)
	s, err := New(Config{Threshold: 80}, r, "")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	eval := func(cpu float64, offset time.Duration) {
		t.Helper()
		if err := s.Evaluate(context.Background(), "host", sampler.Metrics{CPUPercent: cpu}, at.Add(offset)); err != nil {
			t.Fatal(err)
		}
	}
	eval(80, 0)
	if len(r.alerts) != 0 || !s.Snapshot().Hosts["host"]["cpu"].ThresholdStarted.IsZero() {
		t.Fatal("value equal to threshold triggered an alert")
	}
	eval(81, time.Minute)
	eval(80, 2*time.Minute)
	eval(81, 3*time.Minute)
	if len(r.alerts) != 1 {
		t.Fatalf("active threshold alert repeated: %+v", r.alerts)
	}
	eval(79, 4*time.Minute)
	eval(80, 5*time.Minute)
	if len(r.alerts) != 1 {
		t.Fatal("equal value after recovery triggered an alert")
	}
}

func TestSustainedSpikeAllowsValueEqualToThreshold(t *testing.T) {
	r := new(recorder)
	s, err := New(Config{Threshold: 80, SpikeDelta: 20, SpikeDuration: 5 * time.Minute}, r, "")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, sample := range []struct {
		cpu    float64
		offset time.Duration
	}{{60, 0}, {90, time.Minute}, {80, 6 * time.Minute}} {
		if err := s.Evaluate(context.Background(), "host", sampler.Metrics{CPUPercent: sample.cpu}, at.Add(sample.offset)); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.alerts) != 2 || r.alerts[1].Kind != "spike" {
		t.Fatalf("sustained spike at threshold was not delivered: %+v", r.alerts)
	}
}

type channelRecorder struct {
	calls map[string]int
	fail  map[string]bool
}

func (r *channelRecorder) ChannelNames() []string { return []string{"feishu", "qq"} }
func (r *channelRecorder) SendChannel(_ context.Context, channel string, _ Alert) error {
	r.calls[channel]++
	if r.fail[channel] {
		return errors.New("delivery failed")
	}
	return nil
}
func (r *channelRecorder) Send(ctx context.Context, alert Alert) error {
	var failures []error
	for _, channel := range r.ChannelNames() {
		if err := r.SendChannel(ctx, channel, alert); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func TestRetryOnlyFailedChannelAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	r := &channelRecorder{calls: map[string]int{}, fail: map[string]bool{"qq": true}}
	s, err := New(Config{Threshold: 80}, r, path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	m := sampler.Metrics{CPUPercent: 90}
	if err := s.Evaluate(context.Background(), "host", m, at); err == nil {
		t.Fatal("partial channel failure was ignored")
	}
	state := s.Snapshot().Hosts["host"]["cpu"]
	if state.ThresholdDelivered || !state.ThresholdChannels["feishu"] || state.ThresholdChannels["qq"] {
		t.Fatalf("incorrect per-channel delivery state: %+v", state)
	}
	r.fail["qq"] = false
	s, err = New(Config{Threshold: 80}, r, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Evaluate(context.Background(), "host", m, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if r.calls["feishu"] != 1 || r.calls["qq"] != 2 {
		t.Fatalf("successful channel was repeated or failed channel not retried: %+v", r.calls)
	}
	if !s.Snapshot().Hosts["host"]["cpu"].ThresholdDelivered {
		t.Fatal("alert not marked delivered after all channels succeeded")
	}
}

func TestChannelStopsAfterThreeFailuresAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	r := &channelRecorder{calls: map[string]int{}, fail: map[string]bool{"qq": true}}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	m := sampler.Metrics{CPUPercent: 90}
	var s *Scheduler
	for attempt := 1; attempt <= maxDeliveryFailures; attempt++ {
		var err error
		s, err = New(Config{Threshold: 80}, r, path)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Evaluate(context.Background(), "host", m, at.Add(time.Duration(attempt-1)*time.Minute)); err == nil {
			t.Fatalf("attempt %d failure was ignored", attempt)
		}
	}
	state := s.Snapshot().Hosts["host"]["cpu"]
	if state.ThresholdFailures["qq"] != 3 || state.ThresholdFailures["feishu"] != 0 {
		t.Fatalf("incorrect failure counts: %+v", state.ThresholdFailures)
	}
	s, err := New(Config{Threshold: 80}, r, path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Evaluate(context.Background(), "host", m, at.Add(3*time.Minute)); err != nil {
		t.Fatalf("abandoned channel produced another error: %v", err)
	}
	if r.calls["feishu"] != 1 || r.calls["qq"] != 3 {
		t.Fatalf("channel exceeded retry limit: %+v", r.calls)
	}
	// A recovered metric creates a new alert with a fresh delivery budget.
	if err = s.Evaluate(context.Background(), "host", sampler.Metrics{CPUPercent: 50}, at.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err = s.Evaluate(context.Background(), "host", m, at.Add(5*time.Minute)); err == nil {
		t.Fatal("new alert did not retry failed channel")
	}
	if r.calls["feishu"] != 2 || r.calls["qq"] != 4 {
		t.Fatalf("new alert did not reset delivery attempts: %+v", r.calls)
	}
}
