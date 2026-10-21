package config

import "fmt"

// Muster defaults, in days. They are the proposed values for the upkeep pass.
const (
	DefaultMusterStaleDays  = 3
	DefaultMusterBatchDays  = 3
	DefaultMusterDigestDays = 3
	DefaultMusterSkipDays   = 7
	DefaultMusterUndoDays   = 7
)

// Muster holds the upkeep pass's settings. A field left unset takes its default; a
// zero in StaleDays, BatchDays, DigestDays or SkipDays turns that check off.
type Muster struct {
	// Off turns the whole pass off.
	Off bool `yaml:"off,omitempty" json:"off,omitempty"`
	// StaleDays: a lane with no activity for this long is marked stale.
	StaleDays *int `yaml:"stale_days,omitempty" json:"stale_days,omitempty"`
	// BatchDays: an artifact undecided for this long joins one batched decision.
	BatchDays *int `yaml:"batch_days,omitempty" json:"batch_days,omitempty"`
	// DigestDays: a question unanswered this long goes into the digest.
	DigestDays *int `yaml:"digest_days,omitempty" json:"digest_days,omitempty"`
	// SkipDays: this many more days after the digest, it is marked skipped (stale).
	SkipDays *int `yaml:"skip_days,omitempty" json:"skip_days,omitempty"`
	// UndoDays: how long a muster action can be undone.
	UndoDays *int `yaml:"undo_days,omitempty" json:"undo_days,omitempty"`
}

func orDefault(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// Stale is the effective stale-lane days; 0 means never.
func (m Muster) Stale() int { return orDefault(m.StaleDays, DefaultMusterStaleDays) }

// Batch is the effective undecided-artifact days; 0 means never.
func (m Muster) Batch() int { return orDefault(m.BatchDays, DefaultMusterBatchDays) }

// Digest is the effective days before a question goes into the digest; 0 means never.
func (m Muster) Digest() int { return orDefault(m.DigestDays, DefaultMusterDigestDays) }

// Skip is the effective days after the digest before a question is skipped; 0 means never.
func (m Muster) Skip() int { return orDefault(m.SkipDays, DefaultMusterSkipDays) }

// Undo is the effective undo window in days.
func (m Muster) Undo() int { return orDefault(m.UndoDays, DefaultMusterUndoDays) }

func (m Muster) validate() []error {
	var errs []error
	for _, f := range []struct {
		name string
		v    *int
	}{{"stale_days", m.StaleDays}, {"batch_days", m.BatchDays}, {"digest_days", m.DigestDays}, {"skip_days", m.SkipDays}, {"undo_days", m.UndoDays}} {
		if f.v != nil && *f.v < 0 {
			errs = append(errs, fmt.Errorf("muster.%s: %d is negative; 0 turns the check off", f.name, *f.v))
		}
	}
	return errs
}
