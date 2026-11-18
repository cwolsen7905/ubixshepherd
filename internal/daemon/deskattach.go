package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/attach"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// attachKeep is how long an uploaded file waits for the message it was meant for.
const attachKeep = 24 * time.Hour

// attachMu orders the messages that carry files, so one file is not sent twice.
var attachMu sync.Mutex

// uploadMu orders uploads and the sweep they run: a sweep never removes the bytes of a
// file whose row is not yet written.
var uploadMu sync.Mutex

// deskAttachRoutes registers the attachment routes; Handler calls it with its own
// registration function.
func (s *Server) deskAttachRoutes(handle func(string, access, http.HandlerFunc)) {
	handle("POST "+api.PathDeskAttachments, forWeb, s.deskAttachUpload)
	handle("GET "+api.PathDeskAttachments+"/{id}", forWeb, s.deskAttachGet)
}

// attachments is where the files of desk messages are kept: "attachments" under the
// Shepherd home.
func (s *Server) attachments() (attach.Store, error) {
	home, err := paths.Home()
	if err != nil {
		return attach.Store{}, err
	}
	return attach.Store{Dir: filepath.Join(home, "attachments")}, nil
}

// deskAttachUpload keeps one file for the person's next message (api.PathDeskAttachments).
func (s *Server) deskAttachUpload(w http.ResponseWriter, r *http.Request) {
	d, ok := s.deskFor(w, r, 0)
	if !ok {
		return
	}
	files, err := s.attachments()
	if err != nil {
		s.fail(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, attach.MaxFileBytes+(1<<20))
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("send the file as multipart form data"))
		return
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			writeError(w, http.StatusBadRequest, errors.New("no file in the request"))
			return
		}
		if err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Errorf("the upload is over the %d MiB limit or broken", attach.MaxFileBytes>>20))
			return
		}
		if part.FormName() != "file" {
			continue
		}
		// The body is read and hashed before the lock, so a slow client holds nothing up.
		staged, err := files.Stage(part.FileName(), part)
		switch {
		case errors.Is(err, attach.ErrRefused):
			writeError(w, http.StatusBadRequest, err)
			return
		case err != nil:
			s.fail(w, err)
			return
		}
		a, err := s.keepUpload(r.Context(), d.Workspace().ID, staged)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, a)
		return
	}
}

// keepUpload stores staged bytes and writes their row as one step against the sweep, so
// the sweep never removes bytes whose row is not yet written.
func (s *Server) keepUpload(ctx context.Context, ws int64, g *attach.Staged) (store.Attachment, error) {
	uploadMu.Lock()
	defer uploadMu.Unlock()
	if err := g.Commit(); err != nil {
		return store.Attachment{}, err
	}
	in := g.Info
	return s.Store.AddAttachment(ctx, store.Attachment{WorkspaceID: ws, SHA: g.SHA, Name: in.Name, Mime: in.Mime, Kind: in.Kind, Size: in.Size})
}

// sweepAttachments forgets files nobody sent, and removes the bytes nothing refers to.
func (s *Server) sweepAttachments(ctx context.Context) {
	files, err := s.attachments()
	if err != nil {
		return
	}
	uploadMu.Lock()
	defer uploadMu.Unlock()
	free, err := s.Store.SweepAttachments(ctx, time.Now().Add(-attachKeep))
	if err != nil {
		s.Log.Warn("sweep attachments", "err", err)
		return
	}
	for _, sha := range free {
		if err := files.Remove(sha); err != nil {
			s.Log.Warn("remove an attachment", "err", err)
		}
	}
}

// sweepAttachmentsEvery runs the sweep now and then until quit closes, so an idle daemon
// still forgets what the retention rules say.
func (s *Server) sweepAttachmentsEvery(quit <-chan struct{}, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		s.sweepAttachments(context.Background())
		select {
		case <-quit:
			return
		case <-t.C:
		}
	}
}

// deskAttachGet serves a stored file. Only an image is shown inline: anything else is
// a download, and none is ever interpreted by the browser.
func (s *Server) deskAttachGet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("bad attachment id"))
		return
	}
	a, err := s.Store.Attachment(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, fmt.Errorf("no attachment %d", id))
		return
	} else if err != nil {
		s.fail(w, err)
		return
	}
	files, err := s.attachments()
	if err != nil {
		s.fail(w, err)
		return
	}
	f, err := files.Open(a.SHA)
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, fmt.Errorf("attachment %d is gone", id))
		return
	} else if err != nil {
		s.fail(w, err)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "private, max-age=3600")
	disp := "attachment"
	h.Set("Content-Type", "application/octet-stream")
	if a.Kind == attach.KindImage {
		disp = "inline"
		h.Set("Content-Type", a.Mime)
	}
	h.Set("Content-Disposition", disp+"; filename*=UTF-8''"+urlEscape(a.Name))
	http.ServeContent(w, r, "", a.Created, f)
}

func urlEscape(s string) string {
	const hex = "0123456789ABCDEF"
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_' {
			b = append(b, c)
			continue
		}
		b = append(b, '%', hex[c>>4], hex[c&15])
	}
	return string(b)
}

// checkTurnAttachments refuses a message whose files are too many or too big, or are
// not this workspace's, before anything is sent.
func (s *Server) checkTurnAttachments(w http.ResponseWriter, r *http.Request, ws int64, ids []int64) bool {
	if len(ids) == 0 {
		return true
	}
	if len(ids) > attach.MaxTurnFiles {
		writeError(w, http.StatusBadRequest, fmt.Errorf("a message takes at most %d files", attach.MaxTurnFiles))
		return false
	}
	var total int64
	seen := map[int64]bool{}
	for _, id := range ids {
		a, err := s.Store.Attachment(r.Context(), id)
		switch {
		case seen[id]:
			writeError(w, http.StatusBadRequest, fmt.Errorf("attachment %d is listed twice", id))
			return false
		case errors.Is(err, store.ErrNotFound) || (err == nil && (a.WorkspaceID != ws || a.Turn != 0)):
			writeError(w, http.StatusConflict, fmt.Errorf("%w: attachment %d is unknown or already sent", store.ErrConflict, id))
			return false
		case err != nil:
			s.fail(w, err)
			return false
		}
		seen[id] = true
		total += a.Size
	}
	if total > attach.MaxTurnBytes {
		writeError(w, http.StatusBadRequest, fmt.Errorf("a message takes at most %d MiB of files", attach.MaxTurnBytes>>20))
		return false
	}
	return true
}
