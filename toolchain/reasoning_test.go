package toolchain

import (
	"testing"

	"gonano-school/toolchain/types"
)

func TestParseReasoningResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     string
		wantThinking string
		wantAnswer   string
	}{
		{"think tag", "<think>\nOkay the user wants 2+2.\n</think>\n\n4", "Okay the user wants 2+2.", "4"},
		{"r1 tag", " thinking\nreason here\n<｜end▁of▁thinking｜>answer here", "reason here", "answer here"},
		{"gonano tag", "<|think_start|>why<|think_end|>because", "why", "because"},
		{"truncated", "<think>\nstill reasoning", "still reasoning", ""},
		{"plain", "just an answer", "just an answer", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			thinking, answer := ParseReasoningResponse(test.response)
			if thinking != test.wantThinking || answer != test.wantAnswer {
				t.Fatalf("got (%q, %q), want (%q, %q)", thinking, answer, test.wantThinking, test.wantAnswer)
			}
		})
	}
}

func TestExtractConversation(t *testing.T) {
	contents := []string{"solve x+1=2", "<think>\nI think x is 1.\n</think>\n\nx = 1"}
	roles := []string{"user", "assistant"}
	instruction, thinking, response := extractConversation(contents, roles)
	if instruction != "solve x+1=2" {
		t.Fatalf("instruction = %q", instruction)
	}
	if thinking != "I think x is 1." {
		t.Fatalf("thinking = %q", thinking)
	}
	if response != "x = 1" {
		t.Fatalf("response = %q", response)
	}
}

func TestReasoningItemsFromManifest(t *testing.T) {
	manifest := types.Manifest{
		"datasets/reasoning/magpie": {
			Kind:   types.KindReasoning,
			Model:  ReasoningBaseModel,
			Repo:   "example/magpie",
			Config: "default",
			Split:  "train",
		},
		"datasets/base/foo.zim": {
			Kind:  types.KindZIM,
			Model: "gonano-base",
			URL:   "https://example.test/foo.zim",
		},
	}
	items := ReasoningItems(manifest)
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].Dataset.Repo != "example/magpie" {
		t.Fatalf("repo = %q", items[0].Dataset.Repo)
	}
	if slug := SlugForReasoningKey(items[0].Key); slug != "magpie" {
		t.Fatalf("slug = %q, want magpie", slug)
	}
	zim := OnlyZIM(Select(manifest, Filter{}))
	if len(zim) != 1 || !zim[0].Dataset.IsZIM() {
		t.Fatalf("OnlyZIM = %v", zim)
	}
}
