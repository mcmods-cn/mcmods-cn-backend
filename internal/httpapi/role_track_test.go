package httpapi

import (
	"reflect"
	"testing"
	"time"
)

func TestShiftRoleTrackRoles(t *testing.T) {
	track := []string{"lv1", "lv2", "lv3"}
	tests := []struct {
		name      string
		current   []string
		direction int
		want      []string
		changed   bool
	}{
		{name: "upgrade multiple", current: []string{"lv1", "lv2"}, direction: 1, want: []string{"lv2", "lv3"}, changed: true},
		{name: "downgrade multiple", current: []string{"lv2", "lv3"}, direction: -1, want: []string{"lv1", "lv2"}, changed: true},
		{name: "clamp upper boundary", current: []string{"lv3"}, direction: 1, want: []string{"lv3"}},
		{name: "deduplicate targets", current: []string{"lv2", "lv3"}, direction: 1, want: []string{"lv3"}, changed: true},
		{name: "ignore unrelated roles", current: []string{"other"}, direction: 1, want: []string{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expiries := make(map[string]*time.Time, len(test.current))
			for _, role := range test.current {
				expiries[role] = nil
			}
			got, _, changed := shiftRoleTrackRoles(track, expiries, test.direction)
			if !reflect.DeepEqual(got, test.want) || changed != test.changed {
				t.Fatalf("shiftRoleTrackRoles() = %#v changed=%t, want %#v changed=%t", got, changed, test.want, test.changed)
			}
		})
	}
}

func TestMergedTrackUsesLongestGrant(t *testing.T) {
	track := []string{"entry", "last"}
	short := time.Unix(100, 0)
	long := short.Add(time.Hour)
	for _, tc := range []struct{ entry, last, want *time.Time }{
		{&short, &long, &long}, {&long, &short, &long}, {nil, &long, nil}, {&long, nil, nil},
	} {
		_, got, _ := shiftRoleTrackRoles(track, map[string]*time.Time{"entry": tc.entry, "last": tc.last}, 1)
		if expiry, present := got["last"]; !present || expiry != tc.want {
			t.Fatalf("entry=%v last=%v gave expiry=%v, present=%t", tc.entry, tc.last, expiry, present)
		}
	}
}
