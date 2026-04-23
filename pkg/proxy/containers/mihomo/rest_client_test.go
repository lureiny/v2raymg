package mihomo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------- GetVersion (stage 2, kept) ----------

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
	// Match the full hardcoded phrase from do()'s dedicated 401 branch so a
	// regression that falls through to the generic parseErrorBody path (where
	// the server's own "unauthorized" substring would still satisfy a loose
	// check) is caught by this test.
	if !strings.Contains(err.Error(), "unauthorized (check secret)") {
		t.Errorf("expected 'unauthorized (check secret)' in error, got: %v", err)
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

// ---------- GetConfigs ----------

func TestRESTClient_GetConfigs_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"port":0,"mode":"global"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewRESTClient(server.URL, "")
	raw, err := c.GetConfigs(context.Background())
	if err != nil {
		t.Fatalf("GetConfigs: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["mode"] != "global" {
		t.Errorf("mode = %v, want global", got["mode"])
	}
}

func TestRESTClient_GetConfigs_ErrorWithMessage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"something broke"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewRESTClient(server.URL, "")
	_, err := c.GetConfigs(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "something broke") {
		t.Errorf("error should include mihomo message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should include status code, got: %v", err)
	}
}

// ---------- PutConfigs ----------

func TestRESTClient_PutConfigs_Success_ForceFalse(t *testing.T) {
	var (
		gotMethod      string
		gotForceQuery  string
		gotContentType string
		gotAuth        string
		gotBodyPayload string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotForceQuery = r.URL.Query().Get("force")
		gotContentType = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")

		var body struct {
			Payload string `json:"payload"`
			Path    string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		gotBodyPayload = body.Payload
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewRESTClient(server.URL, "tok")
	yaml := []byte("mixed-port: 0\nlisteners: []\n")
	if err := c.PutConfigs(context.Background(), yaml, false); err != nil {
		t.Fatalf("PutConfigs: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", gotMethod)
	}
	if gotForceQuery != "false" {
		t.Errorf("force query = %q, want false", gotForceQuery)
	}
	if gotContentType != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotContentType)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("authorization = %q, want Bearer tok", gotAuth)
	}
	if gotBodyPayload != string(yaml) {
		t.Errorf("body payload = %q, want %q", gotBodyPayload, string(yaml))
	}
}

func TestRESTClient_PutConfigs_Success_ForceTrue(t *testing.T) {
	var gotForceQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		gotForceQuery = r.URL.Query().Get("force")
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewRESTClient(server.URL, "")
	if err := c.PutConfigs(context.Background(), []byte(""), true); err != nil {
		t.Fatalf("PutConfigs: %v", err)
	}
	if gotForceQuery != "true" {
		t.Errorf("force query = %q, want true", gotForceQuery)
	}
}

func TestRESTClient_PutConfigs_EmptySecretOmitsAuthHeader(t *testing.T) {
	var seenAuth bool
	mux := http.NewServeMux()
	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization") != ""
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewRESTClient(server.URL, "")
	if err := c.PutConfigs(context.Background(), []byte("x: 1"), false); err != nil {
		t.Fatalf("PutConfigs: %v", err)
	}
	if seenAuth {
		t.Error("Authorization header should not be set when secret is empty")
	}
}

func TestRESTClient_PutConfigs_ErrorWithMessage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"yaml: line 3: bad indent"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewRESTClient(server.URL, "")
	err := c.PutConfigs(context.Background(), []byte("bad"), false)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "yaml: line 3: bad indent") {
		t.Errorf("error should include mihomo message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should include 400, got: %v", err)
	}
}

func TestRESTClient_PutConfigs_NonJSONErrorBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("raw error text"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewRESTClient(server.URL, "")
	err := c.PutConfigs(context.Background(), []byte("x"), false)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "raw error text") {
		t.Errorf("error should fall back to raw body, got: %v", err)
	}
}

func TestRESTClient_PutConfigs_Unauthorized(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Unauthorized"}`, http.StatusUnauthorized)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewRESTClient(server.URL, "wrong")
	err := c.PutConfigs(context.Background(), []byte(""), false)
	if err == nil {
		t.Fatal("expected error")
	}
	// See TestRESTClient_GetVersion_Unauthorized: match the full phrase so the
	// dedicated 401 branch in do() is actually exercised, not the fallback.
	if !strings.Contains(err.Error(), "unauthorized (check secret)") {
		t.Errorf("expected 'unauthorized (check secret)' in error, got: %v", err)
	}
}

func TestRESTClient_PutConfigs_ContextCanceled(t *testing.T) {
	// Guard with time.After: for PUT with a body, r.Context().Done() sometimes
	// doesn't fire promptly on client-side cancellation (observed in practice
	// for this test but not GetVersion's equivalent). The 500ms fallback
	// ensures the handler always returns so httptest.Server.Close() doesn't
	// hang — the assertion below still verifies that the client returned an
	// error from the 50ms context deadline.
	mux := http.NewServeMux()
	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewRESTClient(server.URL, "")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := c.PutConfigs(ctx, []byte(""), false)
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
}

func TestRESTClient_PutConfigs_EmptyErrorBodyKeepsStatusCode(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewRESTClient(server.URL, "")
	err := c.PutConfigs(context.Background(), []byte(""), false)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should include status code even with empty body, got: %v", err)
	}
}

// ---------- PatchConfigs ----------

func TestRESTClient_PatchConfigs_Success(t *testing.T) {
	var (
		gotBody        map[string]any
		gotContentType string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		gotContentType = r.Header.Get("Content-Type")
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &gotBody)
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewRESTClient(server.URL, "")
	err := c.PatchConfigs(context.Background(), map[string]any{
		"log-level": "debug",
		"mode":      "global",
	})
	if err != nil {
		t.Fatalf("PatchConfigs: %v", err)
	}
	if gotContentType != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotContentType)
	}
	if gotBody["log-level"] != "debug" {
		t.Errorf("body log-level = %v, want debug", gotBody["log-level"])
	}
	if gotBody["mode"] != "global" {
		t.Errorf("body mode = %v, want global", gotBody["mode"])
	}
}

func TestRESTClient_PatchConfigs_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"bad field"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewRESTClient(server.URL, "")
	err := c.PatchConfigs(context.Background(), map[string]any{"bogus": 1})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bad field") {
		t.Errorf("error should include mihomo message, got: %v", err)
	}
}

// ---------- PostConfigsGeo ----------

func TestRESTClient_PostConfigsGeo_Success(t *testing.T) {
	var (
		gotMethod      string
		gotLen         int64
		gotContentType string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/configs/geo", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotLen = r.ContentLength
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewRESTClient(server.URL, "")
	if err := c.PostConfigsGeo(context.Background()); err != nil {
		t.Fatalf("PostConfigsGeo: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotLen > 0 {
		t.Errorf("expected empty body (nil), got content-length=%d", gotLen)
	}
	// Lock in the "nil body → no Content-Type header" invariant from do():
	// setting Content-Type unconditionally would masquerade an empty POST as
	// a JSON payload and confuse mihomo's router if it grows body sniffing.
	if gotContentType != "" {
		t.Errorf("content-type = %q, want empty (nil body)", gotContentType)
	}
}

func TestRESTClient_PostConfigsGeo_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/configs/geo", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"geo update failed"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewRESTClient(server.URL, "")
	err := c.PostConfigsGeo(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "geo update failed") {
		t.Errorf("error should include mihomo message, got: %v", err)
	}
}
