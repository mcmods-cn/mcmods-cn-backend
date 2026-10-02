// db_suite runs every discovered Go test/fuzz seed in bounded serial batches.
// This avoids a single package alarm accumulating every large DB fixture's
// setup time. It does not change a test's own context or query-plan budgets.
package main

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var testNamePattern = regexp.MustCompile(`^(Test|Fuzz)[A-Za-z0-9_]+$`)

func main() {
	batchSize := flag.Int("batch-size", 30, "maximum top-level tests per serial invocation")
	race := flag.Bool("race", false, "enable the race detector for every batch")
	flag.Parse()
	if *batchSize < 1 || *batchSize > 100 {
		fatal(errors.New("batch-size must be between 1 and 100"))
	}
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		fatal(errors.New("MCMODS_RUN_DB_INTEGRATION=1 is required; configure an owned development/test PostgreSQL database first"))
	}
	if err := validateTestEnvironment(os.Getenv("APP_ENV"), os.Getenv("DATABASE_URL"), os.Getenv("MCMODS_TEST_DATABASE_URL")); err != nil {
		fatal(err)
	}
	goBinary := os.Getenv("MCMODS_GO_BINARY")
	if goBinary == "" {
		goBinary = "go"
	}
	packages := flag.Args()
	if len(packages) == 0 {
		packages = []string{"./..."}
	}
	listedPackages, err := listPackages(exec.Command(goBinary, append([]string{"list"}, packages...)...))
	if err != nil {
		fatal(err)
	}
	var discovered, invoked, batches, failures int
	for _, pkg := range listedPackages {
		output, listErr := exec.Command(goBinary, "test", "-list", "^(Test|Fuzz)", pkg).CombinedOutput()
		if listErr != nil {
			fatal(fmt.Errorf("discover tests in %s: %w\n%s", pkg, listErr, output))
		}
		names := parseTestNames(string(output))
		discovered += len(names)
		for _, group := range testBatches(names, *batchSize) {
			batches++
			arguments := []string{"test", "-p=1", "-parallel=1", "-timeout=10m", "-count=1", "-v"}
			if *race {
				arguments = append(arguments, "-race")
			}
			arguments = append(arguments, "-run", "^("+strings.Join(group, "|")+")$", pkg)
			fmt.Printf("DB_SUITE batch=%d package=%s tests=%d first=%s last=%s\n", batches, pkg, len(group), group[0], group[len(group)-1])
			command := exec.Command(goBinary, arguments...)
			command.Stdout, command.Stderr = os.Stdout, os.Stderr
			err = command.Run()
			invoked += len(group)
			if err != nil {
				failures++
				fmt.Fprintf(os.Stderr, "DB_SUITE batch=%d FAILED: %v\n", batches, err)
			}
		}
	}
	fmt.Printf("DB_SUITE discovered=%d invoked=%d batches=%d failed_batches=%d (conditional SKIP remains visible in Go output)\n", discovered, invoked, batches, failures)
	if discovered == 0 || invoked != discovered || failures > 0 {
		os.Exit(1)
	}
}

func listPackages(command *exec.Cmd) ([]string, error) {
	// Go writes module download and other diagnostics to stderr. Only stdout
	// contains package paths; keep diagnostics visible without parsing them.
	command.Stderr = os.Stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("list packages: %w\n%s", err, output)
	}
	return strings.Fields(string(output)), nil
}

func parseTestNames(output string) []string {
	var names []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if testNamePattern.MatchString(line) {
			names = append(names, line)
		}
	}
	return names
}

func testBatches(names []string, size int) [][]string {
	var batches [][]string
	for start := 0; start < len(names); start += size {
		end := min(start+size, len(names))
		batches = append(batches, names[start:end])
	}
	return batches
}

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }

// Both legacy and opt-in fixtures must resolve to the same owned loopback test
// database. This runner never initializes or deletes databases itself.
func validateTestEnvironment(environment, databaseURL, integrationURL string) error {
	if environment != "test" || databaseURL == "" || databaseURL != integrationURL {
		return errors.New("APP_ENV=test and identical nonempty DATABASE_URL/MCMODS_TEST_DATABASE_URL are required")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return errors.New("expected a PostgreSQL test URL")
	}
	if host := parsed.Hostname(); host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return errors.New("database batches only accept loopback PostgreSQL")
	}
	name := strings.TrimPrefix(parsed.Path, "/")
	if !regexp.MustCompile(`^(test_[a-z0-9_]+|[a-z0-9_]+_test)$`).MatchString(name) {
		return errors.New("database name must start with test_ or end with _test")
	}
	return nil
}
