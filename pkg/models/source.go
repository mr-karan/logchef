package models

import "time"

type Source struct {
    ID           string    `db:"id"`
    Name         string    `db:"name"`
    TableName    string    `db:"table_name"`
    SchemaType   string    `db:"schema_type"` // "http" or "app"
    Description  string    `db:"description"`
    DSN          string    `db:"dsn"`
    CreatedAt    time.Time `db:"created_at"`
    UpdatedAt    time.Time `db:"updated_at"`
}
