package commands

// RulesFile is the top-level structure of the rules YAML.
type RulesFile struct {
	SchemaFixer SchemaFixerRules `yaml:"schemafixer"`
}

// SchemaFixerRules contains the full fixer configuration.
type SchemaFixerRules struct {
	Version  float64      `yaml:"version"`
	Defaults AreaDefaults `yaml:"defaults"`
	Tables   []TableRule  `yaml:"tables"`
}

// AreaDefaults holds the fallback area names used when no explicit rule matches.
type AreaDefaults struct {
	Table string `yaml:"table"`
	Index string `yaml:"index"`
	Lob   string `yaml:"lob"`
}

// TableRule holds per-table area overrides for the table itself, its indexes and its LOB fields.
type TableRule struct {
	Name    string            `yaml:"name"`
	Area    string            `yaml:"area"`
	Indexes map[string]string `yaml:"indexes"`
	Lob     map[string]string `yaml:"lob"`
}
