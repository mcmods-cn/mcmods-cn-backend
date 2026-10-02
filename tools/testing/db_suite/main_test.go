package main

import (
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"testing"
)

func TestPackageListSeparatesDiagnosticsAndRejectsFailedDiscovery(t *testing.T) {
	for _, scenario := range []string{"success", "failure"} {
		t.Run(scenario, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestPackageListHelperProcess$", "--", scenario)
			command.Env = append(os.Environ(), "MCMODS_PACKAGE_LIST_HELPER=1")
			packages, err := listPackages(command)
			if scenario == "failure" {
				if err == nil || len(packages) != 0 {
					t.Fatalf("failed discovery returned packages=%v err=%v", packages, err)
				}
				return
			}
			want := []string{"example.test/first", "example.test/second"}
			if err != nil || !reflect.DeepEqual(packages, want) {
				t.Fatalf("package list includes diagnostics or loses packages: got=%v want=%v err=%v", packages, want, err)
			}
		})
	}
}

func TestPackageListHelperProcess(t *testing.T) {
	if os.Getenv("MCMODS_PACKAGE_LIST_HELPER") != "1" {
		return
	}
	fmt.Fprintln(os.Stderr, "go: downloading example.test/dependency v1.0.0")
	fmt.Fprintln(os.Stdout, "example.test/first\nexample.test/second")
	if os.Args[len(os.Args)-1] == "failure" {
		fmt.Fprintln(os.Stderr, "go: package discovery failed")
		os.Exit(1)
	}
	os.Exit(0)
}

func TestDiscoveryKeepsEveryTestAndFuzzSeedWithoutPackageNoise(t *testing.T) {
	got := parseTestNames("TestSchema\r\nTestHTTP\nFuzzCodec\nok\tpackage\t0.01s\n? package [no test files]\nTestInvalid/name\n")
	want := []string{"TestSchema", "TestHTTP", "FuzzCodec"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("names=%v want=%v", got, want)
	}
}

func TestBatchesKeepEveryNameExactlyOnceAndRespectBudget(t *testing.T) {
	input := []string{"TestA", "TestB", "TestC", "TestD", "FuzzE"}
	var actual []string
	for _, batch := range testBatches(input, 2) {
		if len(batch) < 1 || len(batch) > 2 {
			t.Fatalf("batch=%v", batch)
		}
		actual = append(actual, batch...)
	}
	if !reflect.DeepEqual(actual, input) {
		t.Fatalf("batches lost or repeated names: %v", actual)
	}
	if len(testBatches(nil, 2)) != 0 {
		t.Fatal("empty packages must not create a fake batch")
	}
}

func TestDatabaseBatchesRejectMissingMismatchedRemoteAndNonTestTargets(t *testing.T) {
	local := "postgres://postgres@127.0.0.1:5432/mcmods_test?sslmode=disable"
	for _, test := range []struct {
		name, environment, primary, integration string
		valid                                   bool
	}{
		{"owned", "test", local, local, true},
		{"fresh", "test", "postgresql://postgres@localhost/test_phase1_123", "postgresql://postgres@localhost/test_phase1_123", true},
		{"production", "production", local, local, false},
		{"missing", "test", "", "", false},
		{"different", "test", local, local + "&application_name=other", false},
		{"remote", "test", "postgres://postgres@database.example.test/mcmods_test", "postgres://postgres@database.example.test/mcmods_test", false},
		{"shared", "test", "postgres://postgres@127.0.0.1/postgres", "postgres://postgres@127.0.0.1/postgres", false},
		{"wrong protocol", "test", "https://localhost/mcmods_test", "https://localhost/mcmods_test", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateTestEnvironment(test.environment, test.primary, test.integration); (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
}
