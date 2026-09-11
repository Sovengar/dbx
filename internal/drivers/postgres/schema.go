package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/buble/dbx/internal/ui/components/explorer"
)

type SchemaLoader struct {
	conn *pgx.Conn
}

func NewSchemaLoader(conn *pgx.Conn) *SchemaLoader {
	return &SchemaLoader{conn: conn}
}

func (s *SchemaLoader) LoadDatabase(ctx context.Context, dbName string) (*explorer.Node, error) {
	dbNode := explorer.NewNode(dbName, explorer.NodeDatabase, dbName)

	schemas, err := s.ListSchemas(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list schemas: %w", err)
	}

	for _, schema := range schemas {
		if schema == "pg_catalog" || schema == "information_schema" || schema == "pg_toast" {
			continue
		}

		schemaNode := explorer.NewNode(
			fmt.Sprintf("%s.%s", dbName, schema),
			explorer.NodeSchema,
			schema,
		)

		tables, err := s.ListTables(ctx, schema)
		if err != nil {
			continue
		}

		for _, table := range tables {
			tableNode := explorer.NewNode(
				fmt.Sprintf("%s.%s.%s", dbName, schema, table.Name),
				explorer.NodeTable,
				table.Name,
			)
			tableNode.Metadata["row_count"] = table.RowCount
			tableNode.Metadata["table_type"] = table.Type
			tableNode.Metadata["schema"] = schema

			columns, err := s.ListColumns(ctx, schema, table.Name)
			if err == nil {
				for _, col := range columns {
					colNode := explorer.NewNode(
						fmt.Sprintf("%s.%s.%s.%s", dbName, schema, table.Name, col.Name),
						explorer.NodeColumn,
						col.Name,
					)
					colNode.Metadata["data_type"] = col.DataType
					colNode.Metadata["is_nullable"] = col.IsNullable
					colNode.Metadata["column_default"] = col.Default
					tableNode.AddChild(colNode)
				}
			}

			schemaNode.AddChild(tableNode)
		}

		dbNode.AddChild(schemaNode)
	}

	return dbNode, nil
}

func (s *SchemaLoader) ListSchemas(ctx context.Context) ([]string, error) {
	query := `
		SELECT schema_name
		FROM information_schema.schemata
		WHERE schema_name NOT LIKE 'pg_%'
		ORDER BY schema_name
	`

	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		schemas = append(schemas, name)
	}

	return schemas, rows.Err()
}

type TableInfo struct {
	Name     string
	Type     string
	RowCount int
}

func (s *SchemaLoader) ListTables(ctx context.Context, schema string) ([]TableInfo, error) {
	query := `
		SELECT
			t.table_name,
			t.table_type,
			COALESCE(c.reltuples::bigint, 0) as row_count
		FROM information_schema.tables t
		LEFT JOIN pg_class c ON c.relname = t.table_name
			AND c.relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = t.table_schema)
		WHERE t.table_schema = $1
		ORDER BY t.table_name
	`

	rows, err := s.conn.Query(ctx, query, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(&t.Name, &t.Type, &t.RowCount); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}

	return tables, rows.Err()
}

type ColumnInfo struct {
	Name       string
	DataType   string
	IsNullable string
	Default    *string
}

func (s *SchemaLoader) ListColumns(ctx context.Context, schema, table string) ([]ColumnInfo, error) {
	query := `
		SELECT 
			column_name,
			data_type,
			is_nullable,
			column_default
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`

	rows, err := s.conn.Query(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []ColumnInfo
	for rows.Next() {
		var c ColumnInfo
		if err := rows.Scan(&c.Name, &c.DataType, &c.IsNullable, &c.Default); err != nil {
			return nil, err
		}
		columns = append(columns, c)
	}

	return columns, rows.Err()
}
