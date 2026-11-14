// Package progress lets long-running work say which step it's on, so the
// app can show it ("Checking Jira…", "Reading the membership sheet…") and
// the webmaster never has to wonder whether anything is happening.
//
// The work doesn't need to know who's listening: the app puts a reporter in
// the context (With), and the work calls Report as it goes. With no
// reporter (e.g. the command line), Report does nothing.
package progress

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type key struct{}

// With returns a context whose Report calls go to report.
func With(ctx context.Context, report func(step string)) context.Context {
	return context.WithValue(ctx, key{}, report)
}

// Report says which step the work is on now, e.g. "Checking Jira".
func Report(ctx context.Context, step string) {
	if f, ok := ctx.Value(key{}).(func(string)); ok && f != nil {
		f(step)
	}
}

// Reportf is Report with formatting.
func Reportf(ctx context.Context, format string, args ...any) {
	Report(ctx, fmt.Sprintf(format, args...))
}

// Named is anything with a human name, like a connector.
type Named interface{ Name() string }

// Counter is for work done on several systems at once. It reports
// "<doing> Jira, the forum…" with the systems still running, and returns
// a function to call as each one finishes, which updates the report
// ("… (2 of 3 done)").
func Counter[T Named](ctx context.Context, doing string, items []T) func(T) {
	var mu sync.Mutex
	waiting := map[string]bool{}
	for _, it := range items {
		waiting[it.Name()] = true
	}
	report := func() {
		var names []string
		for _, it := range items {
			if waiting[it.Name()] {
				names = append(names, it.Name())
			}
		}
		if len(names) == 0 {
			return
		}
		msg := doing + " " + strings.Join(names, ", ")
		if done := len(items) - len(names); done > 0 {
			msg += fmt.Sprintf(" (%d of %d done)", done, len(items))
		}
		Report(ctx, msg)
	}
	report()
	return func(it T) {
		mu.Lock()
		defer mu.Unlock()
		delete(waiting, it.Name())
		report()
	}
}
