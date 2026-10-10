package api

// The config editor: read Shepherd's settings, preview a change and save it.
//
//	GET  /v1/config          the file as written, what is in force, and the form's field
//	                         description: ConfigDoc
//	POST /v1/config/preview  validate a change and show its diff, saving nothing:
//	                         ConfigChange in, ConfigPreview out
//	PUT  /v1/config          validate, then write (atomically, keeping a backup) and
//	                         reload: ConfigChange in, ConfigSaved out
//
// A browser session may read and preview. It may save only when it was signed in with
// WebSignin.AllowSettings; any other session is refused with 403. The front desk and
// workers are refused every one of these. A save is refused with
//
//	409 the file changed on disk since ETag was read (reload and apply again)
//	422 the result is not a valid config (ConfigRefusal.Problems)
//	428 the change raises what agents may do and its paths are not all in Confirm
//	    (ConfigRefusal.Risky)
const (
	PathConfig        = "/v1/config"
	PathConfigPreview = "/v1/config/preview"
)

// ConfigOp is one change: set Path (keys from the file's top, ["repos", "api", "gate"])
// to Value, or with Unset remove it so the value is inherited or defaulted again.
type ConfigOp struct {
	Path  []string `json:"path"`
	Value any      `json:"value,omitempty"`
	Unset bool     `json:"unset,omitempty"`
}

// ConfigChange is a request to preview or save changes to the file read at ETag.
type ConfigChange struct {
	ETag string     `json:"etag"`
	Ops  []ConfigOp `json:"ops"`
	// Confirm lists the paths of ConfigRisk the person confirmed.
	Confirm []string `json:"confirm,omitempty"`
}

// ConfigRisk is a change that gives agents more power or removes a guard.
type ConfigRisk struct {
	Path string `json:"path"`
	From string `json:"from"`
	To   string `json:"to"`
	Why  string `json:"why"`
}

// ConfigPreview answers a preview: the unified diff of config.yaml, what is wrong with
// the result (nothing, for a change that would save), and what needs confirming.
type ConfigPreview struct {
	ETag   string       `json:"etag"`
	Diff   string       `json:"diff"`
	Errors []string     `json:"errors,omitempty"`
	Risky  []ConfigRisk `json:"risky,omitempty"`
}

// ConfigRefusal is the body of a 409, 422 or 428 from a save; Error is the usual line.
type ConfigRefusal struct {
	Error    string       `json:"error"`
	Problems []string     `json:"problems,omitempty"`
	Risky    []ConfigRisk `json:"risky,omitempty"`
}

// ConfigSaved answers a save. ReloadError is set when the file was written but the
// daemon did not take it up (it should not happen: the write was validated first).
type ConfigSaved struct {
	ETag        string `json:"etag"`
	Diff        string `json:"diff"`
	Backup      string `json:"backup,omitempty"`
	Reloaded    bool   `json:"reloaded"`
	ReloadError string `json:"reload_error,omitempty"`
}

// ConfigProfile is one profile: the keys the file sets, and the values in force after
// inheriting. Overridden lists the dotted paths set in the file (autonomy.push).
type ConfigProfile struct {
	Raw        map[string]any `json:"raw"`
	Effective  map[string]any `json:"effective"`
	Overridden []string       `json:"overridden"`
}

// ConfigDoc answers GET /v1/config.
type ConfigDoc struct {
	Path string `json:"path"`
	// ETag identifies the file as read; send it back with a change.
	ETag string `json:"etag"`
	// Missing: no file yet; the first save creates it from the template.
	Missing bool `json:"missing,omitempty"`
	// CanWrite says whether this caller may save; Why says what to do when not.
	CanWrite bool   `json:"can_write"`
	Why      string `json:"why,omitempty"`
	// Invalid lists what is wrong with the file as it stands (a hand edit): Effective is
	// then the config the daemon is running on, not the file's.
	Invalid []string `json:"invalid,omitempty"`
	// Raw is the file as written, parsed; Effective is the config it gives after
	// defaults.
	Raw       map[string]any `json:"raw"`
	Effective map[string]any `json:"effective"`
	Defaults  ConfigProfile  `json:"defaults"`
	// Repos are the profiles the file names, by repo; Projects are as written, with
	// their budget resolved against the daemon's in Effective.
	Repos    map[string]ConfigProfile `json:"repos"`
	Projects map[string]any           `json:"projects"`
	// Sections describe the form: which fields exist, their kind, choices and help.
	Sections []ConfigSection `json:"sections"`
}

// ConfigSection is a group of fields that sit together in the file. A profile section
// applies at defaults and at repos.<name>; a project section at projects.<name>.
type ConfigSection struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Root is where the section's keys start in the file; Many says its parent is a map
	// keyed by name, so the root is Root + [name].
	Root []string `json:"root"`
	Many bool     `json:"many,omitempty"`
	// Inherits is the section whose values fill in what a named entry leaves out.
	Inherits string        `json:"inherits,omitempty"`
	Fields   []ConfigField `json:"fields"`
}

// Field kinds.
const (
	KindString = "string" // one line
	KindText   = "text"   // several lines
	KindNumber = "number"
	KindBool   = "bool"
	KindEnum   = "enum" // one of Choices
	KindList   = "list" // of strings
	KindMap    = "map"  // string to string
	KindJSON   = "json" // structured: edited as JSON, saved as YAML
)

// ConfigField describes one setting. Key is its path inside the section.
type ConfigField struct {
	Key     []string `json:"key"`
	Kind    string   `json:"kind"`
	Label   string   `json:"label"`
	Help    string   `json:"help,omitempty"`
	Group   string   `json:"group,omitempty"`
	Choices []string `json:"choices,omitempty"`
	// Nullable: leaving it out is a value of its own ("no limit", "the CLI's default").
	Nullable bool `json:"nullable,omitempty"`
	// Risk: raising it gives agents more power, so a save needs a confirm.
	Risk bool `json:"risk,omitempty"`
	// Restart: a change applies when the daemon next starts, not on reload.
	Restart bool `json:"restart,omitempty"`
}
