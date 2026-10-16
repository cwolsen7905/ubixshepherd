package config

import (
	"strings"
	"testing"
)

func TestNextExpiry(t *testing.T) {
	c, err := Parse([]byte(""))
	if err != nil || c.Next.Expiry() != 14 {
		t.Fatalf("default = %d, %v", c.Next.Expiry(), err)
	}
	c, err = Parse([]byte("next:\n  expire_days: 30\n"))
	if err != nil || c.Next.Expiry() != 30 {
		t.Errorf("30 = %d, %v", c.Next.Expiry(), err)
	}
	c, err = Parse([]byte("next:\n  expire_days: 0\n"))
	if err != nil || c.Next.Expiry() != 0 {
		t.Errorf("0 turns it off: %d, %v", c.Next.Expiry(), err)
	}
	if _, err := Parse([]byte("next:\n  expire_days: -1\n")); err == nil || !strings.Contains(err.Error(), "next.expire_days") {
		t.Errorf("negative: %v", err)
	}
}
