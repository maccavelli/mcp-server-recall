package harvest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScrapeWebDocument(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, world!"))
	}))
	defer ts.Close()

	res, err := ScrapeWebDocument(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(res.Symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(res.Symbols))
	}
	if !strings.Contains(res.Symbols[0].Doc, "Hello, world!") {
		t.Errorf("expected doc to contain hello world, got: %v", res.Symbols[0].Doc)
	}

	// test error on 404
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts2.Close()

	_, err = ScrapeWebDocument(context.Background(), ts2.URL)
	if err == nil {
		t.Fatal("expected error on 404")
	}

	// test error on bad url
	_, err = ScrapeWebDocument(context.Background(), "http://127.0.0.1:0/bad")
	if err == nil {
		t.Fatal("expected error on bad URL/connection refused")
	}
}
