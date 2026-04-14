package gpt

import "testing"

func TestExtractGPTID(t *testing.T) {
	gptID, err := extractGPTID("https://chatgpt.com/gpts/editor/g-69de6a95a114819197df38ed79220511")
	if err != nil {
		t.Fatalf("extractGPTID returned error: %v", err)
	}
	if gptID != "g-69de6a95a114819197df38ed79220511" {
		t.Fatalf("unexpected gpt id %q", gptID)
	}
}

func TestLooksLikeLoginRedirect(t *testing.T) {
	if !looksLikeLoginRedirect("https://chatgpt.com/auth/login") {
		t.Fatalf("expected auth login url to be detected")
	}
	if looksLikeLoginRedirect("https://chatgpt.com/gpts/editor") {
		t.Fatalf("did not expect editor url to be treated as login redirect")
	}
}
