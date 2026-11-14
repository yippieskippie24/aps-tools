// Package activity keeps the local activity log: one line for every change
// the tool makes in an outside system (and every attempt that failed), so
// the webmaster, or their successor, can always see what was done and when.
//
// The log is a plain text file next to the settings file
// (~/.config/aps-tools/activity.log). Open it in any text editor, or run
// `aps-tools log`. It stays on this Mac: it names members, so it never goes
// into the public repository, and only you can read it (file mode 0600).
//
// Previews and dry runs change nothing, so they write nothing here.
package activity

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yippieskippie24/aps-tools/internal/config"
)

// FileName is the log's name inside the settings folder.
const FileName = "activity.log"

// timeLayout is how each line's time is written: sortable and readable.
const timeLayout = "2006-01-02 15:04:05"

// Entry is one line of the log.
type Entry struct {
	// Time defaults to now when left zero.
	Time time.Time
	// Member is the member's email, or "" for entries about no one member.
	Member string
	// System is the outside system, e.g. "Mailing list (Cloudflare)", or
	// "Onboarding" for the start and end of a run.
	System string
	// OK is false when this records a failure.
	OK bool
	// What happened, in plain English, e.g. "Added to the news@ list".
	What string
}

// Line renders the entry as one tab-separated line (tabs keep the columns
// apart even when a field contains spaces).
func (e Entry) Line() string {
	mark := "ok"
	if !e.OK {
		mark = "FAILED"
	}
	clean := func(s string) string {
		s = strings.ReplaceAll(s, "\t", " ")
		return strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
	}
	member := clean(e.Member)
	if member == "" {
		member = "-"
	}
	return strings.Join([]string{e.Time.Format(timeLayout), member, clean(e.System), mark, clean(e.What)}, "\t")
}

// Log appends entries to one log file.
type Log struct {
	path string
	now  func() time.Time
}

// Open returns the log at path. The file is created on the first Add.
func Open(path string) *Log { return &Log{path: path, now: time.Now} }

// Default returns the log next to the settings file.
func Default() (*Log, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	return Open(filepath.Join(dir, FileName)), nil
}

// Path is where the log file is.
func (l *Log) Path() string { return l.path }

// Add appends one entry. An error here means the change happened but wasn't
// recorded, so callers show it to the webmaster rather than ignoring it.
func (l *Log) Add(e Entry) error {
	if e.Time.IsZero() {
		e.Time = l.now()
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return fmt.Errorf("couldn't write the activity log: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("couldn't write the activity log: %w", err)
	}
	if _, err := fmt.Fprintln(f, e.Line()); err != nil {
		f.Close()
		return fmt.Errorf("couldn't write the activity log: %w", err)
	}
	return f.Close()
}

// Recent returns the last n lines, oldest first. No log yet means none.
func (l *Log) Recent(n int) ([]string, error) {
	f, err := os.Open(l.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		lines = append(lines, sc.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	return lines, sc.Err()
}

// ParseLine reads one line of the log back into an Entry. ok is false for
// a line that isn't in the log's format (e.g. edited by hand).
func ParseLine(line string) (Entry, bool) {
	f := strings.Split(line, "\t")
	if len(f) < 5 {
		return Entry{}, false
	}
	t, err := time.ParseInLocation(timeLayout, f[0], time.Local)
	if err != nil {
		return Entry{}, false
	}
	member := f[1]
	if member == "-" {
		member = ""
	}
	return Entry{Time: t, Member: member, System: f[2], OK: f[3] == "ok", What: strings.Join(f[4:], " ")}, true
}

// Entries returns the last n entries, oldest first. Lines that can't be
// read are skipped.
func (l *Log) Entries(n int) ([]Entry, error) {
	lines, err := l.Recent(n)
	var out []Entry
	for _, line := range lines {
		if e, ok := ParseLine(line); ok {
			out = append(out, e)
		}
	}
	return out, err
}
