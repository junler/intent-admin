package monitor

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/junler/intent-admin/internal/capability"
)

func TestJobSchemaAndPublicAnnouncementExample(t *testing.T) {
	s := &Service{}
	s.registerJobs()
	for _, action := range []string{"create", "update"} {
		entry, _ := capability.Lookup("monitor.job." + action)
		if !reflect.DeepEqual(entry.InputSchema.Properties["jobGroup"].Enum, []any{"DEFAULT", "critical", "low"}) {
			t.Fatalf("%s lacks the supported queue enum", action)
		}
	}
	entry, _ := capability.Lookup("monitor.job.create")
	var input JobInput
	if len(entry.Examples) == 0 {
		t.Fatal("missing concrete task creation example")
	}
	if err := json.Unmarshal([]byte(entry.Examples[0].Args), &input); err != nil {
		t.Fatal(err)
	}
	if input.JobGroup == nil || *input.JobGroup != "DEFAULT" || input.InvokeTarget == nil || *input.InvokeTarget != "system:createNotice" || input.Status == nil || *input.Status != "0" {
		t.Fatalf("incorrect public announcement example: %+v", input)
	}
	if input.JobParams == nil || !json.Valid([]byte(*input.JobParams)) {
		t.Fatal("example contains invalid handler parameters")
	}
}
