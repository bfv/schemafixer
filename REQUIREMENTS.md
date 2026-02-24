# requirements

## project goal
This project is about fixing OpenEdge database schema (.df) files for the target environment. In OpenEdge the following database constructs are placed in a so-called `area`:
- tables
- indexes
- LOB's

Normally these area's aren't payed much attention to in the development cycle, but on production these DO matter. What should be avoided is that table (and others) are located in the `Schema Area`, since this limits performance and capabilities severily.

The tool we are going write will be able to transform a "development .df" to the (area) structure which can be used for the target environment. This is done based on a YAML defintion file. The CLI command will be called `schemafixer` in this document we use `sf` as short for `schemafixer`.

## tech stack
- Go 1.26.0
  - use cobra/viper for parameters
    - go get github.com/spf13/viper v1.21.0
    - go get -u github.com/spf13/cobra@latest
  - use zerolog for logging 
- We use `make` for building
- output is for windows (x64 only) and linux (x64 only)
- the sources are in `cmd/schemafixer`, the default structure for Go CLI tools
- main.go drives the command. For now there's just the `apply` command. Put the logic in `apply.go`. 
- data structures in `models.go`
- logging related in `logging.go` 
- split up code in logical files 

## functional requirements
- there are subcommand's. We focus on the `apply` subcommand for now.
- `apply` takes two parameters:
  - first: the input schema (f.e. say `schema/sports2020.df`)
  - second: the rules YAML (f.e. `prod-rules.yaml`)
- output is to `stdout` unless the `-o` parameter is specified with a file name
- the `--version` option outputs the version.
- a version variable should be declared in main which can be set via the makefile.

## rules.yaml
```
schemafixer:
  version: 1.0
  defaults:
    table: DataArea
    index: IndexArea
    lob: LobArea
  tables:
    - name: customer
      area: data
      indexes:
        custnum: index1
        comments: idx2
    - name: item
      area: data
      indexes:
        itemid: index1
      lob:
        ItemImage: lob1      
```
The values in default and tables are literal values (no references).
All names are not case-sensitive.

## .df files structure
Added is an example `schema/sports2020.df` .df file.
The structure is:
ADD SEQUENCE/TABLE/FIELD/INDEX...
After this a few indented line follow with the definitions of the particular construct. The construct finishes with a blank line.
Sequences can be ignored.

### tables

The TABLE syntax is:
`ADD TABLE <table name>`
followed by:
`  AREA "<area name>"`
If for this table an explicite area name is specified in the `rules.yaml`, replace the value of `<area name>` with this area from the rules. Otherwise use the default (specified in the `.defaults.table` node).

### fields
Field belong to a table. The table name is always on the same line. Normal fields don't have an area, unless they are lob fields. This is the case if a field has an `  LOB-AREA "<area name>"`. In this case, use the area name from the rules (the field definition IN the table), otherwise use the default from `.defaults.lob`. 

### Indexes
Indexes belong to a table. An example is:
```
ADD INDEX "<index>" ON "<table>"
```
The table name is always on the same line.
Replace the `  AREA "<area name>"` with the value specfied or the default.

### defaults
If no specific rules for tables/field/indexes are found, the defaults should be used.

### others
All other lines should left alone and copied from input to output. Line endings should be what appropriate on the OS (`CRLF` on Windows, `LF` on Linux).

## error handling
If an error is found, exit 1.

## parse command
The parse command is basically the reversed of apply. 
The syntax is: `schemafixer parse <df file> <rules yaml file>`
Both files are mandatory.

The goal of this command is to make a new rules files based on an existing schema and default rules. For every table/index/field is is checked is the (LOB-)area is the default. If not, add a rule for the table/index/field. The syntax of the rules files is the same as of the apply command (in other words, you should be able to apply the newly generated rul to a .df file). The new rules file should contain the default first.

## diff command

The syntax is: `schemafixer diff <source df file> <target df file>`
Create a diff between the two df files from an AREA p.o.v.

The output should be something like:
```
CONSTRUCT       NAME              SOURCE AREA     TARGET AREA
TABLE           Customer          Data Area        CustomerArea
INDEX           Customer.CustNum  Index Area       CustIndexArea
LOB             Item.ItemImage    LOB Area         ImageArea

In the output for index/lob: the name of the construct should be prefixed with the table name.
