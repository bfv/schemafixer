package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var (
	reByteField   = regexp.MustCompile(`(?i)^ADD FIELD "[^"]+" OF "[^"]+" AS (?:character|raw)\b`)
	reFormatWidth = regexp.MustCompile(`(?i)^\s*FORMAT "x\((\d+)\)"\s*$`)
	reMaxWidth    = regexp.MustCompile(`^(\s*MAX-WIDTH\s+)\d+(\s*)$`)
)

// NewFixWidthCmd builds and returns the 'fixwidth' cobra command.
func NewFixWidthCmd() *cobra.Command {
	var outputPath string
	cmd := &cobra.Command{
		Use:   "fixwidth <schema.df>",
		Short: "Fix MAX-WIDTH values from character and raw FORMAT values",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFixWidth(args[0], outputPath)
		},
	}
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Write output to file instead of stdout")
	return cmd
}

func runFixWidth(dfPath, outputPath string) error {
	lines, err := readLines(dfPath)
	if err != nil {
		return fmt.Errorf("reading df file: %w", err)
	}

	lineEnding := "\n"
	if runtime.GOOS == "windows" {
		lineEnding = "\r\n"
	}

	hasChecksum := len(lines) > 0 && reChecksum.MatchString(lines[len(lines)-1])
	if hasChecksum {
		lines = lines[:len(lines)-1]
	}

	var buf bytes.Buffer
	processWidthDF(lines, &buf, lineEnding)

	var out io.Writer = os.Stdout
	if outputPath != "" {
		file, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("creating output file %q: %w", outputPath, err)
		}
		defer file.Close()
		out = file
	}
	if _, err := out.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	if hasChecksum {
		if _, err := fmt.Fprintf(out, "%010d%s", buf.Len(), lineEnding); err != nil {
			return fmt.Errorf("writing checksum: %w", err)
		}
	}
	return nil
}

func processWidthDF(lines []string, buf *bytes.Buffer, lineEnding string) {
	fieldWidth := 0
	for _, line := range lines {
		if reAddField.MatchString(line) {
			fieldWidth = 0
			if reByteField.MatchString(line) {
				fieldWidth = -1
			}
		} else if strings.TrimSpace(line) == "" {
			fieldWidth = 0
		}

		if fieldWidth == -1 {
			if width := maxWidthFromFormat(line); width > 0 {
				fieldWidth = width
			}
		}
		if fieldWidth > 0 {
			if m := reMaxWidth.FindStringSubmatch(line); m != nil {
				line = m[1] + strconv.Itoa(fieldWidth) + m[2]
			}
		}
		buf.WriteString(line)
		buf.WriteString(lineEnding)
	}
}

func maxWidthFromFormat(line string) int {
	m := reFormatWidth.FindStringSubmatch(line)
	if m == nil {
		return 0
	}
	width, err := strconv.Atoi(m[1])
	if err != nil || width <= 0 {
		return 0
	}
	if width > 31995/2 {
		return 31995
	}
	return width * 2
}
