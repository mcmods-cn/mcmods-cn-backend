package activity

import "testing"

func TestMAP003ActivityDictionaryOwnsCodeIDRelationships(t *testing.T) {
	actions := ActionDefinitions()
	objects := ObjectTypeDefinitions()
	if len(actions) != 11 || len(objects) != 27 {
		t.Fatalf("activity dictionary sizes=(%d,%d), want (11,27)", len(actions), len(objects))
	}
	for _, entry := range append(actions, objects...) {
		if entry.ID <= 0 || entry.Code == "" || entry.Name == "" {
			t.Fatalf("invalid activity dictionary entry: %#v", entry)
		}
	}
	if ActionID(" CheckIn ") != ActionCheckIn || ObjectTypeID(" SERVER ") != ObjectServer {
		t.Fatal("activity code lookup does not normalize through the authoritative dictionary")
	}
	if ActionID("check_in") != 0 || ObjectTypeID("minecraft_server") != 0 {
		t.Fatal("activity dictionary accepted a non-canonical alias")
	}

	actions[0].Code = "mutated"
	objects[0].Code = "mutated"
	if ActionID("edit") != ActionEdit || ObjectTypeID("recipe") != ObjectRecipe {
		t.Fatal("callers can mutate the authoritative activity dictionary")
	}
}
