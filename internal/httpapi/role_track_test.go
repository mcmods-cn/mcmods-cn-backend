package httpapi

import (
	"reflect"
	"testing"
)

func TestShiftRoleTrackRoles(t *testing.T) {
	track := []string{"lv1", "lv2", "lv3"}
	tests := []struct {
		name      string
		current   []string
		direction int
		want      []string
	}{
		{name: "upgrade multiple", current: []string{"lv1", "lv2"}, direction: 1, want: []string{"lv2", "lv3"}},
		{name: "downgrade multiple", current: []string{"lv2", "lv3"}, direction: -1, want: []string{"lv1", "lv2"}},
		{name: "clamp upper boundary", current: []string{"lv3"}, direction: 1, want: []string{"lv3"}},
		{name: "deduplicate targets", current: []string{"lv2", "lv3"}, direction: 1, want: []string{"lv3"}},
		{name: "ignore unrelated roles", current: []string{"other"}, direction: 1, want: []string{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shiftRoleTrackRoles(track, test.current, test.direction); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("shiftRoleTrackRoles() = %#v, want %#v", got, test.want)
			}
		})
	}
}
