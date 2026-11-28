package api

import "time"

// PathUsers is the users of a hosted daemon (operator, on the daemon's own machine):
//
//	GET  /v1/users                  the users and their memberships
//	POST /v1/users                  add one with a password: the reply is their setup, shown once
//	POST /v1/users/{name}/password  set a new password, second factor and recovery codes
//	POST /v1/users/{name}/role      give a role in a tenant, or take the membership away
//	POST /v1/users/{name}/signout   end every session and device token of theirs
//	POST /v1/users/{name}/remove    remove them, and everything of theirs
const PathUsers = "/v1/users"

// User is a user as listed.
type User struct {
	Name        string       `json:"name"`
	HasPassword bool         `json:"has_password"`
	Created     time.Time    `json:"created"`
	Memberships []Membership `json:"memberships"`
}

// Membership is a role in a tenant.
type Membership struct {
	Tenant string `json:"tenant"`
	Role   string `json:"role"`
}

// UserList is GET /v1/users.
type UserList struct {
	Users []User `json:"users"`
}

// UserAdd is the body of POST /v1/users. Tenant is "default" and Role "member" when empty.
type UserAdd struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	Tenant   string `json:"tenant,omitempty"`
	Role     string `json:"role,omitempty"`
}

// UserPassword is the body of POST /v1/users/{name}/password.
type UserPassword struct {
	Password string `json:"password"`
}

// UserRole is the body of POST /v1/users/{name}/role. Role "none" takes the membership away.
type UserRole struct {
	Tenant string `json:"tenant"`
	Role   string `json:"role"`
}

// UserRemoved is the reply of POST /v1/users/{name}/remove.
type UserRemoved struct {
	Removed  bool `json:"removed"`
	Sessions int  `json:"sessions"`
}
