package daemon

import (
	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/auth"
)

// The least role a user needs in the tenant for each route, in one table: guard panics on
// a pattern missing from it, so a new route is classified before it is served. A viewer
// reads; a member opens lanes, starts runs and answers decisions; an admin changes the
// tenant's settings and users; an owner may do what an admin may. The access bits in
// Handler still say which kinds of token may call a route at all: an admin's browser
// cannot call an operator-only route.
//
// A deployment route is the daemon's, not a tenant's: its config file, its binary, its
// cluster, its sign-in and users, and its one front desk and keep-going controller. Only
// the default tenant's users may call one for now.
var (
	viewer = routeRule{min: auth.RoleViewer}
	member = routeRule{min: auth.RoleMember}
	admin  = routeRule{min: auth.RoleAdmin}

	deployViewer = routeRule{min: auth.RoleViewer, deployment: true}
	deployMember = routeRule{min: auth.RoleMember, deployment: true}
	deployAdmin  = routeRule{min: auth.RoleAdmin, deployment: true}
)

var routeRules = map[string]routeRule{
	"GET " + api.PathStatus: viewer,

	// The cluster is the deployment's.
	"GET " + api.PathCluster:                                deployViewer,
	"POST " + api.PathClusterWorkers:                        deployAdmin,
	"POST " + api.PathClusterWorkers + "/{name}/revoke":     deployAdmin,
	"POST " + api.PathClusterWorkers + "/{name}/remove":     deployAdmin,
	"POST " + api.PathClusterWorkers + "/{name}/confirm":    deployAdmin,
	"POST " + api.PathClusterMembersOld:                     deployAdmin,
	"POST " + api.PathClusterMembersOld + "/{name}/revoke":  deployAdmin,
	"POST " + api.PathClusterMembersOld + "/{name}/remove":  deployAdmin,
	"POST " + api.PathClusterMembersOld + "/{name}/confirm": deployAdmin,
	"POST " + api.PathClusterJoin:                           deployAdmin,

	// The binary and the daemon itself.
	"GET " + api.PathUpdate:         deployViewer,
	"POST " + api.PathUpdateDismiss: deployAdmin,
	"POST " + api.PathUpdateUpgrade: deployAdmin,
	"POST " + api.PathShutdown:      deployAdmin,
	"POST " + api.PathDrain:         deployAdmin,

	"GET " + api.PathWorkspaces:                        viewer,
	"POST " + api.PathWorkspaces:                       admin,
	"POST " + api.PathWorkspaces + "/{id}/rename":      admin,
	"GET " + api.PathRepos:                             viewer,
	"POST " + api.PathRepos:                            admin,
	"POST " + api.PathRepos + "/{id}/unmanage":         admin,
	"POST /v1/repos/{id}/hook":                         admin,
	"GET " + api.PathResolve:                           viewer,
	"GET " + api.PathLanes:                             viewer,
	"POST " + api.PathLanes:                            member,
	"POST " + api.PathLanes + "/{id}/close":            member,
	"POST " + api.PathLanes + "/{id}/scope":            member,
	"GET " + pathLaneApproval:                          viewer,
	"POST " + pathLaneApprove:                          member,
	"POST " + api.PathLanes + "/{id}/widen":            member,
	"GET " + api.PathChecks:                            viewer,
	"POST " + api.PathChecksApprove:                    member,
	"POST " + api.PathChecksRelease:                    member,
	"POST " + api.PathLanes + "/{id}/ship":             member,
	"GET " + api.PathLanes + "/{id}/conversation":      viewer,
	"GET " + api.PathLanes + "/{id}/diff":              viewer,
	"GET " + api.PathLanes + "/{id}/artifacts":         viewer,
	"GET " + api.PathLanes + "/{id}/artifacts/content": viewer,
	"GET " + api.PathLanes + "/{id}/files":             viewer,
	"POST " + api.PathLanes + "/{id}/gate":             member,
	"GET " + api.PathFoldGC:                            deployViewer,
	"POST " + api.PathPrePush:                          member,

	"POST " + api.PathRuns:                        member,
	"GET " + api.PathRuns:                         viewer,
	"GET " + api.PathHistoryLanes:                 viewer,
	"GET " + api.PathHistoryRuns:                  viewer,
	"GET " + api.PathRuns + "/{id}":               viewer,
	"GET " + api.PathRuns + "/{id}/log":           viewer,
	"GET " + api.PathRuns + "/{id}/diagnoses":     viewer,
	"POST " + api.PathRuns + "/{id}/fix":          member,
	"POST " + api.PathRuns + "/{id}/stop":         member,
	"POST " + api.PathRuns + "/{id}/dismiss":      member,
	"POST " + api.PathRuns + "/{id}/undismiss":    member,
	"POST " + api.PathRuns + "/{id}/events":       member,
	"GET " + api.PathRuns + "/{id}/events":        viewer,
	"POST " + api.PathRuns + "/{id}/decisions":    member,
	"POST " + api.PathRuns + "/{id}/artifacts":    member,
	"POST " + api.PathRuns + "/{id}/requests":     member,
	"GET " + api.PathArtifacts:                    viewer,
	"GET " + api.PathArtifacts + "/{id}":          viewer,
	"GET " + api.PathArtifacts + "/{id}/content":  viewer,
	"PATCH " + api.PathArtifacts + "/{id}":        member,
	"POST " + api.PathArtifacts + "/{id}/discard": member,
	"POST " + api.PathArtifacts + "/gc":           deployAdmin,

	"GET " + api.PathDecisions:                   viewer,
	"POST " + api.PathDecisions + "/{id}/answer": member,
	"GET " + api.PathFeed:                        viewer,
	"GET " + api.PathFeedStream:                  viewer,

	"POST " + api.PathFoldImport:             admin,
	"POST " + api.PathFoldView:               admin,
	"POST " + api.PathFoldReview:             member,
	"POST " + api.PathFoldRetire:             admin,
	"POST " + api.PathSessionsImport:         admin,
	"GET " + api.PathSessions:                viewer,
	"POST " + api.PathSessions + "/{id}/ask": member,
	"GET " + api.PathTags:                    viewer,
	"POST " + api.PathTagsReserve:            member,
	"POST " + api.PathTagsRelease:            member,
	"GET " + api.PathSpend:                   viewer,
	"GET " + api.PathUsage:                   viewer,
	"GET " + api.PathReviewBots:              deployViewer,
	"POST " + api.PathSpend:                  admin,
	"GET " + api.PathSettings + "/{key}":     admin,
	"PUT " + api.PathSettings + "/{key}":     admin,

	// The config file is the deployment's.
	"GET " + api.PathConfig:         deployAdmin,
	"POST " + api.PathConfigPreview: deployAdmin,
	"PUT " + api.PathConfig:         deployAdmin,

	"GET " + api.PathProjects:                            viewer,
	"GET " + api.PathProjects + "/{name}":                viewer,
	"GET " + api.PathProjectsUnassigned:                  viewer,
	"GET " + api.PathProjects + "/{name}/work":           viewer,
	"GET " + api.PathUnassignedWork:                      viewer,
	"GET " + api.PathProjects + "/{name}/brief":          viewer,
	"POST " + api.PathProjects + "/{name}/brief/draft":   member,
	"POST " + api.PathProjects + "/{name}/brief/approve": admin,

	// Keep-going mode is one controller for the daemon.
	"GET " + api.PathKeepGoing:  deployViewer,
	"POST " + api.PathKeepGoing: deployMember,

	"GET " + api.PathCrookVerdicts:               viewer,
	"GET " + api.PathCrookVerdicts + "/{id}":     viewer,
	"GET " + api.PathCrookRules:                  viewer,
	"GET " + api.PathNext:                        viewer,
	"POST " + api.PathNext:                       member,
	"GET " + api.PathNext + "/{id}":              viewer,
	"PATCH " + api.PathNext + "/{id}":            member,
	"GET " + api.PathWaiting:                     viewer,
	"GET " + api.PathQuestions:                   viewer,
	"POST " + api.PathQuestions:                  member,
	"POST " + api.PathQuestions + "/{id}/answer": member,
	"POST " + api.PathQuestions + "/{id}/skip":   member,
	"GET " + api.PathTopics:                      viewer,
	"POST " + api.PathTopics:                     member,
	"GET " + api.PathTopics + "/{id}":            viewer,
	"PATCH " + api.PathTopics + "/{id}":          member,
	"GET " + api.PathRequests:                    viewer,
	"POST " + api.PathRequests + "/{id}/route":   member,
	"POST " + api.PathRequests + "/{id}/close":   member,

	// The front desk is one for the daemon until each tenant has its own.
	"POST " + api.PathDeskTurn:                 deployMember,
	"GET " + api.PathDeskStream:                deployViewer,
	"GET " + api.PathDeskHistory:               deployViewer,
	"GET " + api.PathDeskStatus:                deployViewer,
	"POST " + api.PathDeskInterrupt:            deployMember,
	"POST " + api.PathDeskNew:                  deployMember,
	"POST " + api.PathDeskAttachments:          deployMember,
	"GET " + api.PathDeskAttachments + "/{id}": deployViewer,

	// Sign-in, sessions, the account and users. A user's own session and devices are
	// theirs at any role; the rest is the operator's on the daemon's own machine.
	"POST " + api.PathWebSignin:                  deployAdmin,
	"GET " + api.PathWebSession:                  viewer,
	"POST " + api.PathWebSignout:                 viewer,
	"DELETE " + api.PathWebSessions:              deployAdmin,
	"POST " + api.PathAccount:                    deployAdmin,
	"GET " + api.PathDevices:                     viewer,
	"POST " + api.PathWebSignoutAll:              viewer,
	"POST " + api.PathDevices + "/{id}/revoke":   viewer,
	"GET " + api.PathUsers:                       deployAdmin,
	"POST " + api.PathUsers:                      deployAdmin,
	"POST " + api.PathUsers + "/{name}/password": deployAdmin,
	"POST " + api.PathUsers + "/{name}/role":     deployAdmin,
	"POST " + api.PathUsers + "/{name}/signout":  deployAdmin,
	"POST " + api.PathUsers + "/{name}/remove":   deployAdmin,
}

// outsideMux are the guarded routes served before the API's mux (accountRoute and
// userRoute): the tests that walk every route add them to Handler's.
var outsideMux = []string{
	"POST " + api.PathAccount,
	"GET " + api.PathDevices,
	"POST " + api.PathWebSignoutAll,
	"POST " + api.PathDevices + "/{id}/revoke",
	"GET " + api.PathUsers,
	"POST " + api.PathUsers,
	"POST " + api.PathUsers + "/{name}/password",
	"POST " + api.PathUsers + "/{name}/role",
	"POST " + api.PathUsers + "/{name}/signout",
	"POST " + api.PathUsers + "/{name}/remove",
}
