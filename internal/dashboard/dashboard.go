package dashboard

import (
	"fmt"

	"github.com/tmih06/nightshift/internal/cluster"
	"github.com/tmih06/nightshift/internal/session"
)

// RunOnce prints one dashboard frame without a terminal, the headless
// check of the whole pipeline against the live cluster.
func RunOnce() {
	boxes, pool, metricsOK, err := cluster.Fetch()
	pods := make([]string, len(boxes))
	for i, b := range boxes {
		pods[i] = b.Name
	}
	m := model{boxes: boxes, pool: pool, sessions: session.Existing(pods),
		metricsOK: metricsOK, err: err, loaded: true, width: 100, height: 30}
	if len(boxes) > 0 {
		m.selected = boxes[0].Name
	}
	fmt.Println(m.View())
}
