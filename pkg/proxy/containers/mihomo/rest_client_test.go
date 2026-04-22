package mihomo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRESTClient_GetVersion_Success(t *testing.T) {
	var gotAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"alpha-5a5e312","meta":true}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewRESTClient(server.URL, "s3cr3t")
	v, err := c.GetVersion(context.Background())
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if v != "alpha-5a5e312" {
		t.Errorf("version = %q, want %q", v, "alpha-5a5e312")
	}
	if gotAuth != "Bearer s3cr3t" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer s3cr3t")
	}
}

func TestRESTClient_GetVersion_EmptySecretOmitsHeader(t *testing.T) {
	var seenAuth bool
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization") != ""
		_, _ = w.Write([]byte(`{"version":"alpha","meta":true}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewRESTClient(server.URL, "")
	if _, err := c.GetVersion(context.Background()); err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if seenAuth {
		t.Error("Authorization header should not be set when secret is empty")
	}
}

func TestRESTClient_GetVersion_Unauthorized(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewRESTClient(server.URL, "wrong")
	_, err := c.GetVersion(context.Background())
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !strings.Contains(err.Error(), "unauthorized") {
		t.Errorf("expected 'unauthorized' in error, got: %v", err)
	}
}

func TestRESTClient_GetVersion_ServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad", http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewRESTClient(server.URL, "")
	_, err := c.GetVersion(context.Background())
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected '500' in error, got: %v", err)
	}
}

func TestRESTClient_GetVersion_ContextCanceled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		// hang until client disconnects
		<-r.Context().Done()
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewRESTClient(server.URL, "")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.GetVersion(ctx)
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
}
