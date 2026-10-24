package sqlserver

import "testing"

func TestChunk(t *testing.T) {
	ch := Chunk([]string{"a", "b", "c", "d", "e"}, 2)
	if len(ch) != 3 || len(ch[2]) != 1 || ch[0][1] != "b" {
		t.Errorf("%v", ch)
	}
	if len(Chunk(nil, 2)) != 0 {
		t.Error("empty input -> no chunks")
	}
}

func TestCloseWithoutOpenIsNoop(t *testing.T) {
	if err := New(Config{}).Close(); err != nil {
		t.Error(err)
	}
}
