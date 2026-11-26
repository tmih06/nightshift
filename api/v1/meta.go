package v1

// Info is what GET /v1 returns: who is answering and what it can do.
type Info struct {
	// APIVersion is Version.
	APIVersion string `json:"apiVersion"`
	// Version is the server's own build, for bug reports.
	Version string `json:"version"`
	// Cluster is the cluster the services act on.
	Cluster ClusterInfo `json:"cluster"`
	// Capabilities lists the optional parts this server offers.
	Capabilities []string `json:"capabilities"`
}

// ClusterInfo names the cluster behind a server.
type ClusterInfo struct {
	// Context is the kubeconfig context in use.
	Context string `json:"context"`
	// Namespace is where sandboxes live.
	Namespace string `json:"namespace"`
}

// Has reports whether the server offers the capability.
func (i Info) Has(capability string) bool {
	for _, c := range i.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}
