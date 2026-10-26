package httpx

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// UploadOptions describes what a multipart endpoint accepts. It mimics
// multer's upload.array(FieldName, MaxFiles) with disk storage.
type UploadOptions struct {
	Dir           string // where files are stored (under random names)
	FieldName     string // the only accepted file field, e.g. "files"
	MaxFiles      int
	MaxFileMB     int
	MaxFieldBytes int // text field limit; 0 = 1 MiB (multer's default)
}

// UploadedFile is one stored upload.
type UploadedFile struct {
	Path string // random name on disk (no extension)
	Name string // the client's original file name
}

// Upload is a parsed multipart request.
type Upload struct {
	Files  []UploadedFile
	fields map[string][]string
}

// Field returns every value sent for a text field, joined with "," (the way
// an Express array of repeated fields stringifies).
func (u *Upload) Field(name string) string { return strings.Join(u.fields[name], ",") }

// Paths lists where the files are stored (for storage.UploadStore.Discard).
func (u *Upload) Paths() []string {
	ps := make([]string, len(u.Files))
	for i, f := range u.Files {
		ps[i] = f.Path
	}
	return ps
}

// Cleanup deletes the stored files. Call it when the request is done: the
// inputs are only needed while the request runs. It ignores the keep
// registry; a handler that registered files must use UploadStore.Discard.
func (u *Upload) Cleanup() {
	for _, f := range u.Files {
		_ = os.Remove(f.Path)
	}
}

// ParseUpload reads a multipart/form-data request. Behaviour is multer's:
//   - a non-multipart request yields an empty Upload, not an error;
//   - more than MaxFiles files, or a file under another field name, is
//     "Unexpected field" (a plain error -> 500);
//   - a file over MaxFileMB is a 400 with a clear message.
//
// On any error the files already stored are removed.
func ParseUpload(r *http.Request, opt UploadOptions) (*Upload, error) {
	up := &Upload{fields: map[string][]string{}}
	maxField := opt.MaxFieldBytes
	if maxField == 0 {
		maxField = 1 << 20
	}
	mt, params, perr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if perr != nil || !strings.HasPrefix(mt, "multipart/") {
		return up, nil
	}
	mr := multipart.NewReader(r.Body, params["boundary"])
	fail := func(err error) (*Upload, error) {
		up.Cleanup()
		return nil, err
	}
	for {
		part, perr := mr.NextPart()
		if perr == io.EOF {
			return up, nil
		}
		if perr != nil {
			return fail(Status(http.StatusBadRequest, perr))
		}
		name, fn := part.FormName(), part.FileName()
		cd := part.Header.Get("Content-Disposition")
		if fn == "" && cd != "" && !hasFilenameParam(cd) {
			b, err := io.ReadAll(io.LimitReader(part, int64(maxField)+1))
			if err != nil {
				return fail(Status(http.StatusBadRequest, err))
			}
			if len(b) > maxField {
				return fail(errors.New("Field value too long"))
			}
			up.fields[name] = append(up.fields[name], string(b))
			continue
		}
		if fn == "" { // empty file input
			if _, err := io.Copy(io.Discard, part); err != nil {
				return fail(Status(http.StatusBadRequest, err))
			}
			continue
		}
		if name != opt.FieldName || len(up.Files) >= opt.MaxFiles {
			return fail(errors.New("Unexpected field"))
		}
		if err := up.store(part, fn, opt); err != nil {
			return fail(err)
		}
	}
}

// store writes one file part to disk, enforcing the size limit.
func (u *Upload) store(part io.Reader, clientName string, opt UploadOptions) error {
	// CreateTemp reserves a unique file atomically and uses private permissions.
	out, err := os.CreateTemp(opt.Dir, "upload-*")
	if err != nil {
		return err
	}
	limit := int64(opt.MaxFileMB) << 20
	n, err := io.Copy(out, io.LimitReader(part, limit+1))
	err = errors.Join(err, out.Close())
	// Recorded before the checks so Cleanup also removes a rejected file.
	u.Files = append(u.Files, UploadedFile{Path: out.Name(), Name: filepath.Base(strings.ReplaceAll(clientName, `\`, "/"))})
	if err != nil {
		return err
	}
	if n > limit {
		return Status(http.StatusBadRequest, fmt.Errorf("File too large -- each uploaded file must be %d MB or smaller.", opt.MaxFileMB))
	}
	return nil
}

func hasFilenameParam(cd string) bool {
	_, params, err := mime.ParseMediaType(cd)
	if err != nil {
		return strings.Contains(strings.ToLower(cd), "filename")
	}
	_, ok := params["filename"]
	return ok
}
