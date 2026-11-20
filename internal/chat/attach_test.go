package chat

import (
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

func TestAttachShowsTheKeepGoingSummary(t *testing.T) {
	m, _, a := newTestModel()
	a.keep = api.KeepGoingStatus{Summary: "Since keep-going was turned on at Mon 09:00:\n  started: 1\n"}
	drive(t, m, m.loadKeepGoingSummary())
	if !has(m.Lines(), KindInfo, "Since keep-going was turned on") {
		t.Errorf("thread = %+v", m.Lines())
	}
}

func TestAttachSaysNothingWhenKeepGoingNeverRan(t *testing.T) {
	m, _, _ := newTestModel()
	before := len(m.Lines())
	drive(t, m, m.loadKeepGoingSummary())
	if len(m.Lines()) != before {
		t.Errorf("thread = %+v", m.Lines())
	}
}

func TestInitShowsTheKeepGoingSummary(t *testing.T) {
	m, _, a := newTestModel()
	a.keep = api.KeepGoingStatus{Summary: "Since keep-going was turned on at Mon 09:00:\n  started: 1\n"}
	drive(t, m, m.Init())
	if !has(m.Lines(), KindInfo, "Since keep-going was turned on") {
		t.Errorf("thread = %+v", m.Lines())
	}
}
