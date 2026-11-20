package api

import "github.com/ubixsys/ubixshepherd/internal/reviewbot"

// The review bots page (docs/review-bots.md): what each configured review bot did on the
// forge's merge requests, per project.
//
//	GET /v1/review-bots            ReviewBots
//	GET /v1/review-bots?refresh=1  the same, read again from the forge
//
// Read only. The daemon reads the forge through its CLI and keeps the answer for a few
// minutes; a repo it could not read carries its error and zero counts.
const PathReviewBots = "/v1/review-bots"

// ReviewBots is the body of GET /v1/review-bots: one entry per configured bot, empty when
// none is configured.
type ReviewBots = reviewbot.Report
