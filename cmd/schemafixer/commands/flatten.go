package commands

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

// Regular expressions for the flatten command.
var (
	reFlattenArea    = regexp.MustCompile(`(?m)^  AREA ".*"$`)
	reFlattenLobArea = regexp.MustCompile(`(?m)^  LOB-AREA ".*"$`)
	reFlattenCan     = regexp.MustCompile(`(?m)^  CAN-.*$\n?`)
)

const (
	flattenAreaReplacement    = `  AREA "Schema Area"`
	flattenLobAreaReplacement = `  LOB-AREA "Schema Area"`
)

// NewFlattenCmd builds and returns the 'flatten' cobra command.
func NewFlattenCmd() *cobra.Command {
	var outputPath string

	cmd := &cobra.Command{
		Use:   "flatten <directory|file.df> [file2.df ...]",
		Short: `Reset all AREA/LOB-AREA values to "Schema Area" and strip CAN- lines`,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFlatten(args, outputPath)
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Write result to this file/directory instead of overwriting in place (single input: file path; directory input: output directory)")
	return cmd
}

// runFlatten resolves the input arguments to a concrete file list and processes each one.
func runFlatten(args []string, outputPath string) error {
	var files []string
	dirMode := false

	if len(args) == 1 {
		info, err := os.Stat(args[0])
		if err != nil {
			return fmt.Errorf("stat %q: %w", args[0], err)
		}
		if info.IsDir() {
			dirMode = true
			entries, err := os.ReadDir(args[0])
			if err != nil {
				return fmt.Errorf("reading directory %q: %w", args[0], err)
			}
			for _, e := range entries {
				if e.Type().IsRegular() && strings.EqualFold(filepath.Ext(e.Name()), ".df") {
					files = append(files, filepath.Join(args[0], e.Name()))
				}
			}
		} else {
			files = append(files, args[0])
		}
	} else {
		files = args
	}

	if outputPath != "" {
		if dirMode {
			if err := os.MkdirAll(outputPath, 0o755); err != nil {
				return fmt.Errorf("creating output directory %q: %w", outputPath, err)
			}
		} else if len(files) > 1 {
			return fmt.Errorf("--output cannot be used with multiple file arguments; specify a single file or a directory")
		}
	}

	for _, path := range files {
		var dest string
		switch {
		case outputPath == "":
			dest = path
		case dirMode:
			dest = filepath.Join(outputPath, filepath.Base(path))
		default:
			dest = outputPath
		}

		if err := flattenFile(path, dest); err != nil {
			return fmt.Errorf("flattening %q: %w", path, err)
		}
	}

	return nil
}

// flattenFile applies the flatten transformations to srcPath and writes the
// result to destPath.
//
// The .df trailer declares its own codepage via a "cpstream=<name>" line
// (see the end of any .df file), so no encoding assumption is made here.
// The file is treated as a raw byte sequence — exactly like readLines/
// processDF in apply.go — since AREA/LOB-AREA/CAN- constructs are always
// plain ASCII; any multi-byte payload elsewhere in the file (descriptions,
// labels, etc.) is passed through untouched regardless of its codepage.
func flattenFile(srcPath, destPath string) error {
	log.Debug().Str("file", srcPath).Msg("processing")

	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}

	// Normalize to LF for regex processing, then restore platform line
	// endings on write.
	content := strings.ReplaceAll(string(raw), "\r\n", "\n")

	areaCount := len(reFlattenArea.FindAllString(content, -1))
	lobAreaCount := len(reFlattenLobArea.FindAllString(content, -1))
	canCount := len(reFlattenCan.FindAllString(content, -1))

	newContent := reFlattenArea.ReplaceAllString(content, flattenAreaReplacement)
	newContent = reFlattenLobArea.ReplaceAllString(newContent, flattenLobAreaReplacement)
	newContent = reFlattenCan.ReplaceAllString(newContent, "")

	lineEnding := "\n"
	if runtime.GOOS == "windows" {
		lineEnding = "\r\n"
	}
	if lineEnding != "\n" {
		newContent = strings.ReplaceAll(newContent, "\n", lineEnding)
	}

	if err := os.WriteFile(destPath, []byte(newContent), fileMode(srcPath)); err != nil {
		return err
	}

	log.Info().
		Str("file", filepath.Base(srcPath)).
		Int("area", areaCount).
		Int("lobArea", lobAreaCount).
		Int("canDeleted", canCount).
		Msg("flattened")

	return nil
}

// fileMode preserves the source file's permission bits, falling back to 0644.
func fileMode(path string) fs.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode()
	}
	return 0o644
}
