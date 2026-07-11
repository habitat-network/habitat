package log

import "testing"

func TestLog(t *testing.T) {
	New(WithLevel("debug"), WithStdout(true))
}
