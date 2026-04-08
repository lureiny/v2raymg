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

func newSubServer(uris []string) *httptest.Server {
	body := base64.StdEncoding.EncodeToString([]byte(strings.Join(uris, "\n")))
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
}

func TestRemoteFetcher_AllSuccess(t *testing.T) {
	srv1 := newSubServer([]string{"trojan://a@h1:443#n1"})
	srv2 := newSubServer([]string{"vless://b@h2:443#n2", "ss://c@h3:8388#n3"})
	defer srv1.Close()
	defer srv2.Close()

	f := NewRemoteFetcher([]string{srv1.URL, srv2.URL})
	specs, err := f.Fetch()
	require.NoError(t, err)
	require.Len(t, specs, 3)
	assert.Equal(t, "trojan://a@h1:443#n1", specs[0].URI)
	assert.Equal(t, "vless://b@h2:443#n2", specs[1].URI)
	assert.Equal(t, "ss://c@h3:8388#n3", specs[2].URI)
}

func TestRemoteFetcher_PartialFailure(t *testing.T) {
	srv := newSubServer([]string{"vless://ok@h:443#n"})
	defer srv.Close()

	f := NewRemoteFetcher([]string{"http://127.0.0.1:1", srv.URL}) // first URL unreachable
	specs, err := f.Fetch()
	// 部分失败时有成功 specs → 不返回 error
	require.NoError(t, err)
	require.Len(t, specs, 1)
	assert.Equal(t, "vless://ok@h:443#n", specs[0].URI)
}

func TestRemoteFetcher_AllFail(t *testing.T) {
	f := NewRemoteFetcher([]string{"http://127.0.0.1:1", "http://127.0.0.1:2"})
	specs, err := f.Fetch()
	assert.Error(t, err)
	assert.Nil(t, specs)
	assert.Contains(t, err.Error(), "all remote subscription sources failed")
}

func TestRemoteFetcher_EmptyURLs(t *testing.T) {
	f := NewRemoteFetcher(nil)
	specs, err := f.Fetch()
	require.NoError(t, err)
	assert.Nil(t, specs)
}

func TestRemoteFetcher_EmptySlice(t *testing.T) {
	f := NewRemoteFetcher([]string{})
	specs, err := f.Fetch()
	require.NoError(t, err)
	assert.Nil(t, specs)
}

func TestRemoteFetcher_URLReturnsEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(""))
	}))
	defer srv.Close()

	f := NewRemoteFetcher([]string{srv.URL})
	specs, err := f.Fetch()
	require.NoError(t, err)
	// FetchExternalSub returns nil for empty body → no specs produced
	assert.Nil(t, specs)
}
