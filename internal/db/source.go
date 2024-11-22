package db

import (
	"database/sql"
	"time"

	"github.com/mr-karan/logchef/internal/models"
	"github.com/segmentio/ksuid"
)

type SourceRepository struct {
	db *sql.DB
}

func NewSourceRepository(db *sql.DB) *SourceRepository {
	return &SourceRepository{db: db}
}

func (r *SourceRepository) Create(source *models.Source) error {
	source.ID = ksuid.New().String()
	source.CreatedAt = time.Now()
	source.UpdatedAt = time.Now()

	query := `
        INSERT INTO sources (id, name, table_name, schema_type, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?)
    `

	_, err := r.db.Exec(query,
		source.ID,
		source.Name,
		source.TableName,
		source.SchemaType,
		source.CreatedAt,
		source.UpdatedAt,
	)

	return err
}

func (r *SourceRepository) GetByID(id string) (*models.Source, error) {
	var source models.Source

	query := `
        SELECT id, name, table_name, schema_type, created_at, updated_at
        FROM sources WHERE id = ?
    `

	err := r.db.QueryRow(query, id).Scan(
		&source.ID,
		&source.Name,
		&source.TableName,
		&source.SchemaType,
		&source.CreatedAt,
		&source.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &source, nil
}

func (r *SourceRepository) List() ([]*models.Source, error) {
	query := `
        SELECT id, name, table_name, schema_type, created_at, updated_at
        FROM sources ORDER BY created_at DESC
    `

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []*models.Source
	for rows.Next() {
		var source models.Source
		err := rows.Scan(
			&source.ID,
			&source.Name,
			&source.TableName,
			&source.SchemaType,
			&source.CreatedAt,
			&source.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		sources = append(sources, &source)
	}

	return sources, nil
}
