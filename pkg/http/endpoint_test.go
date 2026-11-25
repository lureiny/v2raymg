package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/http/auth"
)

func TestBindEndpointReq_JSONAndPointers(t *testing.T) {
	type ptrReq struct {
		UploadBps   *int64 `json:"upload_bps,omitempty"`
		DownloadBps *int64 `json:"download_bps,omitempty"`
		MaxClients  *int   `json:"max_clients,omitempty"`
		Target      string `json:"target,omitempty"`
	}

	t.Run("pointer nil vs explicit zero vs non-zero", func(t *testing.T) {
		var got ptrReq
		r := gin.New()
		r.PUT("/bw", func(c *gin.Context) {
			req, err := bindEndpointReq[ptrReq](c, http.MethodPut, false)
			if err != nil {
				writeEndpointError(c, err)
				return
			}
			got = req
			jsonOK(c)
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/bw", bytes.NewBufferString(`{"upload_bps":0,"max_clients":3,"target":"n1"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		if got.UploadBps == nil || *got.UploadBps != 0 {
			t.Fatalf("expected UploadBps=&0, got %v", got.UploadBps)
		}
		if got.DownloadBps != nil {
			t.Fatalf("expected DownloadBps=nil, got %v", *got.DownloadBps)
		}
		if got.MaxClients == nil || *got.MaxClients != 3 {
			t.Fatalf("expected MaxClients=&3, got %v", got.MaxClients)
		}
		if got.Target != "n1" {
			t.Fatalf("expected Target=n1, got %q", got.Target)
		}
	})

	t.Run("explicit null pointer stays nil", func(t *testing.T) {
		var got ptrReq
		r := gin.New()
		r.PUT("/bw", func(c *gin.Context) {
			req, err := bindEndpointReq[ptrReq](c, http.MethodPut, false)
			if err != nil {
				writeEndpointError(c, err)
				return
			}
			got = req
			jsonOK(c)
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/bw", bytes.NewBufferString(`{"upload_bps":null,"max_clients":0}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		if got.UploadBps != nil {
			t.Fatalf("expected UploadBps=nil for JSON null, got %v", *got.UploadBps)
		}
		if got.MaxClients == nil || *got.MaxClients != 0 {
			t.Fatalf("expected MaxClients=&0, got %v", got.MaxClients)
		}
	})

	t.Run("malformed JSON returns 400 regardless of OptionalBody", func(t *testing.T) {
		for _, opt := range []bool{false, true} {
			r := gin.New()
			r.POST("/bad", func(c *gin.Context) {
				_, err := bindEndpointReq[ptrReq](c, http.MethodPost, opt)
				if err != nil {
					writeEndpointError(c, err)
					return
				}
				jsonOK(c)
			})
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/bad", bytes.NewBufferString(`{"target":`))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("optionalBody=%v: expected 400 for malformed JSON, got %d body=%s", opt, w.Code, w.Body.String())
			}
		}
	})

	t.Run("empty body rejected when OptionalBody is false and accepted when true", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			method   string
			body     *bytes.Buffer
			optional bool
			wantCode int
		}{
			{"nil body required", http.MethodPost, nil, false, http.StatusBadRequest},
			{"empty body required", http.MethodPut, bytes.NewBufferString(""), false, http.StatusBadRequest},
			{"whitespace body required", http.MethodDelete, bytes.NewBufferString("   \n\t "), false, http.StatusBadRequest},
			{"nil body optional", http.MethodPost, nil, true, http.StatusOK},
			{"empty body optional", http.MethodPut, bytes.NewBufferString(""), true, http.StatusOK},
			{"whitespace body optional", http.MethodDelete, bytes.NewBufferString("  "), true, http.StatusOK},
		} {
			t.Run(tc.name, func(t *testing.T) {
				r := gin.New()
				r.Handle(tc.method, "/opt", func(c *gin.Context) {
					_, err := bindEndpointReq[ptrReq](c, tc.method, tc.optional)
					if err != nil {
						writeEndpointError(c, err)
						return
					}
					jsonOK(c)
				})
				w := httptest.NewRecorder()
				var req *http.Request
				if tc.body == nil {
					req = httptest.NewRequest(tc.method, "/opt", nil)
				} else {
					req = httptest.NewRequest(tc.method, "/opt", tc.body)
				}
				r.ServeHTTP(w, req)
				if w.Code != tc.wantCode {
					t.Fatalf("expected %d, got %d body=%s", tc.wantCode, w.Code, w.Body.String())
				}
			})
		}
	})
}

func TestBindEndpointReq_FormURLEncoded(t *testing.T) {
	type rotateReq struct {
		Target    string `json:"target,omitempty"    form:"target"`
		Username  string `json:"username,omitempty"  form:"username"`
		Container string `json:"container"           form:"container"`
		Port      uint32 `json:"port,omitempty"      form:"port"`
		Limit     *int   `json:"limit,omitempty"     form:"limit"`
	}

	t.Run("binds application/x-www-form-urlencoded with charset", func(t *testing.T) {
		var got rotateReq
		r := gin.New()
		r.POST("/rotate", func(c *gin.Context) {
			req, err := bindEndpointReq[rotateReq](c, http.MethodPost, false)
			if err != nil {
				writeEndpointError(c, err)
				return
			}
			got = req
			jsonOK(c)
		})

		form := url.Values{}
		form.Set("target", "node2")
		form.Set("username", "alice")
		form.Set("container", "xray")
		form.Set("port", "23456")
		form.Set("limit", "0")

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/rotate", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		if got.Target != "node2" || got.Username != "alice" || got.Container != "xray" || got.Port != 23456 {
			t.Fatalf("unexpected bound form req: %+v", got)
		}
		if got.Limit == nil || *got.Limit != 0 {
			t.Fatalf("expected Limit=&0, got %v", got.Limit)
		}
	})

	t.Run("invalid numeric form field returns 400", func(t *testing.T) {
		r := gin.New()
		r.POST("/rotate", func(c *gin.Context) {
			_, err := bindEndpointReq[rotateReq](c, http.MethodPost, false)
			if err != nil {
				writeEndpointError(c, err)
				return
			}
			jsonOK(c)
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/rotate", strings.NewReader("port=notanumber"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("nil form body allowed only when optional", func(t *testing.T) {
		for _, tc := range []struct {
			optional bool
			wantCode int
		}{
			{true, http.StatusOK},
			{false, http.StatusBadRequest},
		} {
			r := gin.New()
			r.POST("/rotate", func(c *gin.Context) {
				_, err := bindEndpointReq[rotateReq](c, http.MethodPost, tc.optional)
				if err != nil {
					writeEndpointError(c, err)
					return
				}
				jsonOK(c)
			})
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/rotate", nil)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.ServeHTTP(w, req)
			if w.Code != tc.wantCode {
				t.Fatalf("optional=%v: expected %d, got %d body=%s", tc.optional, tc.wantCode, w.Code, w.Body.String())
			}
		}
	})
}

func TestBindEndpointReq_QueryParamsAndDefaults(t *testing.T) {
	type embeddedQuery struct {
		Target string `query:"target" default:"all"`
	}
	type queryReq struct {
		embeddedQuery
		Container string   `query:"container" default:"xray"`
		UseSNI    bool     `query:"use_sni" default:"true"`
		Fake      bool     `query:"fake" default:"false"`
		Count     int      `query:"count" default:"10"`
		Count64   int64    `query:"count64"`
		Port      uint32   `query:"port"`
		OptInt    *int     `query:"opt_int"`
		OptInt64  *int64   `query:"opt_int64"`
		OptUint   *uint16  `query:"opt_uint"`
		Tags      []string `query:"tag"`
		DefTags   []string `query:"def_tag" default:"a,b"`
	}

	t.Run("defaults applied when absent or empty", func(t *testing.T) {
		var got queryReq
		r := gin.New()
		r.GET("/q", func(c *gin.Context) {
			req, err := bindEndpointReq[queryReq](c, http.MethodGet, false)
			if err != nil {
				writeEndpointError(c, err)
				return
			}
			got = req
			jsonOK(c)
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/q?target=", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		if got.Target != "all" || got.Container != "xray" || !got.UseSNI || got.Fake || got.Count != 10 {
			t.Fatalf("unexpected defaults: %+v", got)
		}
		if got.OptInt != nil || got.OptInt64 != nil || got.OptUint != nil {
			t.Fatalf("expected nil pointers when omitted, got %v %v %v", got.OptInt, got.OptInt64, got.OptUint)
		}
		if len(got.Tags) != 0 {
			t.Fatalf("expected empty Tags, got %v", got.Tags)
		}
		if len(got.DefTags) != 2 || got.DefTags[0] != "a" || got.DefTags[1] != "b" {
			t.Fatalf("expected DefTags=[a b], got %v", got.DefTags)
		}
	})

	t.Run("explicit values including zero and false override defaults", func(t *testing.T) {
		var got queryReq
		r := gin.New()
		r.GET("/q", func(c *gin.Context) {
			req, err := bindEndpointReq[queryReq](c, http.MethodGet, false)
			if err != nil {
				writeEndpointError(c, err)
				return
			}
			got = req
			jsonOK(c)
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/q?target=node1&container=mihomo&use_sni=false&fake=1&count=0&count64=-99&port=443&opt_int=0&opt_int64=-5&opt_uint=0&tag=x&tag=y&def_tag=only", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		if got.Target != "node1" || got.Container != "mihomo" || got.UseSNI || !got.Fake || got.Count != 0 || got.Count64 != -99 || got.Port != 443 {
			t.Fatalf("unexpected explicit values: %+v", got)
		}
		if got.OptInt == nil || *got.OptInt != 0 {
			t.Fatalf("expected OptInt=&0, got %v", got.OptInt)
		}
		if got.OptInt64 == nil || *got.OptInt64 != -5 {
			t.Fatalf("expected OptInt64=&-5, got %v", got.OptInt64)
		}
		if got.OptUint == nil || *got.OptUint != 0 {
			t.Fatalf("expected OptUint=&0, got %v", got.OptUint)
		}
		if len(got.Tags) != 2 || got.Tags[0] != "x" || got.Tags[1] != "y" {
			t.Fatalf("expected Tags=[x y], got %v", got.Tags)
		}
		if len(got.DefTags) != 1 || got.DefTags[0] != "only" {
			t.Fatalf("expected DefTags=[only], got %v", got.DefTags)
		}
	})

	t.Run("invalid query values return 400", func(t *testing.T) {
		for _, qs := range []string{"use_sni=notbool", "count=abc", "port=-1", "opt_int=xyz", "opt_uint=999999"} {
			r := gin.New()
			r.GET("/q", func(c *gin.Context) {
				_, err := bindEndpointReq[queryReq](c, http.MethodGet, false)
				if err != nil {
					writeEndpointError(c, err)
					return
				}
				jsonOK(c)
			})
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/q?"+qs, nil)
			r.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("query %q: expected 400, got %d body=%s", qs, w.Code, w.Body.String())
			}
		}
	})

	t.Run("default tag does not clobber value already bound from POST body", func(t *testing.T) {
		type hybridReq struct {
			Target string `json:"target,omitempty" query:"target" default:"all"`
		}
		var got hybridReq
		r := gin.New()
		r.POST("/hybrid", func(c *gin.Context) {
			req, err := bindEndpointReq[hybridReq](c, http.MethodPost, false)
			if err != nil {
				writeEndpointError(c, err)
				return
			}
			got = req
			jsonOK(c)
		})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/hybrid", bytes.NewBufferString(`{"target":"node2"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if got.Target != "node2" {
			t.Fatalf("expected body target node2 to be preserved, got %q", got.Target)
		}
	})
}

func TestBindEndpointReq_PathOverridesBodyAndQuery(t *testing.T) {
	type nodeGroupsReq struct {
		Name   string   `json:"name"   form:"name"   query:"name" path:"name"`
		ID     int64    `json:"id"     path:"id"`
		Groups []string `json:"groups" form:"groups"`
	}

	var got nodeGroupsReq
	r := gin.New()
	r.PUT("/node/:name/item/:id", func(c *gin.Context) {
		req, err := bindEndpointReq[nodeGroupsReq](c, http.MethodPut, false)
		if err != nil {
			writeEndpointError(c, err)
			return
		}
		got = req
		jsonOK(c)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPut,
		"/node/real-node/item/42?name=query-spoof",
		bytes.NewBufferString(`{"name":"body-spoof","id":999,"groups":["hk","us"]}`),
	)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if got.Name != "real-node" {
		t.Fatalf("expected path param real-node to override body & query, got %q", got.Name)
	}
	if got.ID != 42 {
		t.Fatalf("expected path param id=42 to override body, got %d", got.ID)
	}
	if len(got.Groups) != 2 || got.Groups[0] != "hk" || got.Groups[1] != "us" {
		t.Fatalf("expected Groups=[hk us], got %v", got.Groups)
	}

	// Invalid typed path param returns 400.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPut, "/node/real-node/item/notanint", bytes.NewBufferString(`{"groups":[]}`))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid typed path param, got %d body=%s", w2.Code, w2.Body.String())
	}
}

func TestEndpointHandlerFunc_ErrorsAndCaller(t *testing.T) {
	type echoReq struct {
		Mode string `query:"mode"`
	}
	type echoResp struct {
		User string `json:"user"`
		Role string `json:"role"`
		Mode string `json:"mode"`
	}

	ep := Endpoint[echoReq, echoResp]{
		Method: http.MethodGet,
		Path:   "/echo",
		Scope:  ScopeUser,
		Execute: func(ctx context.Context, caller Caller, req echoReq) (echoResp, error) {
			switch req.Mode {
			case "ep400":
				return echoResp{}, NewEndpointErr(http.StatusBadRequest, "bad input")
			case "biz300":
				return echoResp{}, NewBusinessErr(http.StatusOK, 300, "no available node")
			case "biz404":
				return echoResp{}, NewBusinessErr(http.StatusOK, 404, "node not found")
			case "plain":
				return echoResp{}, errors.New("unexpected failure")
			default:
				return echoResp{User: caller.Username, Role: caller.Role, Mode: req.Mode}, nil
			}
		},
	}

	s := newTestHttpServer(nil)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine(http.MethodGet, "/echo", endpointHandlerFunc(ep), mw)

	// 1. Success with JWT caller
	tokenStr := makeJWT(t, s, "alice", "normal")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/echo?mode=ok", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp echoResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.User != "alice" || resp.Role != "normal" || resp.Mode != "ok" {
		t.Fatalf("unexpected echo response: %+v", resp)
	}

	// 2. X-Token caller (admin with empty username)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/echo?mode=ok", nil)
	req.Header.Set("X-Token", s.token)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.User != "" || resp.Role != "admin" {
		t.Fatalf("expected X-Token admin caller, got %+v", resp)
	}

	// 3. Error cases (including legacy HTTP 200 + business code 300/404)
	for _, tc := range []struct {
		mode       string
		wantStatus int
		wantCode   int
		wantMsg    string
	}{
		{"ep400", http.StatusBadRequest, 400, "bad input"},
		{"biz300", http.StatusOK, 300, "no available node"},
		{"biz404", http.StatusOK, 404, "node not found"},
		{"plain", http.StatusInternalServerError, 500, "unexpected failure"},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/echo?mode="+tc.mode, nil)
		req.Header.Set("X-Token", s.token)
		r.ServeHTTP(w, req)
		if w.Code != tc.wantStatus {
			t.Fatalf("mode=%s: expected HTTP %d, got %d body=%s", tc.mode, tc.wantStatus, w.Code, w.Body.String())
		}
		var body struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("mode=%s: unmarshal body: %v", tc.mode, err)
		}
		if body.Code != tc.wantCode || body.Msg != tc.wantMsg {
			t.Fatalf("mode=%s: expected {code:%d msg:%q}, got %+v", tc.mode, tc.wantCode, tc.wantMsg, body)
		}
	}
}

func TestRegisterEndpoint_HelpAndMCPRegistrar(t *testing.T) {
	s := newTestHttpServer(nil)
	s.registerOn(s.RestfulServer, &HelpHandler{}, http.MethodGet)

	api := s.RestfulServer.Group("/api")
	type emptyReq struct{}
	type pingResp struct {
		OK bool `json:"ok"`
	}

	registerEndpoint(s, api, Endpoint[emptyReq, pingResp]{
		Method:         http.MethodGet,
		Path:           "/ping-demo",
		Scope:          ScopeUser,
		Help:           "GET /api/ping-demo help text",
		MCPName:        "ping_demo",
		MCPDescription: "Ping demo tool",
		ReadOnly:       true,
		Execute: func(ctx context.Context, caller Caller, req emptyReq) (pingResp, error) {
			return pingResp{OK: true}, nil
		},
	})
	// Endpoint without MCPName should NOT append to mcpRegistrars.
	registerEndpoint(s, api, Endpoint[emptyReq, pingResp]{
		Method: http.MethodPost,
		Path:   "/http-only",
		Scope:  ScopeAdmin,
		Help:   "POST /api/http-only help text",
		Execute: func(ctx context.Context, caller Caller, req emptyReq) (pingResp, error) {
			return pingResp{OK: true}, nil
		},
	})

	if len(s.mcpRegistrars) != 1 || s.mcpRegistrars[0].name != "ping_demo" {
		t.Fatalf("expected 1 mcp registrar (ping_demo), got %+v", s.mcpRegistrars)
	}
	if _, ok := s.handlersMap["GET /ping-demo"]; !ok {
		t.Fatalf("expected handlersMap to contain 'GET /ping-demo'")
	}
	if _, ok := s.handlersMap["POST /http-only"]; !ok {
		t.Fatalf("expected handlersMap to contain 'POST /http-only'")
	}

	// Verify /help/ping-demo returns the endpoint's Help text.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/help/ping-demo", nil)
	s.RestfulServer.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "GET /api/ping-demo help text" {
		t.Fatalf("unexpected /help/ping-demo response: code=%d body=%q", w.Code, w.Body.String())
	}

	// Verify route itself serves JSON.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/ping-demo", nil)
	s.RestfulServer.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK || !strings.Contains(w2.Body.String(), `"ok":true`) {
		t.Fatalf("unexpected /api/ping-demo response: code=%d body=%q", w2.Code, w2.Body.String())
	}
}
