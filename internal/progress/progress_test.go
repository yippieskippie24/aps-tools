package progress

import (
	"context"
	"testing"
)

func TestReport(t *testing.T) {
	Report(context.Background(), "nobody is listening") // must not panic
	var got []string
	ctx := With(context.Background(), func(s string) { got = append(got, s) })
	Report(ctx, "Reading the sheet")
	Reportf(ctx, "Checking %s", "Jira")
	if len(got) != 2 || got[1] != "Checking Jira" {
		t.Errorf("got %v", got)
	}
}

func TestPrefix(t *testing.T) {
	var got []string
	ctx := With(context.Background(), func(s string) { got = append(got, s) })
	Report(Prefix(ctx, "2 of 5 · Alex"), "Checking Jira")
	Report(Prefix(context.Background(), "ignored"), "nobody listening")
	if len(got) != 1 || got[0] != "2 of 5 · Alex · Checking Jira" {
		t.Errorf("got %v", got)
	}
}
