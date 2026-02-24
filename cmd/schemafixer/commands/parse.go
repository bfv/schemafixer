package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// NewParseCmd builds and returns the 'parse' cobra command.
func NewParseCmd() *cobra.Command {
	var outputFile string

	cmd := &cobra.Command{
		Use:   "parse <schema.df> <rules.yaml>",
		Short: "Generate a rules file from an existing .df schema",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runParse(args[0], args[1], outputFile)
		},
	}

	cmd.Flags().StringVarP(&outputFile, "output", "o", "", "Write output to file instead of stdout")
	return cmd
}

// runParse is the entry point for the parse command.
func runParse(dfPath, rulesPath, outputPath string) error {
	log.Debug().Str("df", dfPath).Str("rules", rulesPath).Str("output", outputPath).Msg("parse started")

	rules, err := loadRules(rulesPath)
	if err != nil {
		return fmt.Errorf("loading rules: %w", err)
	}
	defaults := rules.SchemaFixer.Defaults
	log.Debug().
		Str("defaultTable", defaults.Table).
		Str("defaultIndex", defaults.Index).
		Str("defaultLob", defaults.Lob).
		Msg("defaults loaded")

	lines, err := readLines(dfPath)
	if err != nil {
		return fmt.Errorf("reading df file: %w", err)
	}
	log.Debug().Int("lines", len(lines)).Msg("df file read")

	// tableRules accumulates per-table rules keyed by lowercased table name.
	// We also keep insertion order via a separate slice.
	type tableEntry struct {
		name    string // original casing from .df
		area    string // non-default area, empty = use default
		indexes map[string]string
		lob     map[string]string
	}
	tableOrder := []string{} // lower-cased names, insertion order
	tableMap := map[string]*tableEntry{}

	getOrCreate := func(tableName string) *tableEntry {
		key := strings.ToLower(tableName)
		if _, ok := tableMap[key]; !ok {
			tableMap[key] = &tableEntry{
				name:    tableName,
				indexes: map[string]string{},
				lob:     map[string]string{},
			}
			tableOrder = append(tableOrder, key)
		}
		return tableMap[key]
	}

	// State machine — same approach as apply.
	state := stateNone
	var currentTable, currentField, currentIndex string

	for _, line := range lines {
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
			state = stateNone
		}

		switch state {
		case stateTable:
			if m := reArea.FindStringSubmatch(line); m != nil {
				area := m[2]
				if !strings.EqualFold(area, defaults.Table) {
					e := getOrCreate(currentTable)
					e.area = area
					log.Debug().Str("table", currentTable).Str("area", area).Msg("non-default TABLE area")
				}
			}

		case stateIndex:
			if m := reArea.FindStringSubmatch(line); m != nil {
				area := m[2]
				if !strings.EqualFold(area, defaults.Index) {
					e := getOrCreate(currentTable)
					e.indexes[strings.ToLower(currentIndex)] = area
					log.Debug().Str("index", currentIndex).Str("table", currentTable).Str("area", area).Msg("non-default INDEX area")
				}
			}

		case stateField:
			if m := reLobArea.FindStringSubmatch(line); m != nil {
				area := m[2]
				if !strings.EqualFold(area, defaults.Lob) {
					e := getOrCreate(currentTable)
					e.lob[currentField] = area
					log.Debug().Str("field", currentField).Str("table", currentTable).Str("area", area).Msg("non-default LOB area")
				}
			}
		}
	}

	// Build the output RulesFile — defaults come first, then per-table rules.
	out := RulesFile{
		SchemaFixer: SchemaFixerRules{
			Version:  rules.SchemaFixer.Version,
			Defaults: defaults,
		},
	}

	for _, key := range tableOrder {
		e := tableMap[key]
		tr := TableRule{
			Name: e.name,
			Area: e.area,
		}
		if len(e.indexes) > 0 {
			tr.Indexes = e.indexes
		}
		if len(e.lob) > 0 {
			tr.Lob = e.lob
		}
		out.SchemaFixer.Tables = append(out.SchemaFixer.Tables, tr)
	}

	// Marshal to YAML.
	data, err := yaml.Marshal(&out)
	if err != nil {
		return fmt.Errorf("marshalling yaml: %w", err)
	}

	// Resolve output writer.
	var w io.Writer = os.Stdout
	if outputPath != "" {
		f, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("creating output file %q: %w", outputPath, err)
		}
		defer f.Close()
		w = bufio.NewWriter(f)
		log.Debug().Str("path", outputPath).Msg("writing to file")
		defer w.(*bufio.Writer).Flush()
	}

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	log.Debug().Int("tables", len(out.SchemaFixer.Tables)).Msg("parse complete")
	return nil
}
