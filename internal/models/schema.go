package models

// LogSchema represents the schema of a log field
type LogSchema struct {
	Name     string      `json:"name"`
	Type     string      `json:"type"`
	Path     []string    `json:"path"`
	IsNested bool        `json:"is_nested"`
	Parent   string      `json:"parent,omitempty"`
	Children []LogSchema `json:"children,omitempty"`
}

// LogSchemaParams represents parameters for schema analysis
type LogSchemaParams struct {
	StartTime string
	EndTime   string
	TableName string
}
