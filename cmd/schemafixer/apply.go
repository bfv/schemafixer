package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Regular expressions for .df construct detection and area replacement.
var (
	reAddTable    = regexp.MustCompile(`(?i)^ADD TABLE "([^"]+)"`)
	reAddField    = regexp.MustCompile(`(?i)^ADD FIELD "([^"]+)" OF "([^"]+)"`)
	reAddIndex    = regexp.MustCompile(`(?i)^ADD INDEX "([^"]+)" ON "([^"]+)"`)
	reAddSequence = regexp.MustCompile(`(?i)^ADD SEQUENCE `)
	reChecksum    = regexp.MustCompile(`^\d{10}$`)
	reArea        = regexp.MustCompile(`^(  AREA ")([^"]+)(".*$)`)
	reLobArea     = regexp.MustCompile(`^(  LOB-AREA ")([^"]+)(".*$)`)
)

// parseState tracks which kind of .df construct is currently being parsed.
type parseState int

const (
	stateNone parseState = iota
	stateTable
	stateField
	stateIndex
	stateOther // sequences and unrecognised constructs — pass through unchanged
)

// newApplyCmd builds and returns the 'apply' cobra command.
func newApplyCmd() *cobra.Command {
	var outputFile string

	cmd := &cobra.Command{
		Use:   "apply <schema.df> <rules.yaml>",
		Short: "Apply area rules to a .df schema file",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Bind the cobra flag into viper so it can be read uniformly.
			if err := viper.BindPFlag("output", cmd.Flags().Lookup("output")); err != nil {
				return err
			}
			return runApply(args[0], args[1], viper.GetString("output"))
		},
	}

	cmd.Flags().StringVarP(&outputFile, "output", "o", "", "Write output to file instead of stdout")
	return cmd
}

// runApply is the entry point for the apply command.
func runApply(dfPath, rulesPath, outputPath string) error {
	log.Debug().Str("df", dfPath).Str("rules", rulesPath).Str("output", outputPath).Msg("apply started")

	rules, err := loadRules(rulesPath)
	if err != nil {
		return fmt.Errorf("loading rules: %w", err)
	}
	log.Debug().
		Int("tables", len(rules.SchemaFixer.Tables)).
		Str("defaultTable", rules.SchemaFixer.Defaults.Table).
		Str("defaultIndex", rules.SchemaFixer.Defaults.Index).
		Str("defaultLob", rules.SchemaFixer.Defaults.Lob).
		Msg("rules loaded")

	lines, err := readLines(dfPath)
	if err != nil {
		return fmt.Errorf("reading df file: %w", err)
	}
	log.Debug().Int("lines", len(lines)).Msg("df file read")

	// Use platform-appropriate line endings.
	lineEnding := "\n"
	if runtime.GOOS == "windows" {
		lineEnding = "\r\n"
	}

	// Detect and strip the trailing checksum line (10 decimal digits).
	hasChecksum := false
	processLines := lines
	if len(lines) > 0 && reChecksum.MatchString(lines[len(lines)-1]) {
		hasChecksum = true
		processLines = lines[:len(lines)-1]
		log.Debug().Msg("trailing checksum detected — will recalculate")
	}

	// Transform the .df content into a buffer.
	var buf bytes.Buffer
	if err := processDF(processLines, &rules.SchemaFixer, &buf, lineEnding); err != nil {
		return fmt.Errorf("processing df file: %w", err)
	}

	// Resolve output writer.
	var out io.Writer = os.Stdout
	if outputPath != "" {
		f, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("creating output file %q: %w", outputPath, err)
		}
		defer f.Close()
		out = f
		log.Debug().Str("path", outputPath).Msg("writing to file")
	}

	if _, err := out.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	if hasChecksum {
		byteCount := buf.Len()
		checksumLine := fmt.Sprintf("%010d%s", byteCount, lineEnding)
		if _, err := fmt.Fprint(out, checksumLine); err != nil {
			return fmt.Errorf("writing checksum: %w", err)
		}
		log.Debug().Int("byteCount", byteCount).Msg("checksum written")
	}

	log.Debug().Msg("apply complete")
	return nil
}

// processDF runs the state-machine line transformer and writes results to buf.
func processDF(lines []string, rules *SchemaFixerRules, buf *bytes.Buffer, lineEnding string) error {
	state := stateNone
	var currentTable, currentField, currentIndex string

	for _, line := range lines {
		// ── Detect construct type from ADD … lines ────────────────────────
		if m := reAddTable.FindStringSubmatch(line); m != nil {
			currentTable = m[1]
			currentField = ""
			currentIndex = ""
			state = stateTable
			log.Debug().Str("table", currentTable).Msg("parsing TABLE")

		} else if m := reAddField.FindStringSubmatch(line); m != nil {
			currentField = m[1]
			currentTable = m[2]
			currentIndex = ""
			state = stateField
			log.Debug().Str("field", currentField).Str("table", currentTable).Msg("parsing FIELD")

		} else if m := reAddIndex.FindStringSubmatch(line); m != nil {
			currentIndex = m[1]
			currentTable = m[2]
			currentField = ""
			state = stateIndex
			log.Debug().Str("index", currentIndex).Str("table", currentTable).Msg("parsing INDEX")

		} else if reAddSequence.MatchString(line) {
			currentTable = ""
			currentField = ""
			currentIndex = ""
			state = stateOther

		} else if strings.TrimSpace(line) == "" {
			// Blank line marks end of current construct.
			state = stateNone
		}

		// ── Area substitution ─────────────────────────────────────────────
		switch state {
		case stateTable:
			if m := reArea.FindStringSubmatch(line); m != nil {
				area := rules.tableArea(currentTable)
				line = m[1] + area + m[3]
				log.Debug().Str("table", currentTable).Str("area", area).Msg("TABLE area replaced")
			}

		case stateIndex:
			if m := reArea.FindStringSubmatch(line); m != nil {
				area := rules.indexArea(currentTable, currentIndex)
				line = m[1] + area + m[3]
				log.Debug().Str("index", currentIndex).Str("table", currentTable).Str("area", area).Msg("INDEX area replaced")
			}

		case stateField:
			if m := reLobArea.FindStringSubmatch(line); m != nil {
				area := rules.lobArea(currentTable, currentField)
				line = m[1] + area + m[3]
				log.Debug().Str("field", currentField).Str("table", currentTable).Str("area", area).Msg("LOB-AREA replaced")
			}
		}

		buf.WriteString(line)
		buf.WriteString(lineEnding)
	}

	return nil
}

// ── Rules lookup helpers ──────────────────────────────────────────────────────

// tableArea returns the area for a table, falling back to the default.
func (r *SchemaFixerRules) tableArea(tableName string) string {
	for _, t := range r.Tables {
		if strings.EqualFold(t.Name, tableName) && t.Area != "" {
			return t.Area
		}
	}
	return r.Defaults.Table
}

// indexArea returns the area for a specific index on a table, falling back
// to the table-level override and then the global default.
func (r *SchemaFixerRules) indexArea(tableName, indexName string) string {
	for _, t := range r.Tables {
		if strings.EqualFold(t.Name, tableName) {
			for k, v := range t.Indexes {
				if strings.EqualFold(k, indexName) {
					return v
				}
			}
		}
	}
	return r.Defaults.Index
}

// lobArea returns the LOB area for a specific field on a table, falling
// back to the global default.
func (r *SchemaFixerRules) lobArea(tableName, fieldName string) string {
	for _, t := range r.Tables {
		if strings.EqualFold(t.Name, tableName) {
			for k, v := range t.Lob {
				if strings.EqualFold(k, fieldName) {
					return v
				}
			}
		}
	}
	return r.Defaults.Lob
}

// ── File I/O helpers ──────────────────────────────────────────────────────────

// loadRules reads and unmarshals the YAML rules file.
func loadRules(path string) (*RulesFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rules RulesFile
	if err := yaml.Unmarshal(data, &rules); err != nil {
		return nil, err
	}
	return &rules, nil
}

// readLines reads a file and returns its lines with line endings stripped.
// bufio.Scanner automatically handles both LF and CRLF.
func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}
