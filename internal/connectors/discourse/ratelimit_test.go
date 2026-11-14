package discourse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// When the forum says "too many requests", the client waits as long as it
// asks and tries again, rather than failing part-way through bulk work.
func TestWaitsOutTheRateLimit(t *testing.T) {
	var waited []time.Duration
	saved := pause
	t.Cleanup(func() { pause = saved })
	pause = func(_ context.Context, d time.Duration) error { waited = append(waited, d); return nil }

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"extras":{"wait_seconds":300}}`))
		default:
			w.Write([]byte(`{"user":{"username":"robin"}}`))
		}
	}))
	defer srv.Close()

	c := NewUserKeyClient(srv.URL, "", "k", "id")
	var out struct {
		User struct{ Username string } `json:"user"`
	}
	if err := c.get(context.Background(), "/u/robin.json", nil, &out); err != nil {
		t.Fatal(err)
	}
	if out.User.Username != "robin" || calls != 3 {
		t.Errorf("got %q after %d calls", out.User.Username, calls)
	}
	// 7 seconds as asked; 300 is capped at a minute.
	if len(waited) != 2 || waited[0] != 7*time.Second || waited[1] != time.Minute {
		t.Errorf("waited %v", waited)
	}
}

// It gives up after a few waits, with the usual plain-English error.
func TestGivesUpOnAPersistentRateLimit(t *testing.T) {
	saved := pause
	t.Cleanup(func() { pause = saved })
	pause = func(context.Context, time.Duration) error { return nil }
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	err := NewUserKeyClient(srv.URL, "", "k", "id").get(context.Background(), "/x.json", nil, &struct{}{})
	if err == nil || calls != maxWaits+1 {
		t.Errorf("err %v after %d calls", err, calls)
	}
}
