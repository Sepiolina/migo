package httpx

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestParseUploadTruncatedFieldCleansFiles(t *testing.T) {
	body := "--boundary\r\nContent-Disposition: form-data; name=\"files\"; filename=\"bom.xlsx\"\r\n\r\nworkbook\r\n--boundary\r\nContent-Disposition: form-data; name=\"bam\"\r\n\r\n113280"
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")
	dir := t.TempDir()
	up, err := ParseUpload(r, UploadOptions{Dir: dir, FieldName: "files", MaxFiles: 1, MaxFileMB: 1})
	var he *Error
	if up != nil || !errors.As(err, &he) || he.Status != http.StatusBadRequest {
		t.Fatalf("expected 400 for truncated field, got upload=%v err=%v", up, err)
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("failed request left %d files", len(files))
	}
}

type failingUploadReader struct{}

func (failingUploadReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestStoreReadFailureCleansFile(t *testing.T) {
	up := &Upload{}
	dir := t.TempDir()
	err := up.store(failingUploadReader{}, "bom.xlsx", UploadOptions{Dir: dir, MaxFileMB: 1})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("read error lost: %v", err)
	}
	up.Cleanup()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatal("failed upload left files")
	}
}
