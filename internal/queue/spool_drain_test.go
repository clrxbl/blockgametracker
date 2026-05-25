package queue

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDrainAfterSuccessfulPush(t *testing.T) {
	var calls int32
	var fail atomic.Bool
	fail.Store(true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if fail.Load() {
			http.Error(w, `{"success":false}`, 502)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	client := &Client{
		BaseURL:   srv.URL,
		AccountID: "acct",
		QueueID:   "queue",
		Token:     "t",
		HTTP:      srv.Client(),
	}
	spool, err := NewSpool(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Simulate three failed pushes that landed in the spool.
	for i := 0; i < 3; i++ {
		if _, err := spool.Write([]byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}

	files, _ := spool.List()
	if len(files) != 3 {
		t.Fatalf("want 3 spooled, got %d", len(files))
	}

	// Server now accepts. Drain with a producer-shaped helper.
	fail.Store(false)
	atomic.StoreInt32(&calls, 0)
	p := &Producer{Client: client, Spool: spool, DrainPerTick: 10}
	p.drain(ctx)

	files, _ = spool.List()
	if len(files) != 0 {
		t.Fatalf("expected spool drained, %d remain", len(files))
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 pushes during drain, got %d", got)
	}
}

func TestDrainStopsOnFailureToPreserveOrder(t *testing.T) {
	var fail atomic.Bool
	fail.Store(false)
	var calls int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		// Fail the second push to verify the drain stops there.
		if n == 2 {
			http.Error(w, `{"success":false}`, 502)
			return
		}
		if fail.Load() {
			http.Error(w, `{"success":false}`, 502)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, AccountID: "a", QueueID: "q", Token: "t", HTTP: srv.Client()}
	spool, err := NewSpool(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		if _, err := spool.Write([]byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}

	p := &Producer{Client: client, Spool: spool, DrainPerTick: 10}
	p.drain(context.Background())

	remaining, _ := spool.List()
	if len(remaining) != 2 {
		t.Fatalf("expected 2 batches remaining after stop, got %d", len(remaining))
	}
}
