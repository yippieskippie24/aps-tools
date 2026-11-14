package activity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAddAndRecent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", FileName)
	l := Open(path)
	l.now = func() time.Time { return time.Date(2026, 11, 13, 15, 4, 5, 0, time.UTC) }

	if lines, err := l.Recent(5); err != nil || len(lines) != 0 {
		t.Fatalf("no log yet: %v %v", lines, err)
	}
	for i, e := range []Entry{
		{Member: "alex@example.com", System: "Onboarding", OK: true, What: "Started"},
		{Member: "alex@example.com", System: "Mailing list", OK: false, What: "Cloudflare said\tno\nthanks"},
		{System: "Onboarding", OK: true, What: "Finished"},
	} {
		if err := l.Add(e); err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
	}
	lines, err := l.Recent(2)
	if err != nil || len(lines) != 2 {
		t.Fatalf("Recent(2) = %v, %v", lines, err)
	}
	if want := "2026-11-13 15:04:05\talex@example.com\tMailing list\tFAILED\tCloudflare said no thanks"; lines[0] != want {
		t.Errorf("line =\n%q\nwant\n%q", lines[0], want)
	}
	if !strings.HasSuffix(lines[1], "\t-\tOnboarding\tok\tFinished") {
		t.Errorf("entry with no member: %q", lines[1])
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the log names members, so only you should read it: %v %v", info.Mode(), err)
	}
}

func TestEntriesReadBack(t *testing.T) {
	l := Open(filepath.Join(t.TempDir(), FileName))
	when := time.Date(2026, 11, 13, 15, 4, 5, 0, time.Local)
	l.now = func() time.Time { return when }
	_ = l.Add(Entry{Member: "alex@example.com", System: "Mailing list", OK: false, What: "It failed"})
	_ = l.Add(Entry{System: "Onboarding", OK: true, What: "Finished"})
	entries, err := l.Entries(10)
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries = %+v, %v", entries, err)
	}
	if e := entries[0]; e.Member != "alex@example.com" || e.OK || e.What != "It failed" || !e.Time.Equal(when) {
		t.Errorf("first = %+v", e)
	}
	if e := entries[1]; e.Member != "" || !e.OK {
		t.Errorf("second = %+v", e)
	}
	if _, ok := ParseLine("not a log line"); ok {
		t.Error("a bad line was accepted")
	}
}
