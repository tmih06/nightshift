package cluster

import "testing"

func TestKubeContextFollowsEnvironment(t *testing.T) {
	t.Setenv(ContextEnv, "")
	if got := KubeContext(); got != DefaultKubeContext {
		t.Errorf("KubeContext() = %q, want the kind default %q", got, DefaultKubeContext)
	}
	t.Setenv(ContextEnv, " arn:aws:eks:us-east-1:1:cluster/prod ")
	if got, want := KubeContext(), "arn:aws:eks:us-east-1:1:cluster/prod"; got != want {
		t.Errorf("KubeContext() = %q, want %q", got, want)
	}
}

// In a pod there is no kubeconfig, so kubectl must be given no --context: it
// then uses the pod's service account. A context named "-" would fail every
// call.
func TestInClusterPassesNoContextToKubectl(t *testing.T) {
	t.Setenv(ContextEnv, InCluster)
	if got := ContextArgs(); len(got) != 0 {
		t.Errorf("ContextArgs() = %v, want none in-cluster", got)
	}
	if got := ContextLabel(); got != "in-cluster" {
		t.Errorf("ContextLabel() = %q", got)
	}
	cmd := command(t.Context(), "nightshift", []string{"get", "pods"})
	for _, a := range cmd.Args {
		if a == "--context" {
			t.Errorf("kubectl was given a context in-cluster: %v", cmd.Args)
		}
	}
	t.Setenv(ContextEnv, "gke_proj_zone_cluster")
	if got := ContextArgs(); len(got) != 2 || got[0] != "--context" || got[1] != "gke_proj_zone_cluster" {
		t.Errorf("ContextArgs() = %v on a named context", got)
	}
}
