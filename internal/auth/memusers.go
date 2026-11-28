package auth

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemUsers is a Users in memory, for tests: each user has a MemStore, and the one tenant
// is "default" unless Tenants names more.
type MemUsers struct {
	mu      sync.Mutex
	next    int64
	users   map[int64]*memUser
	Tenants []string
}

type memUser struct {
	u  User
	st *MemStore
}

// NewMemUsers is a directory holding only the owner, with no password yet, and owner's store
// st (a new one when nil).
func NewMemUsers(st *MemStore) *MemUsers {
	if st == nil {
		st = NewMemStore()
	}
	m := &MemUsers{next: OwnerID + 1, users: map[int64]*memUser{}, Tenants: []string{"default"}}
	m.users[OwnerID] = &memUser{u: User{ID: OwnerID, Name: "owner", Members: []Membership{{Tenant: "default", Role: RoleOwner}}}, st: st}
	return m
}

func (m *MemUsers) ForUser(id int64) Store {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[id]; ok {
		return u.st
	}
	return NewMemStore() // a user removed since: an empty store, which has no account
}

func (m *MemUsers) view(u *memUser) User {
	out := u.u
	out.Members = append([]Membership(nil), u.u.Members...)
	_, err := u.st.Account(context.Background())
	out.HasPassword = err == nil
	return out
}

func (m *MemUsers) UserByAlias(_ context.Context, method, subject string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if method != AliasLocal {
		return User{}, ErrNoUser
	}
	for _, u := range m.users {
		if u.u.Name == subject {
			return m.view(u), nil
		}
	}
	return User{}, ErrNoUser
}

func (m *MemUsers) User(_ context.Context, id int64) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[id]; ok {
		return m.view(u), nil
	}
	return User{}, ErrNoUser
}

func (m *MemUsers) Users(context.Context) ([]User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []User
	for _, u := range m.users {
		out = append(out, m.view(u))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *MemUsers) hasTenant(t string) bool {
	for _, x := range m.Tenants {
		if x == t {
			return true
		}
	}
	return false
}

func (m *MemUsers) AddUser(_ context.Context, name, tenant string, role Role, now time.Time) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.hasTenant(tenant) {
		return User{}, ErrNoTenant
	}
	for _, u := range m.users {
		if u.u.Name == name {
			return User{}, ErrUserExists
		}
	}
	u := &memUser{u: User{ID: m.next, Name: name, Created: now, Updated: now, Members: []Membership{{Tenant: tenant, Role: role}}}, st: NewMemStore()}
	m.users[m.next] = u
	m.next++
	return m.view(u), nil
}

// soleOwner says whether the user is the tenant's only owner. The caller holds mu.
func (m *MemUsers) soleOwner(id int64, tenant string) bool {
	var owners []int64
	for _, u := range m.users {
		for _, ms := range u.u.Members {
			if ms.Tenant == tenant && ms.Role == RoleOwner {
				owners = append(owners, u.u.ID)
			}
		}
	}
	return len(owners) == 1 && owners[0] == id
}

func (m *MemUsers) RemoveUser(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[id]; !ok {
		return ErrNoUser
	}
	for _, ms := range m.users[id].u.Members {
		if m.soleOwner(id, ms.Tenant) {
			return ErrLastOwner
		}
	}
	delete(m.users, id)
	return nil
}

func (m *MemUsers) SetRole(_ context.Context, id int64, tenant string, role Role) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return ErrNoUser
	}
	if !m.hasTenant(tenant) {
		return ErrNoTenant
	}
	if role != RoleOwner && m.soleOwner(id, tenant) {
		return ErrLastOwner
	}
	var ms []Membership
	for _, x := range u.u.Members {
		if x.Tenant != tenant {
			ms = append(ms, x)
		}
	}
	if role != "" {
		ms = append(ms, Membership{Tenant: tenant, Role: role})
	}
	u.u.Members = ms
	return nil
}

func (m *MemUsers) DeviceUser(ctx context.Context, hash string) (int64, bool, error) {
	m.mu.Lock()
	users := make(map[int64]*MemStore, len(m.users))
	for id, u := range m.users {
		users[id] = u.st
	}
	m.mu.Unlock()
	for id, st := range users {
		if _, found, err := st.DeviceByHash(ctx, hash); err != nil || found {
			return id, found, err
		}
	}
	return 0, false, nil
}
