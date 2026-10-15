package claudecode

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

// Regression for the stderr noise demotion (patch 11, 2026-10-15):
//   - `[claude-code:unrecognized_model]` notices are informational (custom
//     gateway model ids) — DEBUG only, never the face of an error;
//   - an exit caused by deliberate teardown (/new /stop shutdown — the
//     killing flag) is INFO, not ERROR, and emits no error event;
//   - a real failure whose stderr carried only the notice still surfaces
//     the wait error (previously it was silent, or wore the notice as its
//     message).

const noticeLine = `[claude-code:unrecognized_model] {"model":"ccs-x--glm-5.3-flash[1m]","query_source":"sdk"}`

func TestSplitUnrecognizedModelNotices(t *testing.T) {
	cases := []struct {
		name, in, wantNotice, wantRest string
	}{
		{"notice only", noticeLine, noticeLine, ""},
		{"no notice", "boom stack", "", "boom stack"},
		{"empty", "", "", ""},
		{
			"notice then real error",
			noticeLine + "\npanic: bad",
			noticeLine,
			"panic: bad",
		},
		{
			"real error then notice",
			"first line\n" + noticeLine,
			noticeLine,
			"first line",
		},
		{
			"two notices interleaved",
			noticeLine + "\nmid\n" + noticeLine,
			noticeLine + "\n" + noticeLine,
			"mid",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotNotice, gotRest := splitUnrecognizedModelNotices(tc.in)
			if gotNotice != tc.wantNotice {
				t.Errorf("notice = %q, want %q", gotNotice, tc.wantNotice)
			}
			if gotRest != tc.wantRest {
				t.Errorf("rest = %q, want %q", gotRest, tc.wantRest)
			}
		})
	}
}

// capturedLog collects slog records so tests can assert levels/messages.
type capturedLog struct {
	mu      sync.Mutex
	records []string // "LEVEL message k=v k=v"
}

func (c *capturedLog) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var b strings.Builder
	b.WriteString(r.Level.String())
	b.WriteByte(' ')
	b.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		b.WriteByte(' ')
		b.WriteString(a.Key)
		b.WriteByte('=')
		b.WriteString(a.Value.String())
		return true
	})
	c.records = append(c.records, b.String())
	return nil
}

func (c *capturedLog) Enabled(context.Context, slog.Level) bool { return true }

func (c *capturedLog) WithAttrs([]slog.Attr) slog.Handler { return c }

func (c *capturedLog) WithGroup(name string) slog.Handler { return c }

func (c *capturedLog) has(level, substr string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.records {
		if strings.HasPrefix(r, level+" ") && strings.Contains(r, substr) {
			return true
		}
	}
	return false
}

func withCapturedLog(t *testing.T) *capturedLog {
	t.Helper()
	logs := &capturedLog{}
	old := slog.Default()
	slog.SetDefault(slog.New(logs))
	t.Cleanup(func() { slog.SetDefault(old) })
	return logs
}

func newSessionForFinishReadLoop() *claudeSession {
	ctx, cancel := context.WithCancel(context.Background())
	return &claudeSession{
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
		events: make(chan core.Event, 8),
	}
}

func runFinishReadLoop(cs *claudeSession, waitErr error, stderr string) {
	waitErrCh := make(chan error, 1)
	waitErrCh <- waitErr
	cs.finishReadLoop(waitErrCh, bytes.NewBufferString(stderr))
}

func TestFinishReadLoop_KilledExitWithNoticeDemotedToInfo(t *testing.T) {
	logs := withCapturedLog(t)
	cs := newSessionForFinishReadLoop()
	cs.killing.Store(true)

	runFinishReadLoop(cs, errors.New("exit status 1"), noticeLine)

	if evt, ok := <-cs.events; ok {
		t.Fatalf("killed session must not emit an error event, got %+v", evt)
	}
	if logs.has("ERROR", "claudeSession: process failed") {
		t.Error("killed exit must not be logged as ERROR")
	}
	if !logs.has("INFO", "claudeSession: session terminated by stop/new/shutdown") {
		t.Error("killed exit must be logged as INFO session terminated")
	}
}

func TestFinishReadLoop_NoticeOnlyCrashSurfacesWaitError(t *testing.T) {
	logs := withCapturedLog(t)
	cs := newSessionForFinishReadLoop()

	runFinishReadLoop(cs, errors.New("exit status 1"), noticeLine)

	select {
	case evt, ok := <-cs.events:
		if !ok {
			t.Fatal("events channel closed without an error event")
		}
		if evt.Error == nil || !strings.Contains(evt.Error.Error(), "exit status 1") {
			t.Fatalf("event error = %v, want the wait error", evt.Error)
		}
		if strings.Contains(evt.Error.Error(), "unrecognized_model") {
			t.Fatalf("event error must not wear the model notice: %v", evt.Error)
		}
	default:
		t.Fatal("expected an error event")
	}
	if !logs.has("ERROR", "claudeSession: process failed") {
		t.Error("real crash must stay ERROR")
	}
	if logs.has("ERROR", "claudeSession: process failed") && noticeLoggedAsError(logs) {
		t.Error("model notice must not appear in ERROR logs")
	}
}

func noticeLoggedAsError(logs *capturedLog) bool {
	return logs.has("ERROR", "unrecognized_model")
}

func TestFinishReadLoop_MixedStderrCrashKeepsRealError(t *testing.T) {
	logs := withCapturedLog(t)
	cs := newSessionForFinishReadLoop()

	runFinishReadLoop(cs, errors.New("exit status 1"), noticeLine+"\npanic: boom")

	if evt, ok := <-cs.events; !ok {
		t.Fatal("expected an error event")
	} else if !strings.Contains(evt.Error.Error(), "panic: boom") {
		t.Fatalf("event error = %v, want the real stderr", evt.Error)
	}
	if !logs.has("ERROR", "panic: boom") {
		t.Error("ERROR log should carry the real stderr")
	}
	if noticeLoggedAsError(logs) {
		t.Error("model notice must not appear in ERROR logs")
	}
}

func TestFinishReadLoop_BareCrashNoStderrSurfacesError(t *testing.T) {
	// Previously err != nil with empty stderr was silently dropped — the
	// process disappeared with no log line and no event.
	logs := withCapturedLog(t)
	cs := newSessionForFinishReadLoop()

	runFinishReadLoop(cs, errors.New("signal: killed"), "")

	select {
	case evt, ok := <-cs.events:
		if !ok {
			t.Fatal("events channel closed without an error event")
		}
		if evt.Error == nil || !strings.Contains(evt.Error.Error(), "signal: killed") {
			t.Fatalf("event error = %v, want the wait error", evt.Error)
		}
	default:
		t.Fatal("expected an error event for a bare crash")
	}
	if !logs.has("ERROR", "claudeSession: process failed") {
		t.Error("bare crash must be logged as ERROR")
	}
}
