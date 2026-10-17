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

func TestHandleMessage_EmptyMentionTurnSynthesizesPrompt(t *testing.T) {
	// Patch 18: a platform-admitted empty mention turn is an ADDRESSED
	// signal — the guard synthesizes the localized intent prompt and the
	// agent receives it as the turn prompt, instead of the fixed hint or
	// silence. Reading sentPrompts after waitSent is race-free: the reply
	// path takes p.mu, giving a happens-before edge with Send's write.
	p := &stubPlatformEngine{n: "test"}
	session := newResultAgentSession("ok")
	e := NewEngine("proj", &resultAgent{session: session}, []Platform{p}, "", LangEnglish)

	e.handleMessage(p, &Message{
		SessionKey: "test:u1", Platform: "test", UserID: "u1", UserName: "user",
		Content:          "",
		EmptyMentionTurn: true,
		ReplyCtx:         "ctx",
	})

	sent := waitSent(t, p)
	if len(sent) == 0 {
		t.Fatal("empty mention turn produced no agent reply — guard dropped it")
	}
	if strings.Contains(sent[0], "empty message") || strings.Contains(sent[0], "ℹ️") {
		t.Errorf("reply looks like the fixed hint, want an agent reply to the synthesized prompt: %q", sent[0])
	}
	if len(session.sentPrompts) != 1 {
		t.Fatalf("agent prompts = %v, want exactly the synthesized intent prompt", session.sentPrompts)
	}
	if !strings.Contains(session.sentPrompts[0], "mention") {
		t.Errorf("synthesized prompt should mention the empty mention, got %q", session.sentPrompts[0])
	}
}

func TestHandleMessage_EmptyMentionTurnFromBotStaysSilent(t *testing.T) {
	// Defensive: platforms never set EmptyMentionTurn for bot senders, but
	// the guard must not let a (FromBot, EmptyMentionTurn) combination
	// through — two bots trading empties would ping-pong full turns.
	p := &stubPlatformEngine{n: "test"}
	e := NewEngine("proj", &stubAgent{}, []Platform{p}, "", LangEnglish)

	e.handleMessage(p, &Message{
		SessionKey: "test:u1", Platform: "test", UserID: "ou_bot", UserName: "peer-bot",
		Content:          "",
		FromBot:          true,
		EmptyMentionTurn: true,
		ReplyCtx:         "ctx",
	})

	time.Sleep(200 * time.Millisecond)
	if sent := p.getSent(); len(sent) != 0 {
		t.Fatalf("sent = %v, want silence for bot-sent empty mention turn", sent)
	}
}
