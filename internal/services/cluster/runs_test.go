package cluster

import "testing"

func TestRunRecordsSkipVolumesBeingDeleted(t *testing.T) {
	got, err := parseRunRecords([]byte(`{"items":[
		{"metadata":{"name":"run-a","annotations":{"nightshift.dev/run-record":"{}"}}},
		{"metadata":{"name":"run-b","deletionTimestamp":"2026-01-01T00:00:00Z","annotations":{"nightshift.dev/run-record":"{}"}}},
		{"metadata":{"name":"run-c"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || string(got["run-a"]) != "{}" {
		t.Errorf("records = %q, want only run-a", got)
	}
}
