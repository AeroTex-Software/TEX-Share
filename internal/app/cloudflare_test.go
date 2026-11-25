package app

import "testing"

func TestJSONArrayStart(t *testing.T) {
	if got := jsonArrayStart(`[{"id":"a"}]`); got != 0 {
		t.Errorf("plain array: got %d, want 0", got)
	}
	if got := jsonArrayStart("some log line\n[]"); got != 14 {
		t.Errorf("after log line: got %d, want 14", got)
	}
	if got := jsonArrayStart("no json here"); got != -1 {
		t.Errorf("missing array: got %d, want -1", got)
	}
	// A bracket inside a log line must not be mistaken for the array start.
	if got := jsonArrayStart("INF value[0]\n[]"); got != 13 {
		t.Errorf("log bracket: got %d, want 13", got)
	}
}
