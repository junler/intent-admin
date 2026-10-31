package agent

import (
	"encoding/json"
	"strings"

	"github.com/junler/intent-admin/internal/capability"
)

// Form credentials are delivered to the active browser once, not retained in
// the session transcript, title or model tool-call history.
func redactFormPassword(session *Session, password string) {
	session.Title = "填写表单"
	for i := range session.Messages {
		session.Messages[i].Content = strings.ReplaceAll(session.Messages[i].Content, password, capability.Redacted)
	}
	for _, message := range session.History {
		message.Content = strings.ReplaceAll(message.Content, password, capability.Redacted)
		for i := range message.ToolCalls {
			call := &message.ToolCalls[i]
			raw, _ := json.Marshal(capability.SanitizeValue(json.RawMessage(call.Function.Arguments)))
			encoded, _ := json.Marshal(password)
			marker, _ := json.Marshal(capability.Redacted)
			call.Function.Arguments = strings.ReplaceAll(string(raw), string(encoded), string(marker))
		}
	}
}
