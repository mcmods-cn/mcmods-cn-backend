package httpapi

import (
	"fmt"
	"testing"
)

func TestSelectLoaderArtifactVersionIsStableAndDeterministic(t *testing.T) {
	got, err := selectLoaderArtifactVersion([]string{
		"1.20.1-47.2.0",
		"1.20.1-47.4.0",
		"1.20.1-47.5.0-beta",
		"1.20.1-47.6.0-alpha",
		"1.20.1-47.7.0-rc",
	}, "1.20.1-")
	if err != nil || got != "47.4.0" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestNeoForgeArtifactPrefix(t *testing.T) {
	tests := map[string]string{
		"1.20.1":  "1.20.1-",
		"1.21":    "21.0.",
		"1.21.1":  "21.1.",
		"1.21.11": "21.11.",
		"26.2":    "26.2.0.",
		"26.2.1":  "26.2.1.",
	}
	for minecraftVersion, expected := range tests {
		if got, err := neoForgeArtifactPrefix(minecraftVersion); err != nil || got != expected {
			t.Errorf("neoForgeArtifactPrefix(%q)=(%q,%v); want %q,nil", minecraftVersion, got, err, expected)
		}
	}
	for _, invalid := range []string{"25w14craftmine", "1.21.5-pre2", "26.2-rc-1", "1.19.4"} {
		if prefix, err := neoForgeArtifactPrefix(invalid); err == nil || prefix != "" {
			t.Errorf("neoForgeArtifactPrefix(%q)=(%q,%v); want empty/error", invalid, prefix, err)
		}
	}
}

func BenchmarkSelectLoaderArtifactVersion(b *testing.B) {
	const count = 2048
	versions := make([]string, count)
	for i := range versions {
		build := (i * 701) % count
		version := fmt.Sprintf("1.20.1-47.%d.%d", build/64, build%64)
		if build%4 == 0 {
			version += "-beta"
		}
		versions[i] = version
	}
	b.ReportAllocs()
	for b.Loop() {
		if got, err := selectLoaderArtifactVersion(versions, "1.20.1-"); err != nil || got != "47.31.63" {
			b.Fatalf("selected %q: %v", got, err)
		}
	}
}
