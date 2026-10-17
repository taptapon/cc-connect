package core

import (
	"strings"
	"testing"
)

// Patch 11 named the project and the message fate in the idle auto-reset
// notices. Patch 17 merged the two notices into one: the live-agent path now
// sends MsgSessionAutoResetIdleClosing (threshold + close warning + fate)
// once before the close, and MsgSessionAutoResetIdle alone covers the
// no-agent path. Guard across all languages: format verbs resolved, project
// name present, idle threshold present, fate clause present, and the closing
// variant still carries the 2-minute close cap.
func TestIdleResetNoticesCarryProjectAndFate(t *testing.T) {
	langs := []Language{LangEnglish, LangChinese, LangTraditionalChinese, LangJapanese, LangSpanish}
	fateByLang := map[Language]string{
		LangEnglish:            "handled by the new session",
		LangChinese:            "由新会话处理",
		LangTraditionalChinese: "由新會話處理",
		LangJapanese:           "新セッションが処理",
		LangSpanish:            "nueva sesión",
	}
	for _, lang := range langs {
		i := NewI18n(lang)
		reset := i.Tf(MsgSessionAutoResetIdle, "hub", 180)
		closing := i.Tf(MsgSessionAutoResetIdleClosing, "hub", 180)
		for name, s := range map[string]string{"reset": reset, "closing": closing} {
			if strings.Contains(s, "%!") || strings.Contains(s, "%s") || strings.Contains(s, "%d") {
				t.Errorf("%s (%s): format verb leaked: %q", name, lang, s)
			}
			if !strings.Contains(s, "hub") {
				t.Errorf("%s (%s): project name missing: %q", name, lang, s)
			}
			if !strings.Contains(s, "180") {
				t.Errorf("%s (%s): idle threshold missing: %q", name, lang, s)
			}
			if !strings.Contains(s, fateByLang[lang]) {
				t.Errorf("%s (%s): fate clause missing: %q", name, lang, s)
			}
		}
		if !strings.Contains(closing, "2") {
			t.Errorf("closing (%s): close-cap clause missing: %q", lang, closing)
		}
	}
}
