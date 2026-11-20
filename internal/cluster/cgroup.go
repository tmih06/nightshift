package cluster

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// CgroupSample is a pod's own cgroup v2 counters, read inside the pod, so
// usage is live instead of metrics-server's 15 to 30 second average.
type CgroupSample struct {
	Uptime   float64 // seconds, the pod's clock, so exec latency does not skew rates
	CPUUsec  int64   // cpu.stat usage_usec, cumulative
	MemBytes int64   // working set: memory.current less inactive_file
}

// cgroupFiles are printed in this order; memory.current is the only line
// that is a bare number after /proc/uptime.
var cgroupFiles = []string{"/proc/uptime", "/sys/fs/cgroup/cpu.stat", "/sys/fs/cgroup/memory.current", "/sys/fs/cgroup/memory.stat"}

// ReadCgroup reads pod's counters with one kubectl exec.
func ReadCgroup(pod string) (CgroupSample, error) {
	out, err := runKubectl(nil, append([]string{"exec", pod, "--", "cat"}, cgroupFiles...)...)
	if err != nil {
		return CgroupSample{}, err
	}
	return parseCgroup(string(out))
}

// ReadCgroups reads every pod in parallel, leaving out the ones that fail.
func ReadCgroups(pods []string) map[string]CgroupSample {
	out := make(map[string]CgroupSample, len(pods))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, pod := range pods {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s, err := ReadCgroup(pod); err == nil {
				mu.Lock()
				out[pod] = s
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out
}

func parseCgroup(out string) (CgroupSample, error) {
	var s CgroupSample
	var current, inactive int64 = -1, 0
	haveUptime, haveCPU := false, false
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 2 && !haveUptime:
			// /proc/uptime: "<uptime> <idle>"
			v, err := strconv.ParseFloat(f[0], 64)
			if err != nil {
				return s, fmt.Errorf("uptime %q: %w", line, err)
			}
			s.Uptime, haveUptime = v, true
		case len(f) == 2 && f[0] == "usage_usec":
			v, err := strconv.ParseInt(f[1], 10, 64)
			if err != nil {
				return s, fmt.Errorf("cpu.stat %q: %w", line, err)
			}
			s.CPUUsec, haveCPU = v, true
		case len(f) == 2 && f[0] == "inactive_file":
			inactive, _ = strconv.ParseInt(f[1], 10, 64)
		case len(f) == 1 && haveUptime && current < 0:
			v, err := strconv.ParseInt(f[0], 10, 64)
			if err != nil {
				return s, fmt.Errorf("memory.current %q: %w", line, err)
			}
			current = v
		}
	}
	if !haveUptime || !haveCPU || current < 0 {
		return s, fmt.Errorf("cgroup counters incomplete")
	}
	s.MemBytes = max(current-inactive, 0)
	return s, nil
}

// CPURate is the millicores used between two samples, false when they are
// too close together to say.
func CPURate(prev, cur CgroupSample) (int64, bool) {
	dt := cur.Uptime - prev.Uptime
	if dt < 1 || cur.CPUUsec < prev.CPUUsec {
		return 0, false
	}
	return int64(float64(cur.CPUUsec-prev.CPUUsec) / dt / 1000), true
}

// MemMiB is the sample's working set in MiB.
func (s CgroupSample) MemMiB() int64 { return s.MemBytes / (1024 * 1024) }
