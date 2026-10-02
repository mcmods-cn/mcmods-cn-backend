package antiabuse

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

type antiAbuseMeasuredNode struct {
	Kind        string                  `json:"Node Type"`
	Relation    string                  `json:"Relation Name,omitempty"`
	Index       string                  `json:"Index Name,omitempty"`
	Rows        *float64                `json:"Actual Rows,omitempty"`
	Loops       *float64                `json:"Actual Loops,omitempty"`
	Removed     float64                 `json:"Rows Removed by Filter,omitempty"`
	Rechecked   float64                 `json:"Rows Removed by Index Recheck,omitempty"`
	TotalCost   *float64                `json:"Total Cost,omitempty"`
	ActualTime  *float64                `json:"Actual Total Time,omitempty"`
	SharedHits  *int64                  `json:"Shared Hit Blocks,omitempty"`
	SharedReads *int64                  `json:"Shared Read Blocks,omitempty"`
	LocalHits   *int64                  `json:"Local Hit Blocks,omitempty"`
	LocalReads  *int64                  `json:"Local Read Blocks,omitempty"`
	Children    []antiAbuseMeasuredNode `json:"Plans,omitempty"`
}

type antiAbuseMeasuredPlan struct {
	Node          antiAbuseMeasuredNode `json:"Plan"`
	ExecutionTime *float64              `json:"Execution Time,omitempty"`
}

func validateAntiAbuseMeasuredPlan(raw []byte, table, index string, expectedRows int) error {
	var plans []antiAbuseMeasuredPlan
	if err := json.Unmarshal(raw, &plans); err != nil {
		return fmt.Errorf("decode measured plan: %w", err)
	}
	if len(plans) != 1 {
		return fmt.Errorf("expected one measured plan, got %d", len(plans))
	}
	plan := plans[0]
	if plan.ExecutionTime == nil || plan.Node.ActualTime == nil || plan.Node.Rows == nil || plan.Node.Loops == nil {
		return fmt.Errorf("plan lacks ANALYZE measurements")
	}
	if *plan.Node.Rows != float64(expectedRows) || *plan.Node.Loops != 1 {
		return fmt.Errorf("result rows/loops=%g/%g want=%d/1", *plan.Node.Rows, *plan.Node.Loops, expectedRows)
	}
	if plan.Node.TotalCost == nil || *plan.Node.TotalCost > 10_000 {
		return fmt.Errorf("root cost is missing or exceeds 10000")
	}
	foundIndex := false
	var walk func(antiAbuseMeasuredNode, string) error
	walk = func(node antiAbuseMeasuredNode, inheritedRelation string) error {
		relation := inheritedRelation
		if node.Relation != "" {
			relation = node.Relation
		}
		if relation == table && node.Kind == "Seq Scan" {
			return fmt.Errorf("sequential scan on %s", table)
		}
		if node.Rows == nil || node.Loops == nil {
			return fmt.Errorf("%s lacks actual rows/loops", node.Kind)
		}
		readRows := (*node.Rows + node.Removed + node.Rechecked) * *node.Loops
		if readRows > float64(antiAbusePlanFixtureRows/50) {
			return fmt.Errorf("%s reads %g rows, budget=%d", node.Kind, readRows, antiAbusePlanFixtureRows/50)
		}
		blocks := int64(0)
		measuredBuffers := false
		for _, value := range []*int64{node.SharedHits, node.SharedReads, node.LocalHits, node.LocalReads} {
			if value != nil {
				measuredBuffers = true
				blocks += *value
			}
		}
		if !measuredBuffers {
			return fmt.Errorf("%s lacks BUFFERS measurements", node.Kind)
		}
		if blocks > 1000 {
			return fmt.Errorf("%s reads/hits %d buffer blocks, budget=1000", node.Kind, blocks)
		}
		if relation == table && node.Index == index && *node.Loops > 0 && (node.Kind == "Index Scan" || node.Kind == "Index Only Scan" || node.Kind == "Bitmap Index Scan") {
			foundIndex = true
		}
		for _, child := range node.Children {
			if err := walk(child, relation); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(plan.Node, ""); err != nil {
		return err
	}
	if !foundIndex {
		return fmt.Errorf("measured plan did not execute %s on %s", index, table)
	}
	return nil
}

func TestTEST007MeasuredPlanRejectsUnboundedOrUnmeasuredExecution(t *testing.T) {
	value := func(number float64) *float64 { return &number }
	blocks := func(number int64) *int64 { return &number }
	valid := func() []antiAbuseMeasuredPlan {
		return []antiAbuseMeasuredPlan{{ExecutionTime: value(0.7), Node: antiAbuseMeasuredNode{
			Kind: "Limit", Rows: value(100), Loops: value(1), TotalCost: value(70), ActualTime: value(0.6), LocalHits: blocks(10),
			Children: []antiAbuseMeasuredNode{{Kind: "Index Scan", Relation: "events", Index: "expected_index", Rows: value(100), Loops: value(1), TotalCost: value(1000), ActualTime: value(0.5), LocalHits: blocks(10)}},
		}}}
	}
	if raw, err := json.Marshal(valid()); err != nil || validateAntiAbuseMeasuredPlan(raw, "events", "expected_index", 100) != nil {
		t.Fatalf("valid measured index rejected: marshal=%v plan=%s", err, raw)
	}
	for _, test := range []struct {
		name   string
		mutate func([]antiAbuseMeasuredPlan)
		want   string
	}{
		{"sequential", func(plans []antiAbuseMeasuredPlan) { plans[0].Node.Children[0].Kind = "Seq Scan" }, "sequential scan"},
		{"missing index", func(plans []antiAbuseMeasuredPlan) { plans[0].Node.Children[0].Index = "other_index" }, "did not execute"},
		{"wrong relation", func(plans []antiAbuseMeasuredPlan) { plans[0].Node.Children[0].Relation = "other_table" }, "did not execute"},
		{"unbounded rows", func(plans []antiAbuseMeasuredPlan) { plans[0].Node.Children[0].Rows = value(100_000) }, "reads 100000 rows"},
		{"nested loop amplification", func(plans []antiAbuseMeasuredPlan) { plans[0].Node.Children[0].Loops = value(30) }, "reads 3000 rows"},
		{"discarded filter rows", func(plans []antiAbuseMeasuredPlan) { plans[0].Node.Children[0].Removed = 100_000 }, "reads 100100 rows"},
		{"lossy bitmap rechecks", func(plans []antiAbuseMeasuredPlan) { plans[0].Node.Children[0].Rechecked = 100_000 }, "reads 100100 rows"},
		{"buffer amplification", func(plans []antiAbuseMeasuredPlan) { plans[0].Node.Children[0].LocalReads = blocks(1000) }, "buffer blocks"},
		{"no buffers", func(plans []antiAbuseMeasuredPlan) { plans[0].Node.Children[0].LocalHits = nil }, "lacks BUFFERS"},
		{"plain explain", func(plans []antiAbuseMeasuredPlan) { plans[0].ExecutionTime = nil }, "lacks ANALYZE"},
		{"missing node measurement", func(plans []antiAbuseMeasuredPlan) { plans[0].Node.Children[0].Rows = nil }, "lacks actual"},
		{"cost", func(plans []antiAbuseMeasuredPlan) { plans[0].Node.TotalCost = value(10001) }, "root cost"},
		{"vacuous empty output", func(plans []antiAbuseMeasuredPlan) { plans[0].Node.Rows = value(0) }, "result rows"},
	} {
		t.Run(test.name, func(t *testing.T) {
			plans := valid()
			test.mutate(plans)
			raw, err := json.Marshal(plans)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateAntiAbuseMeasuredPlan(raw, "events", "expected_index", 100); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("invalid plan accepted or wrong diagnostic: %v want=%q", err, test.want)
			}
		})
	}
	for _, raw := range []string{"not-json", "[]", "[{},{ }]"} {
		if err := validateAntiAbuseMeasuredPlan([]byte(raw), "events", "expected_index", 100); err == nil {
			t.Errorf("invalid/empty plan accepted: %s", raw)
		}
	}
}
