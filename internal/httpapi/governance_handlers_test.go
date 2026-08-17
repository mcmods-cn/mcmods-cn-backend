package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReportReasonRegistryCoversEveryTargetAndOther(t *testing.T) {
	expected := []string{"mod", "plugin", "map", "shader", "resource_pack", "datapack", "addon", "discussion", "bug", "news", "tutorial", "skin", "blueprint", "server", "comment", "user"}
	for _, target := range expected {
		reasons, ok := reportTargetReasons[target]
		if !ok || len(reasons) == 0 {
			t.Fatalf("target %q has no report reason registry", target)
		}
		seen := map[string]bool{}
		for _, reason := range reasons {
			if reason == "other" || seen[reason] {
				t.Fatalf("target %q contains invalid or duplicate reason %q", target, reason)
			}
			seen[reason] = true
		}
		if !validReportReason(target, "other") {
			t.Fatalf("target %q does not accept the required other reason", target)
		}
	}
	if !validReportReason("server", "commercial_as_public") || !validReportReason("news", "fake_news") ||
		!validReportReason("user", "avatar_violation") || !validReportReason("mod", "copyright_theft") ||
		!validReportReason("comment", "comment_spam") {
		t.Fatal("one or more target-specific governance reasons are missing")
	}
	if validReportReason("news", "commercial_as_public") || validReportReason("server", "fake_news") {
		t.Fatal("unrelated target-specific reason leaked into another target")
	}
}

func TestReportReasonsEndpointAndUnknownTarget(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/reports/reasons?targetType=server", nil)
	response := httptest.NewRecorder()
	reportReasons(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			Version int `json:"version"`
			Items   []struct {
				Code               string `json:"code"`
				RequiresCustomText bool   `json:"requiresCustomText"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Version != reportReasonVersion || envelope.Data.Items[len(envelope.Data.Items)-1].Code != "other" || !envelope.Data.Items[len(envelope.Data.Items)-1].RequiresCustomText {
		t.Fatalf("unexpected reason response: %+v", envelope.Data)
	}

	response = httptest.NewRecorder()
	reportReasons(response, httptest.NewRequest(http.MethodGet, "/api/v1/reports/reasons?targetType=arbitrary_table", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("arbitrary target must be rejected, got %d", response.Code)
	}
}

func TestNormalizeReportRouteType(t *testing.T) {
	tests := map[string]string{
		"shader_pack": "shader", "minecraft_server": "server", "community_discussion": "discussion",
		"community_issue": "bug", "community_bug": "bug", "community_news": "news", "community_tutorial": "tutorial",
	}
	for input, expected := range tests {
		if actual := normalizeReportRouteType(input); actual != expected {
			t.Fatalf("normalizeReportRouteType(%q)=%q, want %q", input, actual, expected)
		}
	}
}

func TestUnifiedReportsUseAntiAbuseReportPolicy(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/reports", nil)
	if action := antiAbuseAction(request); action != "report.create" {
		t.Fatalf("anti-abuse action=%q, want report.create", action)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/reports/evidence/uploads", nil)
	if action := antiAbuseAction(request); action != "report.create" {
		t.Fatalf("evidence anti-abuse action=%q, want report.create", action)
	}
}

func TestPublicBanStatus(t *testing.T) {
	now := time.Now().UTC()
	future, past, revoked := now.Add(time.Hour), now.Add(-time.Hour), now.Add(-time.Minute)
	for _, test := range []struct {
		name, status, want string
		ends, revoked      *time.Time
	}{
		{name: "permanent", status: "active", want: "permanent"},
		{name: "temporary", status: "active", ends: &future, want: "temporary"},
		{name: "elapsed", status: "active", ends: &past, want: "released"},
		{name: "expired", status: "expired", want: "released"},
		{name: "revoked", status: "revoked", revoked: &revoked, want: "released"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := publicBanStatus(test.status, test.ends, test.revoked, now); got != test.want {
				t.Fatalf("publicBanStatus() = %q, want %q", got, test.want)
			}
		})
	}
}
