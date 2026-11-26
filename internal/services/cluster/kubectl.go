package cluster

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// command builds a kubectl run against Nightshift's cluster in namespace ns.
// The arguments go straight to kubectl's argv, never through a shell.
func command(ctx context.Context, ns string, args []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "kubectl")
	cmd.Args = append(cmd.Args, ContextArgs()...)
	cmd.Args = append(cmd.Args, "-n", ns)
	cmd.Args = append(cmd.Args, args...)
	return cmd
}

// Kubectl runs kubectl in namespace ns and returns what it printed. The
// error carries kubectl's own message.
func Kubectl(ctx context.Context, ns string, args ...string) ([]byte, error) {
	cmd := command(ctx, ns, args)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("kubectl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errOut.String()))
	}
	return out.Bytes(), nil
}

// PortForward returns an unstarted kubectl port-forward of target's port to a
// free local port; kubectl prints the port it chose on its standard output.
func PortForward(ctx context.Context, ns, target string, port int) *exec.Cmd {
	return command(ctx, ns, []string{"port-forward", target, ":" + strconv.Itoa(port)})
}
