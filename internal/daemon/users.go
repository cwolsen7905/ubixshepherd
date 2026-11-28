package daemon

// The users of a hosted daemon: added, given roles, signed out and removed by the operator
// on the daemon's own machine, like the owner account (account.go). The public guard
// answers the operator token 404, so none of this is reachable from the network.

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/auth"
)

// userRoute serves PathUsers and the routes under it, and reports whether it took the
// request.
func (s *Server) userRoute(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	switch {
	case path == api.PathUsers && r.Method == http.MethodGet:
		s.auth(guard("GET "+api.PathUsers, operatorOnly, s.userList)).ServeHTTP(w, r)
	case path == api.PathUsers && r.Method == http.MethodPost:
		w.Header().Set("Cache-Control", "no-store")
		s.auth(guard("POST "+api.PathUsers, operatorOnly, s.userAdd)).ServeHTTP(w, r)
	case strings.HasPrefix(path, api.PathUsers+"/") && r.Method == http.MethodPost:
		name, action, ok := strings.Cut(strings.TrimPrefix(path, api.PathUsers+"/"), "/")
		h := map[string]http.HandlerFunc{"password": s.userPassword, "role": s.userRole, "signout": s.userSignout, "remove": s.userRemove}[action]
		if !ok || name == "" || h == nil {
			return false
		}
		r.SetPathValue("name", name)
		w.Header().Set("Cache-Control", "no-store")
		s.auth(guard("POST "+api.PathUsers+"/{name}/"+action, operatorOnly, h)).ServeHTTP(w, r)
	default:
		return false
	}
	return true
}

// userError writes the answer for a user directory error.
func userError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrNoUser):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, auth.ErrUserExists), errors.Is(err, auth.ErrLastOwner):
		writeError(w, http.StatusConflict, err)
	default:
		writeError(w, http.StatusBadRequest, err)
	}
}

// named is the service of the user the path names, or nil with the error written.
func (s *Server) named(w http.ResponseWriter, r *http.Request) *auth.Service {
	svc := s.service(w, r)
	if svc == nil {
		return nil
	}
	if svc.Users == nil {
		writeError(w, http.StatusNotFound, errors.New("this store keeps one owner and no users"))
		return nil
	}
	u, err := svc.Users.UserByAlias(r.Context(), auth.AliasLocal, r.PathValue("name"))
	if err != nil {
		userError(w, err)
		return nil
	}
	return svc.ForUser(u)
}

func apiUser(u auth.User) api.User {
	out := api.User{Name: u.Name, HasPassword: u.HasPassword, Created: u.Created.UTC(), Memberships: []api.Membership{}}
	for _, m := range u.Members {
		out.Memberships = append(out.Memberships, api.Membership{Tenant: m.Tenant, Role: string(m.Role)})
	}
	return out
}

func (s *Server) userList(w http.ResponseWriter, r *http.Request) {
	svc := s.service(w, r)
	if svc == nil {
		return
	}
	out := api.UserList{Users: []api.User{}}
	if svc.Users == nil {
		out.Users = append(out.Users, apiUser(svc.User()))
		writeJSON(w, http.StatusOK, out)
		return
	}
	us, err := svc.Users.Users(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, u := range us {
		out.Users = append(out.Users, apiUser(u))
	}
	writeJSON(w, http.StatusOK, out)
}

// userAdd adds a user with their password: the reply is their second factor's enrolment
// and recovery codes, shown once. A password the service refuses adds nobody.
func (s *Server) userAdd(w http.ResponseWriter, r *http.Request) {
	svc := s.service(w, r)
	if svc == nil {
		return
	}
	var req api.UserAdd
	if !decode(w, r, &req) {
		return
	}
	if svc.Users == nil {
		writeError(w, http.StatusNotFound, errors.New("this store keeps one owner and no users"))
		return
	}
	if err := auth.CheckUserName(req.Name); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len([]rune(req.Password)) < auth.MinPasswordLen {
		writeError(w, http.StatusBadRequest, fmt.Errorf("the password needs at least %d characters", auth.MinPasswordLen))
		return
	}
	tenant := req.Tenant
	if tenant == "" {
		tenant = "default"
	}
	role := auth.RoleMember
	if req.Role != "" {
		var err error
		if role, err = auth.ParseRole(req.Role); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	u, err := svc.Users.AddUser(r.Context(), req.Name, tenant, role, time.Now())
	if err != nil {
		userError(w, err)
		return
	}
	setup, err := svc.ForUser(u).SetPassword(r.Context(), req.Password)
	if err != nil {
		svc.Users.RemoveUser(r.Context(), u.ID)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.Log.Info("user added", "user", u.Name, "tenant", tenant, "role", string(role))
	writeJSON(w, http.StatusOK, api.AccountSetup{URI: setup.Enrolment.URI, Secret: setup.Enrolment.Secret, RecoveryCodes: setup.RecoveryCodes})
}

// userPassword sets a user's password, a new second factor and recovery codes, and signs
// them out everywhere, as `account set` does for the owner.
func (s *Server) userPassword(w http.ResponseWriter, r *http.Request) {
	us := s.named(w, r)
	if us == nil {
		return
	}
	var req api.UserPassword
	if !decode(w, r, &req) {
		return
	}
	setup, err := us.SetPassword(r.Context(), req.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if h := s.hostedNow(); h != nil && us.User().ID == auth.OwnerID {
		h.once.Do(func() { close(h.accountSet) })
	}
	s.Log.Info("user password set; their sessions and device tokens ended", "user", us.User().Name)
	writeJSON(w, http.StatusOK, api.AccountSetup{URI: setup.Enrolment.URI, Secret: setup.Enrolment.Secret, RecoveryCodes: setup.RecoveryCodes})
}

func (s *Server) userRole(w http.ResponseWriter, r *http.Request) {
	us := s.named(w, r)
	if us == nil {
		return
	}
	var req api.UserRole
	if !decode(w, r, &req) {
		return
	}
	var role auth.Role
	if req.Role != "none" {
		var err error
		if role, err = auth.ParseRole(req.Role); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	if req.Tenant == "" {
		req.Tenant = "default"
	}
	if err := us.Users.SetRole(r.Context(), us.User().ID, req.Tenant, role); err != nil {
		userError(w, err)
		return
	}
	u, err := us.Users.User(r.Context(), us.User().ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("user role set", "user", u.Name, "tenant", req.Tenant, "role", req.Role)
	writeJSON(w, http.StatusOK, apiUser(u))
}

// userSignout ends one user's sessions and device tokens: an operator signing another
// person out.
func (s *Server) userSignout(w http.ResponseWriter, r *http.Request) {
	us := s.named(w, r)
	if us == nil {
		return
	}
	sessions, devices, err := us.SignOutEverywhere(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("user signed out everywhere", "user", us.User().Name, "sessions", sessions, "devices", devices)
	writeJSON(w, http.StatusOK, api.SignedOutEverywhere{Sessions: sessions, Devices: devices})
}

// userRemove removes a user and everything of theirs. Their live sessions end first, so
// nothing of theirs outlasts the row.
func (s *Server) userRemove(w http.ResponseWriter, r *http.Request) {
	us := s.named(w, r)
	if us == nil {
		return
	}
	u := us.User()
	if err := us.Users.RemoveUser(r.Context(), u.ID); err != nil {
		userError(w, err)
		return
	}
	n := s.web.endUser(u.ID)
	s.Log.Info("user removed", "user", u.Name, "sessions", n)
	writeJSON(w, http.StatusOK, api.UserRemoved{Removed: true, Sessions: n})
}
