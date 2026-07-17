package commands

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFlattenFile(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name: "replaces AREA and LOB-AREA and strips CAN- lines",
			input: "ADD TABLE \"Item\"\n" +
				"  AREA \"Data Area\"\n" +
				"  CAN-DELETE \"yes\"\n" +
				"  DESCRIPTION \"an item\"\n" +
				"\n" +
				"ADD FIELD \"Image\" OF \"Item\" AS blob\n" +
				"  LOB-AREA \"Lob Area\"\n" +
				"\n",
			want: "ADD TABLE \"Item\"\n" +
				"  AREA \"Schema Area\"\n" +
				"  DESCRIPTION \"an item\"\n" +
				"\n" +
				"ADD FIELD \"Image\" OF \"Item\" AS blob\n" +
				"  LOB-AREA \"Schema Area\"\n" +
				"\n",
		},
		{
			name:  "CRLF line endings are normalized and restored",
			input: "ADD TABLE \"Item\"\r\n  AREA \"Data Area\"\r\n\r\n",
			want:  "ADD TABLE \"Item\"\n  AREA \"Schema Area\"\n\n",
		},
		{
			name:  "content without AREA/LOB-AREA/CAN- lines is untouched",
			input: "ADD SEQUENCE \"NextItemNum\"\n  INITIAL 1000\n\n",
			want:  "ADD SEQUENCE \"NextItemNum\"\n  INITIAL 1000\n\n",
		},
		{
			name:  "non-ASCII payload elsewhere is passed through byte-for-byte",
			input: "  DESCRIPTION \"caf\xe9\"\n  AREA \"Data Area\"\n",
			want:  "  DESCRIPTION \"caf\xe9\"\n  AREA \"Schema Area\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "in.df")
			dst := filepath.Join(dir, "out.df")

			if err := os.WriteFile(src, []byte(tt.input), 0o644); err != nil {
				t.Fatalf("writing fixture: %v", err)
			}

			err := flattenFile(src, dst)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("flattenFile() error = %v", err)
			}

			got, err := os.ReadFile(dst)
			if err != nil {
				t.Fatalf("reading output: %v", err)
			}

			want := tt.want
			if runtime.GOOS == "windows" {
				want = strings.ReplaceAll(want, "\n", "\r\n")
			}

			if string(got) != want {
				t.Errorf("flattenFile() output mismatch\ngot:  %q\nwant: %q", got, want)
			}
		})
	}
}

func TestFlattenFile_MissingSource(t *testing.T) {
	dir := t.TempDir()
	err := flattenFile(filepath.Join(dir, "does-not-exist.df"), filepath.Join(dir, "out.df"))
	if err == nil {
		t.Fatal("expected error for missing source file, got nil")
	}
}

func TestRunFlatten_SingleFileInPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "in.df")
	if err := os.WriteFile(path, []byte("  AREA \"Data Area\"\n"), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	if err := runFlatten([]string{path}, ""); err != nil {
		t.Fatalf("runFlatten() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}
	if !strings.Contains(string(got), `AREA "Schema Area"`) {
		t.Errorf("expected in-place flattened content, got %q", got)
	}
}

func TestRunFlatten_SingleFileWithOutput(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.df")
	out := filepath.Join(dir, "out.df")
	if err := os.WriteFile(src, []byte("  AREA \"Data Area\"\n"), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	if err := runFlatten([]string{src}, out); err != nil {
		t.Fatalf("runFlatten() error = %v", err)
	}

	// Source must be untouched, output must be flattened.
	srcContent, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("reading source: %v", err)
	}
	if !strings.Contains(string(srcContent), `AREA "Data Area"`) {
		t.Errorf("source file was modified, want untouched: %q", srcContent)
	}

	outContent, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if !strings.Contains(string(outContent), `AREA "Schema Area"`) {
		t.Errorf("expected flattened output, got %q", outContent)
	}
}

func TestRunFlatten_DirectoryMode(t *testing.T) {
	inDir := t.TempDir()
	outDir := filepath.Join(t.TempDir(), "nested", "out")

	files := map[string]string{
		"a.df":       "  AREA \"Data Area\"\n",
		"b.df":       "  LOB-AREA \"Lob Area\"\n",
		"ignored.txt": "  AREA \"Data Area\"\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(inDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("writing fixture %s: %v", name, err)
		}
	}

	if err := runFlatten([]string{inDir}, outDir); err != nil {
		t.Fatalf("runFlatten() error = %v", err)
	}

	for _, name := range []string{"a.df", "b.df"} {
		got, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Fatalf("reading output %s: %v", name, err)
		}
		if !strings.Contains(string(got), `"Schema Area"`) {
			t.Errorf("%s: expected flattened content, got %q", name, got)
		}
	}

	if _, err := os.Stat(filepath.Join(outDir, "ignored.txt")); !os.IsNotExist(err) {
		t.Errorf("non-.df file should not have been processed, stat err = %v", err)
	}
}

func TestRunFlatten_MultipleFilesWithOutputIsError(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.df")
	b := filepath.Join(dir, "b.df")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("  AREA \"Data Area\"\n"), 0o644); err != nil {
			t.Fatalf("writing fixture: %v", err)
		}
	}

	err := runFlatten([]string{a, b}, filepath.Join(dir, "out.df"))
	if err == nil {
		t.Fatal("expected error when using --output with multiple file arguments, got nil")
	}
}

func TestRunFlatten_MultipleFilesInPlace(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.df")
	b := filepath.Join(dir, "b.df")
	if err := os.WriteFile(a, []byte("  AREA \"Data Area\"\n"), 0o644); err != nil {
		t.Fatalf("writing fixture a: %v", err)
	}
	if err := os.WriteFile(b, []byte("  LOB-AREA \"Lob Area\"\n"), 0o644); err != nil {
		t.Fatalf("writing fixture b: %v", err)
	}

	if err := runFlatten([]string{a, b}, ""); err != nil {
		t.Fatalf("runFlatten() error = %v", err)
	}

	for _, p := range []string{a, b} {
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("reading %s: %v", p, err)
		}
		if !strings.Contains(string(got), `"Schema Area"`) {
			t.Errorf("%s: expected flattened content, got %q", p, got)
		}
	}
}

func TestRunFlatten_MissingDirectory(t *testing.T) {
	err := runFlatten([]string{filepath.Join(t.TempDir(), "does-not-exist")}, "")
	if err == nil {
		t.Fatal("expected error for missing input path, got nil")
	}
}
