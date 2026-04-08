package subscription

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchExternalSub_Base64(t *testing.T) {
	uris := []string{
		"trojan://pass@host1:443#node1",
		"vless://uuid@host2:443#node2",
		"ss://YWVzLTI1Ni1nY206cGFzcw@host3:8388#node3",
	}
	body := base64.StdEncoding.EncodeToString([]byte(strings.Join(uris, "\n")))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	got, err := FetchExternalSub(srv.URL)
	require.NoError(t, err)
	assert.Equal(t, uris, got)
}

func TestFetchExternalSub_Plaintext(t *testing.T) {
	uris := []string{
		"trojan://pass@host1:443#node1",
		"vless://uuid@host2:443#node2",
	}
	body := strings.Join(uris, "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	got, err := FetchExternalSub(srv.URL)
	require.NoError(t, err)
	assert.Equal(t, uris, got)
}

func TestFetchExternalSub_EmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(""))
	}))
	defer srv.Close()

	got, err := FetchExternalSub(srv.URL)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestFetchExternalSub_BlankLines(t *testing.T) {
	uris := "trojan://pass@host:443#n1\n\n\nvless://uuid@host:443#n2\n\n"
	body := base64.StdEncoding.EncodeToString([]byte(uris))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	got, err := FetchExternalSub(srv.URL)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"trojan://pass@host:443#n1",
		"vless://uuid@host:443#n2",
	}, got)
}

func TestFetchExternalSub_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := FetchExternalSub(srv.URL)
	assert.Error(t, err)
}

func TestFetchExternalSub_CRLF(t *testing.T) {
	// Windows 风格换行 \r\n，TrimSpace 应正确处理
	uris := "trojan://pass@host:443#n1\r\nvless://uuid@host:443#n2\r\n"
	body := base64.StdEncoding.EncodeToString([]byte(uris))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	got, err := FetchExternalSub(srv.URL)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"trojan://pass@host:443#n1",
		"vless://uuid@host:443#n2",
	}, got)
}

func TestFetchExternalSub_URLSafeBase64(t *testing.T) {
	// 使用 URL-safe base64 编码，tryBase64Decode 应正确解码
	uris := []string{
		"trojan://pass@host1:443#node1",
		"vless://uuid@host2:443#node2",
	}
	body := base64.URLEncoding.EncodeToString([]byte(strings.Join(uris, "\n")))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	got, err := FetchExternalSub(srv.URL)
	require.NoError(t, err)
	assert.Equal(t, uris, got)
}

func TestFetchExternalSub_RawStdBase64NoPadding(t *testing.T) {
	// 使用无 padding 的标准 base64 编码，tryBase64Decode 应正确解码
	uris := []string{
		"vless://uuid@host:443#n1",
	}
	body := base64.RawStdEncoding.EncodeToString([]byte(strings.Join(uris, "\n")))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	got, err := FetchExternalSub(srv.URL)
	require.NoError(t, err)
	assert.Equal(t, uris, got)
}

// --- FetchAndMergeExtSubs 测试 ---

func TestFetchAndMergeExtSubs_Empty(t *testing.T) {
	uris, truncated := FetchAndMergeExtSubs(nil)
	assert.Nil(t, uris)
	assert.False(t, truncated)

	uris, truncated = FetchAndMergeExtSubs([]string{})
	assert.Nil(t, uris)
	assert.False(t, truncated)
}

func TestFetchAndMergeExtSubs_MultipleURLs_OrderPreserved(t *testing.T) {
	srv1 := newSubServer([]string{"trojan://a@h1:443#n1"})
	srv2 := newSubServer([]string{"vless://b@h2:443#n2", "ss://c@h3:8388#n3"})
	defer srv1.Close()
	defer srv2.Close()

	uris, truncated := FetchAndMergeExtSubs([]string{srv1.URL, srv2.URL})
	assert.False(t, truncated)
	require.Len(t, uris, 3)
	// 顺序必须与传入 URL 顺序一致
	assert.Equal(t, "trojan://a@h1:443#n1", uris[0])
	assert.Equal(t, "vless://b@h2:443#n2", uris[1])
	assert.Equal(t, "ss://c@h3:8388#n3", uris[2])
}

func TestFetchAndMergeExtSubs_PartialFailure(t *testing.T) {
	srv := newSubServer([]string{"vless://ok@h:443#n"})
	defer srv.Close()

	uris, truncated := FetchAndMergeExtSubs([]string{"http://127.0.0.1:1", srv.URL})
	assert.False(t, truncated)
	require.Len(t, uris, 1)
	assert.Equal(t, "vless://ok@h:443#n", uris[0])
}

func TestFetchAndMergeExtSubs_AllFail(t *testing.T) {
	uris, truncated := FetchAndMergeExtSubs([]string{"http://127.0.0.1:1", "http://127.0.0.1:2"})
	assert.False(t, truncated)
	assert.Empty(t, uris)
}

func TestFetchAndMergeExtSubs_Truncation(t *testing.T) {
	// 构造 MaxExtSubs+2 个 URL，前 MaxExtSubs 个有效，后 2 个应被截断
	urls := make([]string, 0, MaxExtSubs+2)
	servers := make([]*httptest.Server, MaxExtSubs)
	for i := 0; i < MaxExtSubs; i++ {
		srv := newSubServer([]string{
			"vless://u@h:443#" + strings.Repeat("x", i+1),
		})
		servers[i] = srv
		urls = append(urls, srv.URL)
	}
	defer func() {
		for _, s := range servers {
			s.Close()
		}
	}()

	// 多加 2 个不可达 URL（应被截断不请求）
	urls = append(urls, "http://127.0.0.1:1", "http://127.0.0.1:2")

	uris, truncated := FetchAndMergeExtSubs(urls)
	assert.True(t, truncated)
	assert.Len(t, uris, MaxExtSubs)
}

func TestFetchAndMergeExtSubs_ExactlyMaxExtSubs(t *testing.T) {
	urls := make([]string, MaxExtSubs)
	servers := make([]*httptest.Server, MaxExtSubs)
	for i := 0; i < MaxExtSubs; i++ {
		srv := newSubServer([]string{"vless://u@h:443#" + strings.Repeat("x", i+1)})
		servers[i] = srv
		urls[i] = srv.URL
	}
	defer func() {
		for _, s := range servers {
			s.Close()
		}
	}()

	uris, truncated := FetchAndMergeExtSubs(urls)
	assert.False(t, truncated)
	assert.Len(t, uris, MaxExtSubs)
}

func TestFetchExternalSub_RawURLSafeBase64(t *testing.T) {
	// 使用无 padding 的 URL-safe base64 编码
	uris := []string{
		"trojan://pass@host:443#n1",
		"ss://YWVzLTI1Ni1nY206cGFzcw@host:8388#n2",
	}
	body := base64.RawURLEncoding.EncodeToString([]byte(strings.Join(uris, "\n")))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	got, err := FetchExternalSub(srv.URL)
	require.NoError(t, err)
	assert.Equal(t, uris, got)
}

// --- tryBase64Decode 单元测试 ---

func TestTryBase64Decode(t *testing.T) {
	content := "vless://uuid@host:443#node\ntrojan://pass@host:443#node"

	tests := []struct {
		name    string
		encode  func([]byte) string
		wantErr bool
	}{
		{"StdEncoding", func(b []byte) string { return base64.StdEncoding.EncodeToString(b) }, false},
		{"RawStdEncoding", func(b []byte) string { return base64.RawStdEncoding.EncodeToString(b) }, false},
		{"URLEncoding", func(b []byte) string { return base64.URLEncoding.EncodeToString(b) }, false},
		{"RawURLEncoding", func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }, false},
		{"plaintext_URIs", func(b []byte) string { return string(b) }, true},
		{"empty_string", func([]byte) string { return "" }, true},
		{"pure_garbage", func([]byte) string { return "not base64 at all !!!" }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := tt.encode([]byte(content))
			decoded, err := tryBase64Decode(input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, content, string(decoded))
			}
		})
	}
}

func TestTryBase64Decode_PlaintextNotMisdecoded(t *testing.T) {
	// 明文 URI 不应被 Raw 变体误判为 base64 后产生乱码
	plaintext := "ss://YWVzLTI1Ni1nY206cGFzcw@host:8388#node1\nvless://uuid@host:443#node2"
	_, err := tryBase64Decode(plaintext)
	// 即使 Raw 变体能"解码"这段字符串，启发式校验（UTF-8 + ://）应拒绝乱码结果
	// 所以 tryBase64Decode 应返回 error，让调用方 fallback 到明文
	assert.Error(t, err)
}

func TestLooksLikeURIList(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"valid_uris", []byte("vless://uuid@host:443\ntrojan://pass@host:443"), true},
		{"single_uri", []byte("ss://method:pass@host:8388"), true},
		{"no_scheme", []byte("just some random text"), false},
		{"binary_garbage", []byte{0xff, 0xfe, 0x00, 0x01}, false},
		{"empty", []byte(""), false},
		{"utf8_with_url_but_not_proxy", []byte("visit https://google.com for info"), true}, // 设计如此：宽松匹配
		{"bare_scheme_separator", []byte("://"), true},                                      // 已知边界行为
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, looksLikeURIList(tt.data))
		})
	}
}

// --- P2: httpGet 边界测试 ---

func TestHttpGet_ConnectionRefused(t *testing.T) {
	_, err := httpGet("http://127.0.0.1:1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "httpGet: do request")
}

func TestHttpGet_InvalidURL(t *testing.T) {
	_, err := httpGet("://missing-scheme")
	assert.Error(t, err)
}

func TestHttpGet_Non200StatusCodes(t *testing.T) {
	for _, code := range []int{403, 404, 502} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer srv.Close()
			_, err := httpGet(srv.URL)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "httpGet: status")
		})
	}
}

func TestHttpGet_LargeBodyTruncated(t *testing.T) {
	// 服务器返回 >4MB 响应，httpGet 应只读取 maxResponseBody 字节
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 写 5MB
		chunk := make([]byte, 1024)
		for i := range chunk {
			chunk[i] = 'A'
		}
		for i := 0; i < 5*1024; i++ {
			w.Write(chunk)
		}
	}))
	defer srv.Close()

	data, err := httpGet(srv.URL)
	require.NoError(t, err)
	assert.Equal(t, int(maxResponseBody), len(data))
}

func TestHttpGet_RequestHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "*/*", r.Header.Get("Accept"))
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	data, err := httpGet(srv.URL)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(data))
}

// --- P2: FetchExternalSub 边界测试 ---

func TestFetchExternalSub_WhitespaceBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("   \n\t  \n  "))
	}))
	defer srv.Close()

	got, err := FetchExternalSub(srv.URL)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestFetchExternalSub_InvalidURL(t *testing.T) {
	_, err := FetchExternalSub("http://127.0.0.1:1")
	assert.Error(t, err)
}

func TestFetchExternalSub_Base64AllBlankLines(t *testing.T) {
	// base64("\n\n\n") = "CgoK"，解码后不含 "://" → tryBase64Decode 拒绝 →
	// fallback 明文 → "CgoK" 作为单行返回。这是已知行为：内容不像 URI 列表时
	// 走明文路径，不会丢数据。
	body := base64.StdEncoding.EncodeToString([]byte("\n\n\n"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	got, err := FetchExternalSub(srv.URL)
	require.NoError(t, err)
	assert.Equal(t, []string{"CgoK"}, got)
}

// --- P2: tryBase64Decode 边界测试 ---

func TestTryBase64Decode_ValidBase64ButNoScheme(t *testing.T) {
	// base64("hello world") 能解码为合法 UTF-8 但不含 "://" → 应返回 error
	encoded := base64.StdEncoding.EncodeToString([]byte("hello world"))
	_, err := tryBase64Decode(encoded)
	assert.Error(t, err)
}

func TestTryBase64Decode_ValidBase64ButBinary(t *testing.T) {
	// base64 编码的二进制数据 → 解码非 UTF-8 → 应返回 error
	encoded := base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe, 0x00, 0x01, 0x80})
	_, err := tryBase64Decode(encoded)
	assert.Error(t, err)
}

// --- SanitizeURL 单元测试 ---

func TestSanitizeURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"normal_url", "https://example.com/path?key=secret", "https://example.com"},
		{"with_port", "https://example.com:8443/path", "https://example.com:8443"},
		{"with_userinfo", "https://user:pass@example.com/path", "https://example.com"},
		{"http_scheme", "http://host/sub?token=abc123", "http://host"},
		{"no_scheme", "//host/path", "<invalid-url>"},
		{"no_host", "file:///etc/passwd", "<invalid-url>"},
		{"javascript", "javascript:alert(1)", "<invalid-url>"},
		{"completely_invalid", ":::", "<invalid-url>"},
		{"empty_string", "", "<invalid-url>"},
		{"bare_text", "not a url at all", "<invalid-url>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, SanitizeURL(tt.input))
		})
	}
}
