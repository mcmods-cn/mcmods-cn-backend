package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreatorTeamRoleMetadataRequiresExactServerRouteAndValidCurrentDTO(t *testing.T) {
	const route = "/api/v1/creators/team00001/members"
	const member = `{"creatorId":"author001","roleId":"role00001","title":"Developer"}`
	valid := `{"members":[` + member + `]}`
	tooMany := make([]creatorMemberPayload, maximumCreatorTeamMembers+1)
	for index := range tooMany {
		tooMany[index] = creatorMemberPayload{CreatorID: fmt.Sprintf("author%03d", index), RoleID: "role00001", Title: "Developer"}
	}
	oversized, err := json.Marshal(map[string]any{"members": tooMany})
	if err != nil {
		t.Fatal("encode bounded synthetic member payload")
	}
	maximum, err := json.Marshal(map[string]any{"members": tooMany[:maximumCreatorTeamMembers]})
	if err != nil {
		t.Fatal("encode exact maximum synthetic member payload")
	}
	cases := []struct {
		name, method, path, body string
		metadata                 bool
	}{
		{"role label", http.MethodPut, route, valid, true},
		{"remove relationship", http.MethodPut, route, `{"members":[]}`, true},
		{"existing normalization", http.MethodPut, route, `{"members":[{"creatorId":" AUTHOR001 ","roleId":" ROLE00001 ","title":" Developer "}]}`, true},
		{"other method", http.MethodPost, route, valid, false},
		{"profile route", http.MethodPut, "/api/v1/creators/team00001", valid, false},
		{"new content route", http.MethodPost, "/api/v1/creators", valid, false},
		{"extra path", http.MethodPut, route + "/other", valid, false},
		{"trailing slash", http.MethodPut, route + "/", valid, false},
		{"invalid team identity", http.MethodPut, "/api/v1/creators/team/members", valid, false},
		{"missing members", http.MethodPut, route, `{}`, false},
		{"null members", http.MethodPut, route, `{"members":null}`, false},
		{"null root", http.MethodPut, route, `null`, false},
		{"wrong type", http.MethodPut, route, `{"members":[{"creatorId":9,"roleId":"role00001","title":"Developer"}]}`, false},
		{"invalid author", http.MethodPut, route, `{"members":[{"creatorId":"bad","roleId":"role00001","title":"Developer"}]}`, false},
		{"invalid role", http.MethodPut, route, `{"members":[{"creatorId":"author001","roleId":"bad","title":"Developer"}]}`, false},
		{"duplicate relation", http.MethodPut, route, `{"members":[` + member + `,` + member + `]}`, false},
		{"title exact byte maximum", http.MethodPut, route, `{"members":[{"creatorId":"author001","roleId":"role00001","title":"` + strings.Repeat("x", 160) + `"}]}`, true},
		{"title over byte maximum", http.MethodPut, route, `{"members":[{"creatorId":"author001","roleId":"role00001","title":"` + strings.Repeat("x", 161) + `"}]}`, false},
		{"title byte limit", http.MethodPut, route, `{"members":[{"creatorId":"author001","roleId":"role00001","title":"` + strings.Repeat("界", 54) + `"}]}`, false},
		{"member exact maximum", http.MethodPut, route, string(maximum), true},
		{"member count limit", http.MethodPut, route, string(oversized), false},
		{"client exemption flag", http.MethodPut, route, `{"members":[` + member + `],"skipFingerprint":true}`, false},
		{"future narrative field", http.MethodPut, route, `{"members":[` + member + `],"contentMarkdown":"a new narrative"}`, false},
		{"nested narrative field", http.MethodPut, route, `{"members":[{"creatorId":"author001","roleId":"role00001","title":"Developer","content":"a new narrative"}]}`, false},
		{"malformed JSON", http.MethodPut, route, `{"members":[`, false},
		{"multiple documents", http.MethodPut, route, valid + `{}`, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			body, _ := inspectAntiAbuseBody(request)
			if !bytes.Equal(body, []byte(test.body)) {
				t.Fatal("anti-abuse inspection must preserve exact handler input")
			}
			if got := creatorTeamRoleMetadataRequest(request, body); got != test.metadata {
				t.Fatalf("server-owned metadata classification=%v, expected=%v", got, test.metadata)
			}
		})
	}
	request := httptest.NewRequest(http.MethodPut, route, strings.NewReader(valid))
	request.Header.Set("Content-Type", "application/json")
	_, content := inspectAntiAbuseBody(request)
	if content != "Developer" || antiAbuseAction(request, nil) != "review.submit" {
		t.Fatal("relationship classification must retain the title and normal risk action")
	}
}
