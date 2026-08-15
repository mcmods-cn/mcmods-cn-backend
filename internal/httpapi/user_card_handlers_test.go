package httpapi

import "testing"

func TestNormalizePublicCardSlots(t *testing.T) {
	t.Parallel()
	slots, err := normalizePublicCardSlots([]string{"comment_count", "", "edit_count", "", "map_count", ""})
	if err != nil || len(slots) != 6 || slots[2] != "edit_count" {
		t.Fatalf("unexpected normalized slots: %#v, %v", slots, err)
	}
	if _, err = normalizePublicCardSlots([]string{"comment_count"}); err == nil {
		t.Fatal("slot configuration with the wrong length must be rejected")
	}
	if _, err = normalizePublicCardSlots([]string{"email", "", "", "", "", ""}); err == nil {
		t.Fatal("a non-whitelisted statistic must be rejected")
	}
	if _, err = normalizePublicCardSlots([]string{"comment_count", "comment_count", "", "", "", ""}); err == nil {
		t.Fatal("duplicate public statistics must be rejected")
	}
}

func TestMapPublicOnlineVisibilityNeverExposesHiddenState(t *testing.T) {
	t.Parallel()
	if got := mapPublicOnlineVisibility(false, true); got != publicOnlineStatusHidden {
		t.Fatalf("active hidden user was exposed as %q", got)
	}
	if got := mapPublicOnlineVisibility(false, false); got != publicOnlineStatusHidden {
		t.Fatalf("inactive hidden user was exposed as %q", got)
	}
}
