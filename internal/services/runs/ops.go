package runs

import (
	"time"

	"github.com/tmih06/nightshift/internal/services/cluster"
	"github.com/tmih06/nightshift/internal/services/session"
)

// Ops is what a run does to the cluster. Live is the real thing; tests swap it.
type Ops struct {
	PoolTemplate  func(pool string) (string, error)
	TemplatePod   func(template string) (pod map[string]any, image string, err error)
	CreateVolume  func(name, repo, branch string, record []byte) (created bool, err error)
	Records       func() (map[string][]byte, error)
	Save          func(name string, record []byte) error
	Clone         func(volume, image, script string, timeout time.Duration, progress func(string)) error
	CreateSandbox func(template string, pod map[string]any, volume, mount string) (string, error)
	WaitPod       func(pod string, timeout time.Duration, progress func(string)) error
	SandboxOn     func(volume string) (string, error)
	Exec          func(pod string, stdin []byte, args ...string) ([]byte, error)
	Ensure        func(pod string) error
	Send          func(pod string, keys []session.Key) error
	DeleteSandbox func(name string) error
	DeleteVolume  func(name string) error
}

// Live is Ops against the cluster kubectl points at.
func Live() Ops {
	return Ops{
		PoolTemplate:  cluster.PoolTemplate,
		TemplatePod:   cluster.TemplatePod,
		CreateVolume:  cluster.CreateRunVolume,
		Records:       cluster.RunRecords,
		Save:          cluster.SaveRunRecord,
		Clone:         cluster.CloneIntoVolume,
		CreateSandbox: cluster.CreateVolumeSandbox,
		WaitPod:       cluster.WaitForPod,
		SandboxOn:     cluster.SandboxOnVolume,
		Exec:          cluster.ExecInPod,
		Ensure:        session.Ensure,
		Send:          func(pod string, keys []session.Key) error { return session.SendBatch(pod, "", keys) },
		DeleteSandbox: cluster.DeleteSandbox,
		DeleteVolume:  cluster.DeleteVolume,
	}
}
