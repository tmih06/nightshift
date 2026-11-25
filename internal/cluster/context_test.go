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
