package config

import (
	"errors"
	"fmt"
)

// ClusterRoleController is the one role a daemon can be given: the controller holds the
// link every worker dials into.
const ClusterRoleController = "controller"

// clusterRoleHub is the old name of ClusterRoleController, still accepted with a warning.
const clusterRoleHub = "hub"

// Cluster joins daemons on different machines (docs/cluster.md). With nothing set a
// daemon is on its own, as before, and never dials anywhere.
type Cluster struct {
	// Role is "controller" on the daemon workers link to (it needs daemon.public, since a
	// worker reaches it over HTTPS), or empty. The old value "hub" is read as
	// "controller", with a warning.
	Role string `yaml:"role,omitempty" json:"role,omitempty"`
	// Controller is the https address of the controller a worker links to, for example
	// "https://shepherd.example.com". The credential is not here: `shepherd cluster
	// join` keeps it in the Shepherd home.
	Controller string `yaml:"controller,omitempty" json:"controller,omitempty"`
	// Hub is the old name of Controller, still read with a warning. normalize moves it to
	// Controller; it stays set only when the two disagree, which validate refuses.
	Hub string `yaml:"hub,omitempty" json:"-"`

	// roleAlias and hubAlias say the file used an old name, for Warnings.
	roleAlias, hubAlias bool
}

// IsController says this daemon is a controller.
func (c Cluster) IsController() bool { return c.Role == ClusterRoleController }

// Warnings names each old key or value the file used, and what replaces it.
func (c Cluster) Warnings() []string {
	var out []string
	if c.roleAlias {
		out = append(out, "cluster.role: hub is deprecated, read as controller: set cluster.role: controller")
	}
	if c.hubAlias {
		out = append(out, "cluster.hub is deprecated, read as cluster.controller: rename the key to cluster.controller")
	}
	return out
}

func normURL(u string) string {
	if n, err := NormalizeURL(u); err == nil {
		return n
	}
	return u
}

func (c *Cluster) normalize() {
	if c.Role == clusterRoleHub {
		c.Role, c.roleAlias = ClusterRoleController, true
	}
	if c.Controller != "" {
		c.Controller = normURL(c.Controller)
	}
	if c.Hub != "" {
		c.hubAlias = true
		hub := normURL(c.Hub)
		switch c.Controller {
		case "":
			c.Controller, c.Hub = hub, ""
		case hub:
			c.Hub = ""
		}
	}
}

func (c Cluster) validate(public *Public) []error {
	var errs []error
	switch c.Role {
	case "", ClusterRoleController:
	default:
		errs = append(errs, fmt.Errorf("cluster.role: %q is not %q or empty", c.Role, ClusterRoleController))
	}
	if c.Hub != "" {
		errs = append(errs, fmt.Errorf("cluster.hub (the old name of cluster.controller) is %q but cluster.controller is %q: keep only cluster.controller", c.Hub, c.Controller))
	}
	if c.Controller != "" {
		if _, err := NormalizeURL(c.Controller); err != nil {
			errs = append(errs, fmt.Errorf("cluster.controller: %q %v", c.Controller, err))
		}
	}
	if c.IsController() && c.Controller != "" {
		errs = append(errs, errors.New("cluster.role: controller and cluster.controller are both set; a daemon is a controller or links to one"))
	}
	if c.IsController() && !public.Enabled() {
		errs = append(errs, errors.New("cluster.role: controller needs daemon.public, because workers reach it over https"))
	}
	return errs
}
