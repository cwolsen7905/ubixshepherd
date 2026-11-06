package auth

import (
	"context"
	"sync"
	"time"
)

// MemStore is a Store in memory, for tests and for a daemon test harness.
type MemStore struct {
	mu       sync.Mutex
	acct     *Account
	factors  map[string]FactorRecord
	recovery []RecoveryCode
	devices  map[string]Device
}

// NewMemStore is an empty store.
func NewMemStore() *MemStore {
	return &MemStore{factors: map[string]FactorRecord{}, devices: map[string]Device{}}
}

func (m *MemStore) Account(context.Context) (Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.acct == nil {
		return Account{}, ErrNoAccount
	}
	return *m.acct, nil
}

func (m *MemStore) PutAccount(_ context.Context, a Account) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acct = &a
	return nil
}

func (m *MemStore) Factors(context.Context) ([]FactorRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []FactorRecord
	for _, f := range m.factors {
		out = append(out, f)
	}
	sortFactors(out)
	return out, nil
}

func (m *MemStore) PutFactor(_ context.Context, f FactorRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.factors[f.ID] = f
	return nil
}

func (m *MemStore) DeleteFactor(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.factors, id)
	return nil
}

func (m *MemStore) AdvanceStep(_ context.Context, id string, step int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.factors[id]
	if !ok || step <= f.LastStep {
		return false, nil
	}
	f.LastStep = step
	m.factors[id] = f
	return true, nil
}

func (m *MemStore) ReplaceRecovery(_ context.Context, codes []RecoveryCode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recovery = append([]RecoveryCode(nil), codes...)
	return nil
}

func (m *MemStore) RecoveryCodes(context.Context) ([]RecoveryCode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]RecoveryCode(nil), m.recovery...), nil
}

func (m *MemStore) UseRecovery(_ context.Context, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.recovery {
		if m.recovery[i].ID == id && !m.recovery[i].Used {
			m.recovery[i].Used = true
			return true, nil
		}
	}
	return false, nil
}

func (m *MemStore) PutDevice(_ context.Context, d Device) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.devices[d.ID] = d
	return nil
}

func (m *MemStore) DeviceByHash(_ context.Context, hash string) (Device, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.devices {
		if d.Hash == hash {
			return d, true, nil
		}
	}
	return Device{}, false, nil
}

func (m *MemStore) TouchDevice(_ context.Context, id string, used, expires time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.devices[id]; ok {
		d.LastUsed, d.Expires = used, expires
		m.devices[id] = d
	}
	return nil
}

func (m *MemStore) Devices(context.Context) ([]Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Device
	for _, d := range m.devices {
		out = append(out, d)
	}
	sortDevices(out)
	return out, nil
}

func (m *MemStore) DeleteDevice(_ context.Context, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.devices[id]
	delete(m.devices, id)
	return ok, nil
}

func (m *MemStore) DeleteDevices(context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.devices)
	m.devices = map[string]Device{}
	return n, nil
}

func (m *MemStore) PruneDevices(_ context.Context, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, d := range m.devices {
		if !d.Expires.After(now) {
			delete(m.devices, id)
		}
	}
	return nil
}
