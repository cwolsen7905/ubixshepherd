package config

import "fmt"

// DefaultNextExpireDays is how long a proposed next-up item may wait for the person's
// approval before it expires to deferred.
const DefaultNextExpireDays = 14

// Next holds the next-up list's settings.
type Next struct {
	// ExpireDays is the days a proposed item (one the desk or an agent put up) may stay
	// unapproved before it is deferred; 0 turns expiry off. Unset is DefaultNextExpireDays.
	ExpireDays *int `yaml:"expire_days,omitempty" json:"expire_days,omitempty"`
}

// Expiry is the effective expiry in days; 0 means never.
func (n Next) Expiry() int {
	if n.ExpireDays == nil {
		return DefaultNextExpireDays
	}
	return *n.ExpireDays
}

func (n Next) validate() []error {
	if n.ExpireDays != nil && *n.ExpireDays < 0 {
		return []error{fmt.Errorf("next.expire_days: %d is negative; 0 turns expiry off", *n.ExpireDays)}
	}
	return nil
}
