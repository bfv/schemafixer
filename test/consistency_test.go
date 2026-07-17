// Package test contains cross-implementation consistency checks that verify
// the Python port (python/schemafixer.py) produces byte-for-byte identical
// output to the Go implementation (cmd/schemafixer). The Go implementation
// is treated as the source of truth: any divergence is reported as a
// failure of the Python port, never the other way around.
package test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"
)

// repoRoot returns the absolute path to the repository root, derived from
// this test file's own location so it works regardless of the directory
// `go test` is invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("determining working directory: %v", err)
	}
	// This file lives in <root>/test, so the parent directory is the root.
	return filepath.Dir(wd)
}

var (
	goBinaryOnce sync.Once
	goBinaryPath string
	goBinaryErr  error
)

// buildGoBinary compiles cmd/schemafixer once per test run and returns the
// path to the resulting executable.
func buildGoBinary(t *testing.T, root string) string {
	t.Helper()
	goBinaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "schemafixer-bin-*")
		if err != nil {
			goBinaryErr = err
			return
		}
		bin := filepath.Join(dir, "schemafixer")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/schemafixer")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			goBinaryErr = fmt.Errorf("building go binary: %w\n%s", err, out)
			return
		}
		goBinaryPath = bin
	})
	if goBinaryErr != nil {
		t.Fatalf("%v", goBinaryErr)
	}
	return goBinaryPath
}

// pythonInterpreter locates a usable python3/python interpreter that has
// PyYAML installed. Tests are skipped (not failed) when unavailable, since
// this is an environment precondition rather than a code defect.
func pythonInterpreter(t *testing.T) string {
	t.Helper()
	candidates := []string{"python3", "python"}
	for _, name := range candidates {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if err := exec.Command(path, "-c", "import yaml").Run(); err != nil {
			continue
		}
		return path
	}
	t.Skip("no python interpreter with PyYAML found in PATH; skipping Go-vs-Python consistency test")
	return ""
}

// runToFile invokes name with args, appending "-o" outPath (an absolute
// path), with the given working directory, and returns the resulting
// file's contents.
func runToFile(t *testing.T, workDir, outPath, name string, args ...string) []byte {
	t.Helper()
	fullArgs := append(append([]string{}, args...), "-o", outPath)
	cmd := exec.Command(name, fullArgs...)
	cmd.Dir = workDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("running %s %v: %v\nstderr:\n%s", name, fullArgs, err, stderr.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output %q: %v", outPath, err)
	}
	return data
}

// TestGoVsPythonConsistency runs each subcommand through both the Go binary
// and the Python port with identical arguments and asserts byte-identical
// output files. Go is authoritative; a mismatch fails the Python side.
func TestGoVsPythonConsistency(t *testing.T) {
	root := repoRoot(t)
	python := pythonInterpreter(t)
	goBin := buildGoBinary(t, root)
	pyScript := filepath.Join(root, "python", "schemafixer.py")

	// compareYAML: the "parse" command's output formatting has a benign,
	// well-known gopkg.in/yaml.v3 quirk (inconsistent indent step for maps
	// nested inside sequence items) that PyYAML cannot byte-replicate. For
	// that case only, compare parsed structure rather than raw bytes; all
	// other commands produce plain-text/.df output where byte-for-byte
	// equality is both achievable and meaningful.
	cases := []struct {
		name        string
		args        []string
		outFile     string
		compareYAML bool
	}{
		{
			name:    "apply",
			args:    []string{"apply", "schema/sports2020.df", "model/rules.yaml"},
			outFile: "apply-out.df",
		},
		{
			name:        "parse",
			args:        []string{"parse", "schema/sports2020.df", "model/default.yaml"},
			outFile:     "parse-out.yaml",
			compareYAML: true,
		},
		{
			name:    "diff",
			args:    []string{"diff", "schema/sports2020.df", "schema/sports2020-prd.df"},
			outFile: "diff-out.txt",
		},
		{
			name:    "diff-tablemove",
			args:    []string{"diff", "schema/sports2020.df", "schema/sports2020-prd.df", "--tablemove", "kvstore"},
			outFile: "diff-tablemove-out.txt",
		},
		{
			name:    "flatten",
			args:    []string{"flatten", "schema/sports2020.df"},
			outFile: "flatten-out.df",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			goDir := t.TempDir()
			pyDir := t.TempDir()

			goOut := runToFile(t, root, filepath.Join(goDir, tc.outFile), goBin, tc.args...)
			pyArgs := append([]string{pyScript}, tc.args...)
			pyOut := runToFile(t, root, filepath.Join(pyDir, tc.outFile), python, pyArgs...)

			if tc.compareYAML {
				assertYAMLEqual(t, goOut, pyOut)
				return
			}

			if !bytes.Equal(goOut, pyOut) {
				t.Errorf(
					"python output diverges from go (source of truth) for args %v\n--- go (%d bytes) ---\n%s\n--- python (%d bytes) ---\n%s",
					tc.args, len(goOut), goOut, len(pyOut), pyOut,
				)
			}
		})
	}
}

// assertYAMLEqual parses both byte slices as YAML and fails the test if the
// resulting structures are not deeply equal, regardless of formatting.
func assertYAMLEqual(t *testing.T, goOut, pyOut []byte) {
	t.Helper()

	var goVal, pyVal any
	if err := yaml.Unmarshal(goOut, &goVal); err != nil {
		t.Fatalf("unmarshalling go output as YAML: %v", err)
	}
	if err := yaml.Unmarshal(pyOut, &pyVal); err != nil {
		t.Fatalf("unmarshalling python output as YAML: %v", err)
	}

	if !reflect.DeepEqual(goVal, pyVal) {
		t.Errorf(
			"python YAML output diverges from go (source of truth)\n--- go ---\n%s\n--- python ---\n%s",
			goOut, pyOut,
		)
	}
}
