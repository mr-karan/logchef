package models

import (
    "database/sql"
    "time"
)

type SourceRepository struct {
    db *sql.DB
}

func NewSourceRepository(db *sql.DB) *SourceRepository {
    return &SourceRepository{db: db}
}

func (r *SourceRepository) Create(source *Source) error {
    query := `
        INSERT INTO sources (id, name, table_name, schema_type, dsn, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?)
    `
    now := time.Now()
    source.CreatedAt = now
    source.UpdatedAt = now
    
    _, err := r.db.Exec(query, source.ID, source.Name, source.TableName, 
        source.SchemaType, source.DSN, source.CreatedAt, source.UpdatedAt)
    return err
}

func (r *SourceRepository) GetByName(name string) (*Source, error) {
    var source Source
    err := r.db.QueryRow("SELECT id, name, table_name, schema_type, dsn, created_at, updated_at FROM sources WHERE name = ?", name).Scan(
        &source.ID, &source.Name, &source.TableName,
        &source.SchemaType, &source.DSN, &source.CreatedAt, &source.UpdatedAt)
    if err != nil {
        return nil, err
    }
    return &source, nil
}

func (r *SourceRepository) Get(id string) (*Source, error) {
    var source Source
    err := r.db.QueryRow("SELECT id, name, table_name, schema_type, dsn, created_at, updated_at FROM sources WHERE id = ?", id).Scan(
        &source.ID, &source.Name, &source.TableName, 
        &source.SchemaType, &source.DSN, &source.CreatedAt, &source.UpdatedAt)
    if err != nil {
        return nil, err
    }
    return &source, nil
}

func (r *SourceRepository) Update(source *Source) error {
    query := `
        UPDATE sources 
        SET name = ?, schema_type = ?, updated_at = ?
        WHERE id = ?
    `
    source.UpdatedAt = time.Now()
    _, err := r.db.Exec(query, source.Name, source.SchemaType, 
        source.UpdatedAt, source.ID)
    return err
}

func (r *SourceRepository) Delete(id string) error {
    _, err := r.db.Exec("DELETE FROM sources WHERE id = ?", id)
    return err
}

func (r *SourceRepository) List() ([]Source, error) {
    rows, err := r.db.Query("SELECT id, name, table_name, schema_type, dsn, created_at, updated_at FROM sources ORDER BY created_at DESC")
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var sources []Source
    for rows.Next() {
        var source Source
        err := rows.Scan(
            &source.ID, &source.Name, &source.TableName,
            &source.SchemaType, &source.DSN, &source.CreatedAt, &source.UpdatedAt)
        if err != nil {
            return nil, err
        }
        sources = append(sources, source)
    }
    return sources, rows.Err()
}
