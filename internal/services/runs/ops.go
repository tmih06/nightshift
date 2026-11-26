package runs

import (
	"github.com/tmih06/nightshift/internal/services/checkout"
	"github.com/tmih06/nightshift/internal/services/cluster"
)

// Ops is what a run does to the cluster: the checkout pipeline's steps, and
// what only a run needs. Live is the real thing; tests swap it.
type Ops struct {
	checkout.Ops
	CreateRunVolume func(name, repo, branch string, record []byte) (created bool, err error)
	Records         func() (map[string][]byte, error)
	Save            func(name string, record []byte) error
	Exec            func(pod string, stdin []byte, args ...string) ([]byte, error)
	DeleteSandbox   func(name string) error
	DeleteVolume    func(name string) error
}

// Live is Ops against the cluster kubectl points at.
func Live() Ops {
	return Ops{
		Ops:             checkout.Live(),
		CreateRunVolume: cluster.CreateRunVolume,
		Records:         cluster.RunRecords,
		Save:            cluster.SaveRunRecord,
		Exec:            cluster.ExecInPod,
		DeleteSandbox:   cluster.DeleteSandbox,
		DeleteVolume:    cluster.DeleteVolume,
	}
}
