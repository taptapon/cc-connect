package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

func TestConfiguredModels_BoundaryConditions(t *testing.T) {
	a := &Agent{
		providers: []core.ProviderConfig{
			{Models: []core.ModelOption{{Name: "first"}}},
			{Models: []core.ModelOption{{Name: "second"}}},
		},
	}

	tests := []struct {
		name      string
		activeIdx int
		wantNil   bool
		wantName  string
	}{
		{name: "negative index", activeIdx: -1, wantNil: true},
		{name: "out of range", activeIdx: 2, wantNil: true},
		{name: "valid index", activeIdx: 1, wantName: "second"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a.activeIdx = tt.activeIdx
			got := a.configuredModels()
			if tt.wantNil {
				if got != nil {
					t.Fatalf("configuredModels() = %v, want nil", got)
				}
				return
			}
			if len(got) != 1 || got[0].Name != tt.wantName {
				t.Fatalf("configuredModels() = %v, want %q", got, tt.wantName)
			}
		})
	}
}

func TestGetModel_PrefersActiveProviderModel(t *testing.T) {
	a := &Agent{
		model: "sonnet",
		providers: []core.ProviderConfig{
			{Name: "anthropic", Model: "opus"},
		},
		activeIdx: 0,
	}

	if got := a.GetModel(); got != "opus" {
		t.Fatalf("GetModel() = %q, want opus", got)
	}
}

func TestModelPickerOptions_FromSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	settings := `{
	  "modelPicker": {
	    "options": [
	      {"model": "ccs-claude-glm[1M]", "label": "glm (zhipu)", "description": "glm-5.3 [1M]"},
	      {"model": "ccs-claude-ds", "description": "deepseek-flash (1M)"}
	    ],
	    "replaceBuiltInOptions": true
	  }
	}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &Agent{}
	opts, replace, ok := a.modelPickerOptions()
	if !ok || !replace {
		t.Fatalf("modelPickerOptions() ok=%v replace=%v, want true true", ok, replace)
	}
	if len(opts) != 2 {
		t.Fatalf("got %d options, want 2", len(opts))
	}
	if opts[0].Name != "ccs-claude-glm[1M]" || opts[0].Desc != "glm (zhipu)" {
		t.Fatalf("opts[0] = %+v", opts[0])
	}
	if opts[1].Desc != "deepseek-flash (1M)" {
		t.Fatalf("opts[1] = %+v, want label fallback to description", opts[1])
	}
}

func TestModelPickerOptions_MissingOrMalformed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)

	a := &Agent{}
	if _, _, ok := a.modelPickerOptions(); ok {
		t.Fatal("expected ok=false when settings.json is missing")
	}

	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := a.modelPickerOptions(); ok {
		t.Fatal("expected ok=false for malformed settings.json")
	}
}

func TestAvailableModels_PrefersModelPickerOverFallback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	settings := `{"modelPicker":{"options":[{"model":"ccs-x","label":"X (zhipu)"}],"replaceBuiltInOptions":true}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &Agent{}
	models := a.AvailableModels(context.Background())
	if len(models) != 1 || models[0].Name != "ccs-x" {
		t.Fatalf("AvailableModels() = %+v, want single picker option", models)
	}
}

func TestAvailableModels_FallsBackToBuiltinsWithoutPicker(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "")

	a := &Agent{}
	models := a.AvailableModels(context.Background())
	if len(models) == 0 || models[0].Name != "sonnet" {
		t.Fatalf("AvailableModels() = %+v, want builtin fallback", models)
	}
}
