package manifest

import (
	"os"
	"strings"
	"testing"
)

// At 500m CPU and 256Mi the services were throttled in most periods, which
// made terminals lag, and were OOM-killed, which dropped every one of them.
func TestTheServicesHaveRoomForTheirKubectls(t *testing.T) {
	raw, err := os.ReadFile("../../../k8s/nightshiftd.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if want := "limits:\n            cpu: \"2\"\n            memory: 512Mi\n"; !strings.Contains(string(raw), want) {
		t.Errorf("nightshiftd.yaml lacks the limits %q", want)
	}
}
