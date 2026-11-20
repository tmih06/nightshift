package cluster

import "testing"

const sampleOut = `12345.67 98765.43
usage_usec 2500000
user_usec 2000000
system_usec 500000
10485760
anon 4194304
file 3145728
active_file 1048576
inactive_file 2097152
`

func TestParseCgroup(t *testing.T) {
	s, err := parseCgroup(sampleOut)
	if err != nil {
		t.Fatal(err)
	}
	if s.Uptime != 12345.67 || s.CPUUsec != 2500000 || s.MemBytes != 10485760-2097152 {
		t.Fatalf("parsed %+v", s)
	}
	if s.MemMiB() != 8 {
		t.Fatalf("MemMiB = %d, want 8", s.MemMiB())
	}
	if _, err := parseCgroup("12.0 1.0\n"); err == nil {
		t.Fatal("incomplete counters should fail")
	}
}

func TestCPURate(t *testing.T) {
	a := CgroupSample{Uptime: 100, CPUUsec: 1_000_000}
	b := CgroupSample{Uptime: 102, CPUUsec: 1_500_000} // 0.5 s of CPU over 2 s
	if mc, ok := CPURate(a, b); !ok || mc != 250 {
		t.Fatalf("rate = %d %v, want 250", mc, ok)
	}
	if _, ok := CPURate(a, CgroupSample{Uptime: 100.5, CPUUsec: 1_100_000}); ok {
		t.Fatal("half a second apart is too close")
	}
	if _, ok := CPURate(b, a); ok {
		t.Fatal("a counter going backwards (restart) has no rate")
	}
}
