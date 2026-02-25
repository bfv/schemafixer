package commands

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

// areaRecord holds the area assignment for a single .df construct.
type areaRecord struct {
	constructType string // TABLE, INDEX, LOB
	displayName   string // e.g. "Customer", "Customer.CustNum", "Item.ItemImage"
	key           string // lowercase unique key for matching
	area          string
}

// NewDiffCmd builds and returns the 'diff' cobra command.
func NewDiffCmd() *cobra.Command {
	var outputFile string

	cmd := &cobra.Command{
		Use:   "diff <source.df> <target.df>",
		Short: "Show area differences between two .df schema files",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(args[0], args[1], outputFile)
		},
	}

	cmd.Flags().StringVarP(&outputFile, "output", "o", "", "Write output to file instead of stdout")
	return cmd
}

// runDiff is the entry point for the diff command.
func runDiff(sourcePath, targetPath, outputPath string) error {
	log.Debug().Str("source", sourcePath).Str("target", targetPath).Str("output", outputPath).Msg("diff started")

	sourceLines, err := readLines(sourcePath)
	if err != nil {
		return fmt.Errorf("reading source df: %w", err)
	}
	targetLines, err := readLines(targetPath)
	if err != nil {
		return fmt.Errorf("reading target df: %w", err)
	}

	sourceRecords := extractAreas(sourceLines)
	targetRecords := extractAreas(targetLines)
	log.Debug().Int("sourceConstructs", len(sourceRecords)).Int("targetConstructs", len(targetRecords)).Msg("areas extracted")

	// Build lookup maps keyed by the lowercase key.
	sourceMap := make(map[string]*areaRecord, len(sourceRecords))
	targetMap := make(map[string]*areaRecord, len(targetRecords))
	for i := range sourceRecords {
		sourceMap[sourceRecords[i].key] = &sourceRecords[i]
	}
	for i := range targetRecords {
		targetMap[targetRecords[i].key] = &targetRecords[i]
	}

	// Collect differences, preserving source order, then target-only extras.
	const missing = "(not present)"
	var rows []diffRow

	// Walk source records — compare against target.
	seenKeys := map[string]bool{}
	for _, rec := range sourceRecords {
		seenKeys[rec.key] = true
		tgt, ok := targetMap[rec.key]
		if !ok {
			// Present in source only.
			rows = append(rows, diffRow{rec.constructType, rec.displayName, rec.area, missing})
			continue
		}
		if !strings.EqualFold(rec.area, tgt.area) {
			rows = append(rows, diffRow{rec.constructType, rec.displayName, rec.area, tgt.area})
		}
	}

	// Walk target records — add those not in source.
	for _, rec := range targetRecords {
		if !seenKeys[rec.key] {
			rows = append(rows, diffRow{rec.constructType, rec.displayName, missing, rec.area})
		}
	}

	if len(rows) == 0 {
		fmt.Println("No area differences found.")
		return nil
	}

	out := io.Writer(os.Stdout)
	if outputPath != "" {
		f, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("creating output file: %w", err)
		}
		defer f.Close()
		out = f
	}

	printDiffTable(out, rows)
	log.Debug().Int("differences", len(rows)).Msg("diff complete")
	return nil
}

// diffRow holds one line of diff output.
type diffRow struct {
	constructType string
	displayName   string
	sourceArea    string
	targetArea    string
}

// printDiffTable renders the diff as a fixed-column table.
func printDiffTable(w io.Writer, rows []diffRow) {
	// Determine column widths dynamically.
	const (
		hConstruct = "CONSTRUCT"
		hName      = "NAME"
		hSource    = "SOURCE AREA"
		hTarget    = "TARGET AREA"
	)

	wConstruct := len(hConstruct)
	wName := len(hName)
	wSource := len(hSource)

	for _, r := range rows {
		if len(r.constructType) > wConstruct {
			wConstruct = len(r.constructType)
		}
		if len(r.displayName) > wName {
			wName = len(r.displayName)
		}
		if len(r.sourceArea) > wSource {
			wSource = len(r.sourceArea)
		}
	}

	// Add padding between columns.
	wConstruct += 2
	wName += 2
	wSource += 2

	fmtRow := func(c, n, s, t string) {
		fmt.Fprintf(w, "%-*s%-*s%-*s%s\n", wConstruct, c, wName, n, wSource, s, t)
	}

	fmtRow(hConstruct, hName, hSource, hTarget)
	fmtRow(strings.Repeat("-", wConstruct-2), strings.Repeat("-", wName-2), strings.Repeat("-", wSource-2), strings.Repeat("-", len(hTarget)))

	for _, r := range rows {
		fmtRow(r.constructType, r.displayName, r.sourceArea, r.targetArea)
	}
}

// extractAreas parses a .df file and returns ordered area records.
func extractAreas(lines []string) []areaRecord {
	var records []areaRecord

	state := stateNone
	var currentTable, currentField, currentIndex string

	for _, line := range lines {
		if m := reAddTable.FindStringSubmatch(line); m != nil {
			currentTable = m[1]
			currentField = ""
			currentIndex = ""
			state = stateTable

		} else if m := reAddField.FindStringSubmatch(line); m != nil {
			currentField = m[1]
			currentTable = m[2]
			currentIndex = ""
			state = stateField

		} else if m := reAddIndex.FindStringSubmatch(line); m != nil {
			currentIndex = m[1]
			currentTable = m[2]
			currentField = ""
			state = stateIndex

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
				records = append(records, areaRecord{
					constructType: "TABLE",
					displayName:   currentTable,
					key:           "table:" + strings.ToLower(currentTable),
					area:          m[2],
				})
			}

		case stateIndex:
			if m := reArea.FindStringSubmatch(line); m != nil {
				records = append(records, areaRecord{
					constructType: "INDEX",
					displayName:   currentTable + "." + currentIndex,
					key:           "index:" + strings.ToLower(currentTable) + "." + strings.ToLower(currentIndex),
					area:          m[2],
				})
			}

		case stateField:
			if m := reLobArea.FindStringSubmatch(line); m != nil {
				records = append(records, areaRecord{
					constructType: "LOB",
					displayName:   currentTable + "." + currentField,
					key:           "lob:" + strings.ToLower(currentTable) + "." + strings.ToLower(currentField),
					area:          m[2],
				})
			}
		}
	}

	return records
}
