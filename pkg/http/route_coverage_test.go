package http

// route_coverage_test.go — the enforcement half of the integration-coverage
// contract. This file has NO build tag, so it runs in the default CI build.
//
// Rule: every HTTP route this server registers must have a case in the cluster
// integration suite (pkg/proxy/systemtest/cluster_e2e_api_test.go) and must be
// listed in testdata/e2e_covered_routes.txt.
//
//	route registered but not listed -> this test fails (default CI)
//	route listed but never called   -> TestClusterE2E_FullAPI/route_coverage fails
//
// Adding an endpoint without integration coverage therefore breaks the build,
// rather than quietly shipping untested.

import (
	"os"
	"sort"
	"strings"
	"testing"
)

const coveredRoutesPath = "testdata/e2e_covered_routes.txt"

// registeredRoutes returns "METHOD /full/path" for every route the server wires
// up, taken from gin itself so group prefixes and param templates are exact.
//
// clusterEnabled is on because the node-groups routes only exist in that mode and
// they must be covered too.
func registeredRoutes(t *testing.T) []string {
	t.Helper()

	s := NewHttpServer()
	s.token = "route-coverage-token"
	s.jwtSecret = "route-coverage-secret-32-chars!!"
	s.clusterEnabled = true
	s.registerRoutes()
	// Registered separately from registerRoutes (only when prometheus is on), but
	// it is a real route and must be covered like any other.
	s.RegisterHandler(&MetricHandler{}, "GET")

	var out []string
	for _, r := range s.RestfulServer.Routes() {
		out = append(out, r.Method+" "+r.Path)
	}
	sort.Strings(out)
	return out
}

func loadCoveredRoutes(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(coveredRoutesPath)
	if err != nil {
		t.Fatalf("read %s: %v", coveredRoutesPath, err)
	}
	out := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[line] = true
	}
	return out
}

// TestRouteCoverage_EveryRouteHasIntegrationCoverage fails when a route is added
// without a corresponding integration case.
func TestRouteCoverage_EveryRouteHasIntegrationCoverage(t *testing.T) {
	covered := loadCoveredRoutes(t)

	var missing []string
	for _, route := range registeredRoutes(t) {
		if !covered[route] {
			missing = append(missing, route)
		}
	}

	if len(missing) > 0 {
		t.Errorf(`%d route(s) are registered but have no integration coverage:

  %s

Every endpoint must be exercised against a real cluster. To fix:
  1. add a case to pkg/proxy/systemtest/cluster_e2e_api_test.go (TestClusterE2E_FullAPI)
     that actually calls the route, and
  2. add the "METHOD /path" line above to pkg/http/%s

Run the suite with:
  go test ./pkg/proxy/systemtest -tags=integration -run TestClusterE2E -v -timeout 20m`,
			len(missing), strings.Join(missing, "\n  "), coveredRoutesPath)
	}
}

// TestRouteCoverage_NoStaleEntries catches the opposite drift: a route was
// removed or renamed but its line was left behind, which would otherwise make the
// integration suite fail with a confusing "never called" error.
func TestRouteCoverage_NoStaleEntries(t *testing.T) {
	registered := map[string]bool{}
	for _, r := range registeredRoutes(t) {
		registered[r] = true
	}

	var stale []string
	for line := range loadCoveredRoutes(t) {
		if !registered[line] {
			stale = append(stale, line)
		}
	}
	sort.Strings(stale)

	if len(stale) > 0 {
		t.Errorf("%s lists %d route(s) that are no longer registered "+
			"(renamed or removed?); drop them and the matching integration case:\n  %s",
			coveredRoutesPath, len(stale), strings.Join(stale, "\n  "))
	}
}
