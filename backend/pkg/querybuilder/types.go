package querybuilder

// Query represents a SQL query with its parameters
type Query struct {
	SQL  string
	Args []interface{}
}

// Builder is the interface implemented by query builders
type Builder interface {
	Build() (*Query, error)
}

// Options represents raw SQL query options
type Options struct {
	TableName string
	RawSQL    string
	Limit     int
}
