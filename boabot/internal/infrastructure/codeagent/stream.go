package codeagent

import "encoding/json"

// streamEvent is the minimal structure shared by all claude stream-json events.
type streamEvent struct {
	Type   string          `json:"type"`
	Delta  *deltaField     `json:"delta,omitempty"`
	Result string          `json:"result,omitempty"`
	Usage  json.RawMessage `json:"usage,omitempty"`
}

type deltaField struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ParseStreamLine parses one JSON line from the Claude Code stream-json output
// and returns any text it contains, along with a boolean indicating whether
// parsing succeeded. Malformed lines return ("", false). Known event types with
// no text return ("", true).
//
// It is exported so the mcp package can reuse it when post-processing
// run_claude_code output.
func ParseStreamLine(line string) (string, bool) {
	var ev streamEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		// Malformed lines are silently skipped.
		return "", false
	}
	switch ev.Type {
	case "content_block_delta":
		if ev.Delta != nil && ev.Delta.Type == "text_delta" {
			return ev.Delta.Text, true
		}
	case "result":
		if ev.Result != "" {
			return ev.Result, true
		}
	}
	return "", true
}
