// Package pageserver shows the tool's pages (like a member's welcome email,
// ready to copy) in the webmaster's browser, from a tiny web server inside
// the tool, instead of saving them as files.
//
// Why a server: nothing with a member's details is left lying on disk, and
// the page is a normal web page, so the browser's copy buttons work
// reliably. It's kept as safe as a file:
//   - it listens only on 127.0.0.1 (this Mac), on a random free port;
//   - each page has a long random address, so it can't be guessed;
//   - pages are held in memory and are gone when the tool closes;
//   - it only answers GET requests for those addresses.
//
// It uses Go's built-in web server, so it adds nothing to install.
package pageserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Server serves pages from memory on 127.0.0.1.
type Server struct {
	base    string // e.g. "http://127.0.0.1:52311"
	srv     *http.Server
	mu      sync.Mutex
	pages   map[string][]byte
	fetched map[string]chan struct{} // closed when a page is first loaded
}

// Start starts a server on a random free port on 127.0.0.1.
func Start() (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("couldn't start the tool's page server: " + err.Error())
	}
	s := &Server{
		base:    "http://" + ln.Addr().String(),
		pages:   map[string][]byte{},
		fetched: map[string]chan struct{}{},
	}
	s.srv = &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second}
	go s.srv.Serve(ln) //nolint:errcheck // ends with ErrServerClosed on Close
	return s, nil
}

var (
	sharedOnce sync.Once
	shared     *Server
	sharedErr  error
)

// Shared returns the tool's one page server, starting it the first time.
// It runs until the tool exits.
func Shared() (*Server, error) {
	sharedOnce.Do(func() { shared, sharedErr = Start() })
	return shared, sharedErr
}

// Add stores a page and returns its address.
func (s *Server) Add(page []byte) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	s.mu.Lock()
	s.pages[id] = page
	s.fetched[id] = make(chan struct{})
	s.mu.Unlock()
	return s.base + "/p/" + id
}

// WaitFetched waits until the page at url has been loaded at least once,
// or until timeout. It reports whether it was loaded. Used by commands that
// exit right after opening a page, so the browser gets it first.
func (s *Server) WaitFetched(url string, timeout time.Duration) bool {
	id := strings.TrimPrefix(url, s.base+"/p/")
	s.mu.Lock()
	ch, ok := s.fetched[id]
	s.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case <-ch:
		return true
	case <-time.After(timeout):
		return false
	}
}

// ServeHTTP answers GET /p/<id> with the page; anything else is "not found".
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, ok := strings.CutPrefix(r.URL.Path, "/p/")
	if !ok || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	page, found := s.pages[id]
	ch := s.fetched[id]
	s.mu.Unlock()
	if !found {
		http.NotFound(w, r)
		return
	}
	select {
	case <-ch:
	default:
		close(ch)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store") // never keep member details
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = w.Write(page)
}

// Close stops the server.
func (s *Server) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.srv.Shutdown(ctx)
}
