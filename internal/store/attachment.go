package store

import "time"

// Attachment is a file the person put in a desk message: what it is called, what it is,
// and the hash its bytes are kept under (see internal/attach). Turn is 0 until a message
// takes it.
type Attachment struct {
	ID          int64     `json:"id"`
	WorkspaceID int64     `json:"-"`
	Turn        int64     `json:"-"`
	SHA         string    `json:"-"`
	Name        string    `json:"name"`
	Mime        string    `json:"mime"`
	Kind        string    `json:"kind"`
	Size        int64     `json:"size"`
	Created     time.Time `json:"-"`
}
