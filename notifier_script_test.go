package cameracoordinator

import (
	"bytes"
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// call records a single invocation of exec.CommandContext.
type call struct {
	script string
	args   []string
}

// fakeCommand returns a fake commandContext implementation along with a
// channel that receives every invocation. The returned function can be
// assigned directly to a ScriptNotifier's commandContext field
func fakeCommand(fail bool) (func(ctx context.Context, name string, args ...string) *exec.Cmd, <-chan call) {
	ch := make(chan call, 100)
	fn := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		ch <- call{name, args}
		if fail {
			return exec.CommandContext(ctx, "false")
		}
		return exec.CommandContext(ctx, "true")
	}
	return fn, ch
}

// sendEvent submits an event to the channel with a timeout to avoid hangs in
// case of a bug in the notifier.
func sendEvent(t *testing.T, events chan<- CameraEvent, ev CameraEvent) {
	t.Helper()
	select {
	case events <- ev:
	case <-time.After(defaultTimeout):
		t.Fatal("timed out sending event")
	}
}

// recvCall reads a single call from the channel. If no call
// arrives before defaultTimeout the function returns nil instead of
// failing; callers can decide whether the absence of a call is expected.
func recvCall(t *testing.T, ch <-chan call) *call {
	t.Helper()
	select {
	case c := <-ch:
		return &c
	case <-time.After(defaultTimeout):
		return nil
	}
}

func TestScriptNotifier_OnEvent(t *testing.T) {
	fakeCommandContext, callsCh := fakeCommand(false)

	events := make(chan CameraEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	a := NewScriptNotifier(ScriptNotifierConfig{OnScript: "on"})
	a.commandContext = fakeCommandContext
	wg.Go(func() {
		_ = a.Run(ctx, events)
	})
	defer wg.Wait()
	defer cancel()

	sendEvent(t, events, CameraEvent{Type: CameraEventRecordingOn, VideoDevice: "video0"})
	c := recvCall(t, callsCh)
	if c == nil {
		t.Fatal("expected a call but got none")
	}
	if c.script != "on" || len(c.args) != 2 || c.args[0] != "recording_on" || c.args[1] != "video0" {
		t.Errorf("unexpected call: %+v", c)
	}
}

func TestScriptNotifier_OffEvent(t *testing.T) {
	fakeCommandContext, callsCh := fakeCommand(false)

	events := make(chan CameraEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	a := NewScriptNotifier(ScriptNotifierConfig{OffScript: "off"})
	a.commandContext = fakeCommandContext
	wg.Go(func() {
		_ = a.Run(ctx, events)
	})
	defer wg.Wait()
	defer cancel()

	sendEvent(t, events, CameraEvent{Type: CameraEventRecordingOff, VideoDevice: "video1"})
	c := recvCall(t, callsCh)
	if c == nil {
		t.Fatal("expected a call but got none")
	}
	if c.script != "off" || len(c.args) != 2 || c.args[0] != "recording_off" || c.args[1] != "video1" {
		t.Errorf("unexpected call: %+v", c)
	}
}

func TestScriptNotifier_NoScriptConfigured(t *testing.T) {
	fakeCommandContext, callsCh := fakeCommand(false)

	events := make(chan CameraEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	a := NewScriptNotifier(ScriptNotifierConfig{})
	a.commandContext = fakeCommandContext
	wg.Go(func() {
		_ = a.Run(ctx, events)
	})
	defer wg.Wait()
	defer cancel()

	sendEvent(t, events, CameraEvent{Type: CameraEventRecordingOn, VideoDevice: "video0"})

	if c := recvCall(t, callsCh); c != nil {
		t.Errorf("unexpected call %v", c)
	}
}

func TestScriptNotifier_ClosedChannel(t *testing.T) {
	fakeCommandContext, callsCh := fakeCommand(false)

	events := make(chan CameraEvent)
	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	a := NewScriptNotifier(ScriptNotifierConfig{})
	a.commandContext = fakeCommandContext
	wg.Go(func() {
		_ = a.Run(ctx, events)
	})
	defer wg.Wait()
	defer cancel()

	close(events)

	wg.Wait()
	if c := recvCall(t, callsCh); c != nil {
		t.Errorf("unexpected call %v", c)
	}
}

func TestScriptNotifier_ContextCancellation(t *testing.T) {
	fakeCommandContext, callsCh := fakeCommand(false)

	events := make(chan CameraEvent)
	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	a := NewScriptNotifier(ScriptNotifierConfig{})
	a.commandContext = fakeCommandContext
	wg.Go(func() {
		_ = a.Run(ctx, events)
	})
	cancel()
	wg.Wait()

	if c := recvCall(t, callsCh); c != nil {
		t.Errorf("unexpected call %v", c)
	}
}

func TestScriptNotifier_FailingScript(t *testing.T) {
	fakeCommandContext, callsCh := fakeCommand(true)

	// capture logs to ensure error format
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	old := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(old)

	events := make(chan CameraEvent, 2)
	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	a := NewScriptNotifier(ScriptNotifierConfig{OnScript: "on"})
	a.commandContext = fakeCommandContext
	wg.Go(func() {
		_ = a.Run(ctx, events)
	})
	defer wg.Wait()
	defer cancel()

	sendEvent(t, events, CameraEvent{Type: CameraEventRecordingOn, VideoDevice: "video0"})
	sendEvent(t, events, CameraEvent{Type: CameraEventRecordingOn, VideoDevice: "video0"})
	var calls []call
	for i := range 2 {
		c := recvCall(t, callsCh)
		if c == nil {
			t.Fatalf("did not receive call #%d", i+1)
		}
		calls = append(calls, *c)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}

	logs := buf.String()
	if strings.Contains(logs, "output=") {
		t.Errorf("log unexpectedly contains output field: %q", logs)
	}
}

func TestScriptNotifier_BothScripts(t *testing.T) {
	fakeCommandContext, callsCh := fakeCommand(false)

	events := make(chan CameraEvent, 2)
	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	a := NewScriptNotifier(ScriptNotifierConfig{OnScript: "on", OffScript: "off"})
	a.commandContext = fakeCommandContext
	wg.Go(func() {
		_ = a.Run(ctx, events)
	})
	defer wg.Wait()
	defer cancel()

	sendEvent(t, events, CameraEvent{Type: CameraEventRecordingOn, VideoDevice: "video2"})
	sendEvent(t, events, CameraEvent{Type: CameraEventRecordingOff, VideoDevice: "video2"})
	var calls []call
	for i := range 2 {
		c := recvCall(t, callsCh)
		if c == nil {
			t.Fatalf("did not receive call #%d", i+1)
		}
		calls = append(calls, *c)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	// order is not guaranteed, ensure both on+off were invoked
	seen := map[string]bool{}
	for _, c := range calls {
		seen[c.script] = true
		switch c.script {
		case "on":
			if len(c.args) != 2 || c.args[0] != "recording_on" || c.args[1] != "video2" {
				t.Errorf("unexpected on call args: %+v", c.args)
			}
		case "off":
			if len(c.args) != 2 || c.args[0] != "recording_off" || c.args[1] != "video2" {
				t.Errorf("unexpected off call args: %+v", c.args)
			}
		default:
			t.Errorf("unexpected script %q", c.script)
		}
	}
	if !seen["on"] || !seen["off"] {
		t.Errorf("missing expected scripts, got %+v", calls)
	}
}
