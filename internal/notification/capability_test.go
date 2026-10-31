package notification

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/junler/intent-admin/internal/capability"
)

func TestPersonalNotificationPreviewAndRecipient(t *testing.T) {
	New(nil).RegisterCapabilities()
	ctx := capability.WithActor(context.Background(), &capability.Actor{UserID: 7, Source: capability.SourceAI, MaxRisk: capability.RiskHigh})
	p, err := capability.RunPreview(ctx, "system.notification.send", json.RawMessage(`{"title":"统计","content":"新增用户 12 人"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) != 1 || p.Changes[0].Fields[0].After != int64(7) {
		t.Fatalf("preview did not bind principal: %+v", p)
	}
	// Preview runs without a database; attempting delivery here would panic.
	if _, err := capability.RunPreview(ctx, "system.notification.send", json.RawMessage(`{"title":"统计","content":"正文","recipientId":99}`)); err == nil {
		t.Fatal("accepted another recipient")
	}
	if _, err := capability.RunPreview(capability.WithActor(context.Background(), capability.SystemActor("unbound")), "system.notification.send", json.RawMessage(`{"title":"统计","content":"正文"}`)); err == nil {
		t.Fatal("accepted an unbound system principal")
	}
}
