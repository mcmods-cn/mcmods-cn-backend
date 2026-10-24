//go:build audittools

package httpapi

// This opt-in diagnostic evaluates middleware inference using production code.
// It produces review candidates, never acceptance results, and touches no DB.
import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/activity"
)

func TestAuditToolActivityBoundaryInventory(t *testing.T) {
	input := os.Getenv("MCMODS_ACTIVITY_BOUNDARY_INVENTORY_INPUT")
	output := os.Getenv("MCMODS_ACTIVITY_BOUNDARY_INVENTORY_OUTPUT")
	if input == "" || output == "" {
		t.Fatal("set explicit read-only AST input and diagnostic output paths")
	}
	raw, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Routes    []map[string]any          `json:"routes"`
		Functions map[string]map[string]any `json:"functions"`
	}
	if err = json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	for _, route := range report.Routes {
		pattern, _ := route["pattern"].(string)
		method, path, _ := strings.Cut(pattern, " ")
		request := &http.Request{Method: method, URL: &url.URL{Path: path}}
		event, valid := requestActivityEvent(request, &requestActivity{userID: 1})
		route["default_action_id"], route["default_object_type_id"] = event.ActionID, event.ObjectTypeID
		route["default_activity_valid"] = valid
		route["default_durable"] = valid && activity.IsDurableAction(event.ActionID)
		if isHighFrequencyActivityExcluded(request) {
			route["default_classification"] = "INTENTIONAL_HIGH_FREQUENCY_EXCLUSION_CHECK_ANY_EXPLICIT_ACTIVITY"
		} else if !valid {
			route["default_classification"] = "NO_DEFAULT_ACTIVITY_CHECK_EXPLICIT_ANNOTATIONS_AND_DYNAMIC_DISPATCH"
		} else {
			route["default_classification"] = "DEFAULT_ACTIVITY_REQUIRES_BUSINESS_BOUNDARY_REVIEW"
		}
		// Reachability is informational only: a call in an error branch or another
		// handler does not prove this route's successful mutation is atomic.
		pending := []string{}
		handler, _ := route["handler"].(string)
		pending = append(pending, handler)
		visited := map[string]bool{}
		atomicFns := []string{}
		factFns := []string{}
		for len(pending) > 0 {
			name := pending[0]
			pending = pending[1:]
			if visited[name] {
				continue
			}
			visited[name] = true
			fn := report.Functions[name]
			if fn == nil {
				continue
			}
			if lines, ok := fn["atomic_enqueue_lines"].([]any); ok && len(lines) > 0 {
				atomicFns = append(atomicFns, name)
			}
			if name == "insertCatalogActivityTx" {
				factFns = append(factFns, name)
			}
			callees, _ := fn["callees"].([]any)
			for _, callee := range callees {
				if next, ok := callee.(string); ok {
					pending = append(pending, next)
				}
			}
		}
		route["static_reachable_atomic_functions"] = atomicFns
		route["static_reachable_activity_fact_functions"] = factFns
		route["verification"] = "NOT_ESTABLISHED_BY_THIS_TOOL"
	}
	out := map[string]any{"version": 1, "scope": "Diagnostic only: production middleware default inference plus AST call reachability, no HTTP/DB execution; explicit annotations, concrete route kinds and successful branches require independent verification.", "routes": report.Routes, "route_count": len(report.Routes)}
	result, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(output, append(result, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d route diagnostics; no route acceptance inferred", len(report.Routes))
}
