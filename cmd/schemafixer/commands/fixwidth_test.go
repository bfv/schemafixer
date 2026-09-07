package commands

import (
	"bytes"
	"strings"
	"testing"
)

func TestProcessWidthDF(t *testing.T) {
	input := "ADD FIELD \"Reason\" OF \"Employee\" AS character\n  FORMAT \"X(8)\"\n  MAX-WIDTH 6\n\n" +
		"ADD FIELD \"Payload\" OF \"Message\" AS raw\n  FORMAT \"x(20000)\"\n  MAX-WIDTH 1\n\n" +
		"ADD FIELD \"Count\" OF \"Message\" AS integer\n  FORMAT \"x(8)\"\n  MAX-WIDTH 6\n\n"
	want := "ADD FIELD \"Reason\" OF \"Employee\" AS character\n  FORMAT \"X(8)\"\n  MAX-WIDTH 16\n\n" +
		"ADD FIELD \"Payload\" OF \"Message\" AS raw\n  FORMAT \"x(20000)\"\n  MAX-WIDTH 31995\n\n" +
		"ADD FIELD \"Count\" OF \"Message\" AS integer\n  FORMAT \"x(8)\"\n  MAX-WIDTH 6\n\n"

	var output bytes.Buffer
	processWidthDF(strings.Split(strings.TrimSuffix(input, "\n"), "\n"), &output, "\n")
	if got := output.String(); got != want {
		t.Errorf("processWidthDF() output mismatch\ngot:  %q\nwant: %q", got, want)
	}
}
