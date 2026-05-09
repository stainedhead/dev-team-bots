package codeagent

import "testing"

func TestParseStreamLine_ContentBlockDelta(t *testing.T) {
	t.Parallel()
	line := `{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello world"}}`
	text, ok := ParseStreamLine(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if text != "hello world" {
		t.Errorf("expected 'hello world', got %q", text)
	}
}

func TestParseStreamLine_ResultEvent(t *testing.T) {
	t.Parallel()
	line := `{"type":"result","result":"task completed"}`
	text, ok := ParseStreamLine(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if text != "task completed" {
		t.Errorf("expected 'task completed', got %q", text)
	}
}

func TestParseStreamLine_UnknownEventType(t *testing.T) {
	t.Parallel()
	line := `{"type":"message_start","message":{}}`
	text, ok := ParseStreamLine(line)
	if !ok {
		t.Fatal("expected ok=true for unknown event type")
	}
	if text != "" {
		t.Errorf("expected empty text for unknown event, got %q", text)
	}
}

func TestParseStreamLine_MalformedJSON(t *testing.T) {
	t.Parallel()
	text, ok := ParseStreamLine("{not valid json}")
	if ok {
		t.Fatal("expected ok=false for malformed JSON")
	}
	if text != "" {
		t.Errorf("expected empty text for malformed JSON, got %q", text)
	}
}

func TestParseStreamLine_EmptyDelta(t *testing.T) {
	t.Parallel()
	// Delta with wrong type — should return empty text but ok=true.
	line := `{"type":"content_block_delta","delta":{"type":"other_delta","text":"x"}}`
	text, ok := ParseStreamLine(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if text != "" {
		t.Errorf("expected empty text for non-text_delta, got %q", text)
	}
}
