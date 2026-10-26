package sqlserver

import "testing"

func TestCloseWithoutOpenIsNoop(t *testing.T) {
	if err := New(Config{}).Close(); err != nil {
		t.Error(err)
	}
}
