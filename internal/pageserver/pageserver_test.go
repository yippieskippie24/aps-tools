package pageserver

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestServesPagesOnlyAtTheirAddress(t *testing.T) {
	s, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	url := s.Add([]byte("<p>Hello Alex</p>"))
	if !strings.HasPrefix(url, "http://127.0.0.1:") || len(url) < len("http://127.0.0.1:1/p/")+32 {
		t.Fatalf("url = %q: want 127.0.0.1 and a long random address", url)
	}
	if s.WaitFetched(url, 10*time.Millisecond) {
		t.Error("reported fetched before anyone loaded it")
	}

	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "<p>Hello Alex</p>" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("GET page: %d %q %v", resp.StatusCode, body, resp.Header)
	}
	if !s.WaitFetched(url, time.Second) {
		t.Error("not reported as fetched")
	}

	base := url[:strings.Index(url, "/p/")]
	for _, bad := range []string{base + "/", base + "/p/", base + "/p/0123456789abcdef0123456789abcdef"} {
		resp, err := http.Get(bad)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s: %d, want 404", bad, resp.StatusCode)
		}
	}
	resp, err = http.Post(url, "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("POST: %d, want 404", resp.StatusCode)
	}
}
