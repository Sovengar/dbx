package context

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type SchemaExport struct {
	Database string         `json:"database"`
	Schemas  []SchemaInfo   `json:"schemas"`
}

type SchemaInfo struct {
	Name   string      `json:"name"`
	Tables []TableInfo `json:"tables"`
}

type TableInfo struct {
	Name      string       `json:"name"`
	Type      string       `json:"type"`
	RowCount  int64        `json:"row_count"`
	Columns   []ColumnInfo `json:"columns"`
	Indexes   []IndexInfo  `json:"indexes,omitempty"`
	FKs       []FKInfo     `json:"foreign_keys,omitempty"`
}

type ColumnInfo struct {
	Name         string  `json:"name"`
	DataType     string  `json:"data_type"`
	IsNullable   bool    `json:"is_nullable"`
	DefaultValue *string `json:"default_value,omitempty"`
	OrdinalPos   int     `json:"ordinal_position"`
}

type IndexInfo struct {
	Name     string   `json:"name"`
	Columns  []string `json:"columns"`
	IsUnique bool     `json:"is_unique"`
	IsPrimary bool    `json:"is_primary"`
}

type FKInfo struct {
	Name            string `json:"name"`
	Columns         string `json:"columns"`
	RefSchema       string `json:"ref_schema"`
	RefTable        string `json:"ref_table"`
	RefColumns      string `json:"ref_columns"`
}

func ExportSchema(ctx context.Context, conn *pgx.Conn, dbName string) (*SchemaExport, error) {
	export := &SchemaExport{Database: dbName}

	schemas, err := listSchemas(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("failed to list schemas: %w", err)
	}

	for _, schemaName := range schemas {
		if schemaName == "pg_catalog" || schemaName == "information_schema" || schemaName == "pg_toast" {
			continue
		}

		si := SchemaInfo{Name: schemaName}

		tables, err := listTables(ctx, conn, schemaName)
		if err != nil {
			continue
		}

		for _, t := range tables {
			ti := TableInfo{
				Name:     t.Name,
				Type:     t.Type,
				RowCount: t.RowCount,
			}

			ti.Columns, _ = listColumns(ctx, conn, schemaName, t.Name)
			ti.Indexes, _ = listIndexes(ctx, conn, schemaName, t.Name)
			ti.FKs, _ = listFKs(ctx, conn, schemaName, t.Name)

			si.Tables = append(si.Tables, ti)
		}

		export.Schemas = append(export.Schemas, si)
	}

	return export, nil
}

func ExportSchemaString(ctx context.Context, conn *pgx.Conn, dbName string) (string, error) {
	export, err := ExportSchema(ctx, conn, dbName)
	if err != nil {
		return "", err
	}

	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return "", err
	}

	return string(data), nil
}

type tableRow struct {
	Name     string
	Type     string
	RowCount int64
}

func listSchemas(ctx context.Context, conn *pgx.Conn) ([]string, error) {
	query := `
		SELECT schema_name
		FROM information_schema.schemata
		WHERE schema_name NOT LIKE 'pg_%'
		ORDER BY schema_name
	`
	rows, err := conn.Query(ctx, query)
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

func listTables(ctx context.Context, conn *pgx.Conn, schema string) ([]tableRow, error) {
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
	rows, err := conn.Query(ctx, query, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []tableRow
	for rows.Next() {
		var t tableRow
		if err := rows.Scan(&t.Name, &t.Type, &t.RowCount); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

func listColumns(ctx context.Context, conn *pgx.Conn, schema, table string) ([]ColumnInfo, error) {
	query := `
		SELECT
			column_name,
			data_type,
			is_nullable,
			column_default,
			ordinal_position
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`
	rows, err := conn.Query(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []ColumnInfo
	for rows.Next() {
		var c ColumnInfo
		var nullable string
		if err := rows.Scan(&c.Name, &c.DataType, &nullable, &c.DefaultValue, &c.OrdinalPos); err != nil {
			return nil, err
		}
		c.IsNullable = nullable == "YES"
		columns = append(columns, c)
	}
	return columns, rows.Err()
}

func listIndexes(ctx context.Context, conn *pgx.Conn, schema, table string) ([]IndexInfo, error) {
	query := `
		SELECT
			i.relname as index_name,
			am.amname as index_type,
			pg_get_indexdef(i.oid) as index_def,
			ix.indisunique as is_unique,
			ix.indisprimary as is_primary,
			array_agg(a.attname ORDER BY array_position(ix.indkey, a.attnum)) as columns
		FROM pg_class t
		JOIN pg_namespace ns ON ns.oid = t.relnamespace
		JOIN pg_index ix ON ix.indrelid = t.oid
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_am am ON am.oid = i.relam
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(ix.indkey)
		WHERE ns.nspname = $1 AND t.relname = $2
		GROUP BY i.relname, am.amname, pg_get_indexdef(i.oid), ix.indisunique, ix.indisprimary
		ORDER BY i.relname
	`
	rows, err := conn.Query(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var indexes []IndexInfo
	for rows.Next() {
		var idx IndexInfo
		if err := rows.Scan(&idx.Name, &struct{}{}, &struct{}{}, &idx.IsUnique, &idx.IsPrimary, &idx.Columns); err != nil {
			return nil, err
		}
		indexes = append(indexes, idx)
	}
	return indexes, rows.Err()
}

func listFKs(ctx context.Context, conn *pgx.Conn, schema, table string) ([]FKInfo, error) {
	query := `
		SELECT
			tc.constraint_name,
			kcu.column_name,
			ccu.table_schema as ref_schema,
			ccu.table_name as ref_table,
			ccu.column_name as ref_column
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
			ON tc.constraint_name = kcu.constraint_name
			AND tc.table_schema = kcu.table_schema
		JOIN information_schema.constraint_column_usage ccu
			ON tc.constraint_name = ccu.constraint_name
			AND tc.table_schema = ccu.table_schema
		WHERE tc.constraint_type = 'FOREIGN KEY'
			AND tc.table_schema = $1
			AND tc.table_name = $2
	`
	rows, err := conn.Query(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fks []FKInfo
	for rows.Next() {
		var fk FKInfo
		if err := rows.Scan(&fk.Name, &fk.Columns, &fk.RefSchema, &fk.RefTable, &fk.RefColumns); err != nil {
			return nil, err
		}
		fks = append(fks, fk)
	}
	return fks, rows.Err()
}
