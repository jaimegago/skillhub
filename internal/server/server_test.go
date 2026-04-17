package server_test

import (
	"bytes"
	"sort"
	"strings"
	"testing"

	"github.com/jaime-gago/skillhub/internal/server"
	"github.com/jaime-gago/skillhub/internal/tools"
)

func TestToolListMatchesRegistry(t *testing.T) {
	srv := server.New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "test")

	got := srv.ToolNames()

	want := make([]string, len(tools.Registry))
	for i, tool := range tools.Registry {
		want[i] = tool.Name
	}

	sort.Strings(got)
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("tool count: got %d, want %d\ngot:  %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tool[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}
