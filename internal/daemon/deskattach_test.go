package daemon

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 40)...)

func (r *deskRig) upload(t *testing.T, name string, data []byte) (int, store.Attachment, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write(data)
	mw.Close()
	req, _ := http.NewRequest("POST", r.ts.URL+api.PathDeskAttachments, &body)
	req.Header.Set("Authorization", "Bearer "+r.s.Token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var a store.Attachment
	json.Unmarshal(raw, &a)
	return resp.StatusCode, a, string(raw)
}

func TestDeskTurnWithAttachments(t *testing.T) {
	r := newDeskRig(t, "never", 0)
	r.s.AttachDir = filepath.Join(t.TempDir(), "att")
	if code, _, body := r.upload(t, "run.exe", []byte("hello")); code != http.StatusBadRequest || !strings.Contains(body, "program") {
		t.Errorf("exe: %d %s", code, body)
	}
	code, img, _ := r.upload(t, "shot.png", pngBytes)
	if code != http.StatusCreated || img.Kind != "image" || img.Name != "shot.png" {
		t.Fatalf("image: %d %+v", code, img)
	}
	code, logf, _ := r.upload(t, "ci.log", []byte("step 1 ok\nkey AKIAIOSFODNN7EXAMPLE\n"))
	if code != http.StatusCreated {
		t.Fatalf("log: %d", code)
	}
	// An unknown file, or too many, is refused before anything is sent.
	if c := call(t, r.ts, r.s.Token, "POST", api.PathDeskTurn, api.DeskTurn{Text: "hi", Attachments: []int64{999}}, nil); c != http.StatusConflict {
		t.Errorf("unknown file: %d", c)
	}
	if c := call(t, r.ts, r.s.Token, "POST", api.PathDeskTurn, api.DeskTurn{Text: "hi", Attachments: []int64{1, 2, 3, 4, 5, 6}}, nil); c != http.StatusBadRequest {
		t.Errorf("six files: %d", c)
	}
	ch, _ := r.stream(t, api.PathDeskStream+"?after=0", nil)
	var acc api.DeskTurnAccepted
	if c := call(t, r.ts, r.s.Token, "POST", api.PathDeskTurn, api.DeskTurn{Attachments: []int64{img.ID, logf.ID}}, &acc); c != http.StatusAccepted {
		t.Fatalf("turn: %d", c)
	}
	got := until(t, ch, store.DeskTurnEnd)
	u := got[0].data
	if u.Kind != store.DeskUser || u.Text != "See the attached files." || len(u.Attachments) != 2 || u.Attachments[0].Name != "shot.png" {
		t.Errorf("user event = %+v", u)
	}
	// The agent read where to open each file, and the redacted text of the small log.
	b, _ := os.ReadFile(r.log + ".msg")
	msg := string(b)
	files, _ := r.s.attachments()
	if !strings.Contains(msg, "shot.png") || !strings.Contains(msg, "ci.log") || !strings.Contains(msg, "step 1 ok") ||
		strings.Contains(msg, "AKIAIOSFODNN7EXAMPLE") || !strings.Contains(msg, files.Dir) {
		t.Errorf("agent message:\n%s", msg)
	}
	// A file goes with one message only.
	if c := call(t, r.ts, r.s.Token, "POST", api.PathDeskTurn, api.DeskTurn{Text: "again", Attachments: []int64{img.ID}}, nil); c != http.StatusConflict {
		t.Errorf("file sent twice: %d", c)
	}
	var hist api.DeskHistory
	call(t, r.ts, r.s.Token, "GET", api.PathDeskHistory+"?limit=20", nil, &hist)
	if len(hist.Events) == 0 || len(hist.Events[0].Attachments) != 2 {
		t.Errorf("history lost the files: %+v", hist.Events)
	}
	// Serving: an image inline, anything else a download, never sniffed.
	for _, c := range []struct {
		id        int64
		typ, disp string
	}{{img.ID, "image/png", "inline"}, {logf.ID, "application/octet-stream", "attachment"}} {
		req, _ := http.NewRequest("GET", r.ts.URL+api.PathDeskAttachments+"/"+itoa(c.id), nil)
		req.Header.Set("Authorization", "Bearer "+r.s.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != c.typ || !strings.HasPrefix(resp.Header.Get("Content-Disposition"), c.disp) || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("serve %d: %d %v", c.id, resp.StatusCode, resp.Header)
		}
	}
	if c := call(t, r.ts, r.s.Token, "GET", api.PathDeskAttachments+"/999", nil, nil); c != http.StatusNotFound {
		t.Errorf("missing: %d", c)
	}
}
