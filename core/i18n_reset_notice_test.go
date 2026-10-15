package core

import (
	"strings"
	"testing"
)

// Patch 10: the idle auto-reset notices must name the project and tell the
// user what happens to the message that triggered the reset (2026-10-15:
// "重置的谁和谁的对话，看不出来" — both notices were anonymous and said
// nothing about the just-arrived message). MsgSessionAutoResetIdle gained a
// leading %s (project name) and MsgSessionClosingGraceful gained both a %s
// and a fate clause; guard the argument order and the fate sentence across
// all languages so a future translation edit cannot silently drop them.
func TestIdleResetNoticesCarryProjectAndFate(t *testing.T) {
	langs := []Language{LangEnglish, LangChinese, LangTraditionalChinese, LangJapanese, LangSpanish}
	for _, lang := range langs {
		i := NewI18n(lang)
		closing := i.Tf(MsgSessionClosingGraceful, "hub")
		reset := i.Tf(MsgSessionAutoResetIdle, "hub", 180)
		for name, s := range map[string]string{"closing": closing, "reset": reset} {
			if strings.Contains(s, "%!") || strings.Contains(s, "%s") || strings.Contains(s, "%d") {
				t.Errorf("%s (%s): format verb leaked: %q", name, lang, s)
			}
			if !strings.Contains(s, "hub") {
				t.Errorf("%s (%s): project name missing: %q", name, lang, s)
			}
		}
		if !strings.Contains(reset, "180") {
			t.Errorf("reset (%s): idle threshold missing: %q", lang, reset)
		}
	}

	// The reset notice must state the fate of the just-arrived message in
	// every language.
	fateByLang := map[Language]string{
		LangEnglish:            "handled by the new session",
		LangChinese:            "新会话继续处理",
		LangTraditionalChinese: "新會話繼續處理",
		LangJapanese:           "新しいセッションが処理",
		LangSpanish:            "nueva sesión",
	}
	for lang, fate := range fateByLang {
		i := NewI18n(lang)
		if s := i.Tf(MsgSessionAutoResetIdle, "hub", 180); !strings.Contains(s, fate) {
			t.Errorf("reset (%s): fate clause missing: %q", lang, s)
		}
	}
}
