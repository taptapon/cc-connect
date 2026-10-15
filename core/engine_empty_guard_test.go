package core

import (
	"strings"
	"testing"
	"time"
)

// Regression for the empty-message guard root-cure (2026-10-15, patch 11):
// an all-empty message must never spawn an agent turn. Humans get a fixed
// hint reply naming the project; bot senders stay SILENT — two bots in a
// shared group would otherwise answer each other's empties and ping-pong
// hint replies forever.

func waitSent(t *testing.T, p *stubPlatformEngine) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sent := p.getSent(); len(sent) > 0 {
			return sent
		}
		time.Sleep(10 * time.Millisecond)
	}
	return p.getSent()
}

func TestHandleMessage_EmptyTextRepliesHintToHuman(t *testing.T) {
	p := &stubPlatformEngine{n: "test"}
	e := NewEngine("proj", &stubAgent{}, []Platform{p}, "", LangEnglish)

	e.handleMessage(p, &Message{
		SessionKey: "test:u1", Platform: "test", UserID: "u1", UserName: "user",
		Content:  "   ", // whitespace-only text, no media, no quote
		ReplyCtx: "ctx",
	})

	sent := waitSent(t, p)
	if len(sent) != 1 {
		t.Fatalf("sent = %v, want exactly the fixed hint reply", sent)
	}
	if !strings.Contains(sent[0], "proj") {
		t.Errorf("hint reply should name the project, got %q", sent[0])
	}
}

func TestHandleMessage_WhitespaceQuoteCountsAsEmpty(t *testing.T) {
	p := &stubPlatformEngine{n: "test"}
	e := NewEngine("proj", &stubAgent{}, []Platform{p}, "", LangEnglish)

	e.handleMessage(p, &Message{
		SessionKey: "test:u1", Platform: "test", UserID: "u1", UserName: "user",
		Content:      "",
		ExtraContent: "   ", // whitespace-only reply quote must not smuggle through
		ReplyCtx:     "ctx",
	})

	sent := waitSent(t, p)
	if len(sent) != 1 {
		t.Fatalf("sent = %v, want exactly the fixed hint reply", sent)
	}
	if !strings.Contains(sent[0], "proj") {
		t.Errorf("hint reply should name the project, got %q", sent[0])
	}
}

func TestHandleMessage_EmptyMessageFromBotStaysSilent(t *testing.T) {
	p := &stubPlatformEngine{n: "test"}
	e := NewEngine("proj", &stubAgent{}, []Platform{p}, "", LangEnglish)

	e.handleMessage(p, &Message{
		SessionKey: "test:u1", Platform: "test", UserID: "ou_bot", UserName: "peer-bot",
		Content:  "",
		FromBot:  true,
		ReplyCtx: "ctx",
	})

	time.Sleep(200 * time.Millisecond)
	if sent := p.getSent(); len(sent) != 0 {
		t.Fatalf("sent = %v, want silence for bot-sent empty message (ping-pong loop guard)", sent)
	}
}

func TestHandleMessage_NonEmptyMessageStillProcessed(t *testing.T) {
	// Control: real text must NOT trip the guard — the agent session must
	// receive the prompt (stubAgentSession.Send is invoked via StartSession).
	p := &stubPlatformEngine{n: "test"}
	agent := &resultAgent{session: newResultAgentSession("ok")}
	e := NewEngine("proj", agent, []Platform{p}, "", LangEnglish)

	e.handleMessage(p, &Message{
		SessionKey: "test:u1", Platform: "test", UserID: "u1", UserName: "user",
		Content:  "hello",
		ReplyCtx: "ctx",
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		got := len(p.sent)
		p.mu.Unlock()
		if got > 0 {
			return // agent turn produced its reply — guard did not misfire
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("non-empty message never produced an agent reply — guard is misfiring")
}
