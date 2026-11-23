package config

import (
	"errors"
	"fmt"
)

// ClusterRoleHub is the one role a daemon can be given: the hub holds the link every
// member dials into.
const ClusterRoleHub = "hub"

// Cluster joins daemons on different machines (docs/cluster.md). With nothing set a
// daemon is on its own, as before.
type Cluster struct {
	// Role is "hub" on the daemon members link to (it needs daemon.public, since a
	// member reaches it over HTTPS), or empty.
	Role string `yaml:"role,omitempty" json:"role,omitempty"`
	// Hub is the https address of the hub a member links to, for example
	// "https://shepherd.example.com". The credential is not here: `shepherd cluster
	// join` keeps it in the Shepherd home.
	Hub string `yaml:"hub,omitempty" json:"hub,omitempty"`
}

// IsHub says this daemon is a hub.
func (c Cluster) IsHub() bool { return c.Role == ClusterRoleHub }

func (c *Cluster) normalize() {
	if c.Hub != "" {
		if u, err := NormalizeURL(c.Hub); err == nil {
			c.Hub = u
		}
	}
}

func (c Cluster) validate(public *Public) []error {
	var errs []error
	switch c.Role {
	case "", ClusterRoleHub:
	default:
		errs = append(errs, fmt.Errorf("cluster.role: %q is not %q or empty", c.Role, ClusterRoleHub))
	}
	if c.Hub != "" {
		if _, err := NormalizeURL(c.Hub); err != nil {
			errs = append(errs, fmt.Errorf("cluster.hub: %q %v", c.Hub, err))
		}
	}
	if c.IsHub() && c.Hub != "" {
		errs = append(errs, errors.New("cluster.role: hub and cluster.hub are both set; a daemon is a hub or links to one"))
	}
	if c.IsHub() && !public.Enabled() {
		errs = append(errs, errors.New("cluster.role: hub needs daemon.public, because members reach it over https"))
	}
	return errs
}
