package httpapi

import "testing"

func TestSelectLoaderArtifactVersionIsStableAndDeterministic(t *testing.T) {
	got, err := selectLoaderArtifactVersion([]string{"1.20.1-47.2.0", "1.20.1-47.4.0", "1.20.1-47.5.0-beta"}, "1.20.1-")
	if err != nil || got != "47.4.0" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestNeoForgeArtifactPrefix(t *testing.T) {
	if got := neoForgeArtifactPrefix("1.21.1"); got != "21.1." {
		t.Fatalf("got %q", got)
	}
	if got := neoForgeArtifactPrefix("1.20.1"); got != "1.20.1-" {
		t.Fatalf("got %q", got)
	}
}
