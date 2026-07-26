//go:build integration

package systemtest

// cluster_e2e_coverage_test.go — route-hit bookkeeping for the cluster suite.
//
// Every authenticated API call made through e2eServer.apiDo is attributed to its
// gin route TEMPLATE (e.g. PUT /api/user/alice/role -> PUT /api/user/:name/role)
// and recorded here. At the end of the suite TestClusterE2E_RouteCoverage asserts
// that every route listed in pkg/http/testdata/e2e_covered_routes.txt was really
// exercised.
//
// The other half of the guard lives in pkg/http/route_coverage_test.go, which runs
// WITHOUT a build tag (so it is part of the default CI build) and fails when a
// route is registered but missing from that same file. Together:
//
//	route registered but not listed  -> default CI fails (add a case + list it)
//	route listed but never called    -> this suite fails (the case is a no-op)

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
)

var (
	routeHitsMu sync.Mutex
	routeHits   = map[string]bool{}
)

// recordRouteHit stores "METHOD /template" for one call.
func recordRouteHit(method, path string) {
	routeHitsMu.Lock()
	defer routeHitsMu.Unlock()
	routeHits[method+" "+normalizeRoutePath(path)] = true
}

func recordedRoutes() map[string]bool {
	routeHitsMu.Lock()
	defer routeHitsMu.Unlock()
	out := make(map[string]bool, len(routeHits))
	for k := range routeHits {
		out[k] = true
	}
	return out
}

// routeParamPatterns maps a concrete request path back to the gin template it was
// routed by. Only paths with a wildcard segment need an entry; everything else is
// already literal.
//
// Keep this in sync with the ":"-bearing routes in pkg/http/init.go. A path that
// matches none of these is returned unchanged, so a missed entry shows up as an
// unrecognised route in the coverage diff rather than silently passing.
var routeParamPatterns = []struct {
	re   *regexp.Regexp
	tmpl string
}{
	{regexp.MustCompile(`^/api/user/[^/]+/role$`), "/api/user/:name/role"},
	{regexp.MustCompile(`^/api/user/[^/]+/bandwidth$`), "/api/user/:name/bandwidth"},
	{regexp.MustCompile(`^/api/user/[^/]+/client-limit$`), "/api/user/:name/client-limit"},
	{regexp.MustCompile(`^/api/node/[^/]+/groups$`), "/api/node/:name/groups"},
	{regexp.MustCompile(`^/help(/.*)?$`), "/help/*relativePath"},
}

func normalizeRoutePath(path string) string {
	// Drop any query string; routing only ever considers the path.
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	for _, p := range routeParamPatterns {
		if p.re.MatchString(path) {
			return p.tmpl
		}
	}
	return path
}

// coveredRoutesFile is the shared contract between this suite and the
// no-build-tag guard in pkg/http.
const coveredRoutesFile = "../../http/testdata/e2e_covered_routes.txt"

// loadCoveredRoutes reads the expected route list. Blank lines and # comments are
// ignored so the file can explain itself.
func loadCoveredRoutes(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(coveredRoutesFile))
	if err != nil {
		t.Fatalf("read %s: %v", coveredRoutesFile, err)
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	sort.Strings(out)
	return out
}
