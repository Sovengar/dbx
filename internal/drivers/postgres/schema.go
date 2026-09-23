package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/buble/dbx/internal/ui/components/explorer"
)

type SchemaLoader struct {
	conn *pgx.Conn
}

func NewSchemaLoader(conn *pgx.Conn) *SchemaLoader {
	return &SchemaLoader{conn: conn}
}

type TableDetail struct {
	Name     string
	Type     string
	RowCount int
	Columns  []ColumnInfo
	Indexes  []IndexInfoFull
	FKs      []ForeignKeyInfo
}

type SchemaDetail struct {
	Name   string
	Tables []TableDetail
}

// SchemaText renders schema details in the compact text format used to build
// LLM prompts. It is shared by the CLI (`dbx ask`) and the TUI ASK pane.
func SchemaText(schemas []SchemaDetail) string {
	var b strings.Builder
	for _, sd := range schemas {
		b.WriteString(fmt.Sprintf("Schema: %s\n", sd.Name))
		for _, table := range sd.Tables {
			b.WriteString(fmt.Sprintf("  Table: %s (%d rows)\n", table.Name, table.RowCount))
			for _, col := range table.Columns {
				nullable := ""
				if col.IsNullable == "YES" {
					nullable = " NULL"
				}
				b.WriteString(fmt.Sprintf("    %s %s%s\n", col.Name, col.DataType, nullable))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

type LoadDatabaseResult struct {
	Root    *explorer.Node
	Schemas []SchemaDetail
}

func (s *SchemaLoader) LoadDatabase(ctx context.Context, dbName string) (*LoadDatabaseResult, error) {
	dbNode := explorer.NewNode(dbName, explorer.NodeDatabase, dbName)
	dbNode.Expanded = true

	schemas, err := s.ListSchemas(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list schemas: %w", err)
	}

	var allSchemas []SchemaDetail

	for _, schema := range schemas {
		if schema == "pg_catalog" || schema == "information_schema" || schema == "pg_toast" {
			continue
		}

		schemaNode := explorer.NewNode(
			fmt.Sprintf("%s.%s", dbName, schema),
			explorer.NodeSchema,
			schema,
		)
		schemaNode.Expanded = false

		tables, err := s.ListTables(ctx, schema)
		if err != nil {
			continue
		}

		columnsByTable, _ := s.ListColumnsBySchema(ctx, schema)

		sd := SchemaDetail{Name: schema}

		for _, table := range tables {
			tableNode := explorer.NewNode(
				fmt.Sprintf("%s.%s.%s", dbName, schema, table.Name),
				explorer.NodeTable,
				table.Name,
			)
			tableNode.Metadata["row_count"] = table.RowCount
			tableNode.Metadata["table_type"] = table.Type
			tableNode.Metadata["schema"] = schema

			td := TableDetail{
				Name:     table.Name,
				Type:     table.Type,
				RowCount: table.RowCount,
			}

			if columns, ok := columnsByTable[table.Name]; ok {
				td.Columns = columns
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

			sd.Tables = append(sd.Tables, td)
			schemaNode.AddChild(tableNode)
		}

		allSchemas = append(allSchemas, sd)
		dbNode.AddChild(schemaNode)
	}

	return &LoadDatabaseResult{Root: dbNode, Schemas: allSchemas}, nil
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

type ConstraintInfo struct {
	Name    string
	Type    string // PRIMARY KEY, UNIQUE, CHECK, FOREIGN KEY
	Columns string // "col1, col2"
}

type ForeignKeyInfo struct {
	Name      string
	Column    string
	RefSchema string
	RefTable  string
	RefColumn string
}

type IndexInfo struct {
	Name    string
	Columns string
	Unique  bool
	Def     string
}

func (s *SchemaLoader) ListColumnsBySchema(ctx context.Context, schema string) (map[string][]ColumnInfo, error) {
	query := `
		SELECT
			table_name,
			column_name,
			data_type,
			is_nullable,
			column_default
		FROM information_schema.columns
		WHERE table_schema = $1
		ORDER BY table_name, ordinal_position
	`
	rows, err := s.conn.Query(ctx, query, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]ColumnInfo)
	for rows.Next() {
		var tableName, name, dataType, isNullable string
		var def *string
		if err := rows.Scan(&tableName, &name, &dataType, &isNullable, &def); err != nil {
			return nil, err
		}
		result[tableName] = append(result[tableName], ColumnInfo{
			Name:       name,
			DataType:   dataType,
			IsNullable: isNullable,
			Default:    def,
		})
	}
	return result, rows.Err()
}

func (s *SchemaLoader) ListIndexesFullBySchema(ctx context.Context, schema string) (map[string][]IndexInfoFull, error) {
	query := `
		SELECT
			t.relname as table_name,
			i.relname as index_name,
			ix.indisunique as is_unique,
			ix.indisprimary as is_primary,
			array_agg(a.attname ORDER BY array_position(ix.indkey, a.attnum)) as columns
		FROM pg_class t
		JOIN pg_namespace ns ON ns.oid = t.relnamespace
		JOIN pg_index ix ON ix.indrelid = t.oid
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(ix.indkey)
		WHERE ns.nspname = $1 AND t.relkind = 'r'
		GROUP BY t.relname, i.relname, ix.indisunique, ix.indisprimary
		ORDER BY t.relname, i.relname
	`
	rows, err := s.conn.Query(ctx, query, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]IndexInfoFull)
	for rows.Next() {
		var tableName, name string
		var isUnique, isPrimary bool
		var columns []string
		if err := rows.Scan(&tableName, &name, &isUnique, &isPrimary, &columns); err != nil {
			return nil, err
		}
		result[tableName] = append(result[tableName], IndexInfoFull{
			Name:      name,
			Columns:   columns,
			IsUnique:  isUnique,
			IsPrimary: isPrimary,
		})
	}
	return result, rows.Err()
}

func (s *SchemaLoader) ListForeignKeysBySchema(ctx context.Context, schema string) (map[string][]ForeignKeyInfo, error) {
	query := `
		SELECT
			tc.table_name,
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
		ORDER BY tc.table_name, tc.constraint_name
	`
	rows, err := s.conn.Query(ctx, query, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]ForeignKeyInfo)
	for rows.Next() {
		var tableName, name, column, refSchema, refTable, refColumn string
		if err := rows.Scan(&tableName, &name, &column, &refSchema, &refTable, &refColumn); err != nil {
			return nil, err
		}
		result[tableName] = append(result[tableName], ForeignKeyInfo{
			Name:      name,
			Column:    column,
			RefSchema: refSchema,
			RefTable:  refTable,
			RefColumn: refColumn,
		})
	}
	return result, rows.Err()
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

func (s *SchemaLoader) ListConstraints(ctx context.Context, schema, table string) ([]ConstraintInfo, error) {
	query := `
		SELECT tc.constraint_name, tc.constraint_type,
			string_agg(DISTINCT kcu.column_name, ', ' ORDER BY kcu.column_name)
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
			ON tc.constraint_name = kcu.constraint_name
			AND tc.table_schema = kcu.table_schema
		WHERE tc.table_schema = $1 AND tc.table_name = $2
		GROUP BY tc.constraint_name, tc.constraint_type
		ORDER BY tc.constraint_name
	`

	rows, err := s.conn.Query(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var constraints []ConstraintInfo
	for rows.Next() {
		var c ConstraintInfo
		if err := rows.Scan(&c.Name, &c.Type, &c.Columns); err != nil {
			return nil, err
		}
		constraints = append(constraints, c)
	}

	return constraints, rows.Err()
}

func (s *SchemaLoader) ListForeignKeys(ctx context.Context, schema, table string) ([]ForeignKeyInfo, error) {
	query := `
		SELECT
			tc.constraint_name,
			kcu.column_name,
			ccu.table_schema AS ref_schema,
			ccu.table_name AS ref_table,
			ccu.column_name AS ref_column
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
			ON tc.constraint_name = kcu.constraint_name
			AND tc.table_schema = kcu.table_schema
		JOIN information_schema.constraint_column_usage ccu
			ON tc.constraint_name = ccu.constraint_name
			AND tc.table_schema = ccu.table_schema
		WHERE tc.constraint_type = 'FOREIGN KEY'
			AND tc.table_schema = $1 AND tc.table_name = $2
		ORDER BY tc.constraint_name, kcu.ordinal_position
	`

	rows, err := s.conn.Query(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fks []ForeignKeyInfo
	for rows.Next() {
		var fk ForeignKeyInfo
		if err := rows.Scan(&fk.Name, &fk.Column, &fk.RefSchema, &fk.RefTable, &fk.RefColumn); err != nil {
			return nil, err
		}
		fks = append(fks, fk)
	}

	return fks, rows.Err()
}

type IndexInfoFull struct {
	Name      string
	Columns   []string
	IsUnique  bool
	IsPrimary bool
}

func (s *SchemaLoader) ListIndexesFull(ctx context.Context, schema, table string) ([]IndexInfoFull, error) {
	query := `
		SELECT
			i.relname as index_name,
			ix.indisunique as is_unique,
			ix.indisprimary as is_primary,
			array_agg(a.attname ORDER BY array_position(ix.indkey, a.attnum)) as columns
		FROM pg_class t
		JOIN pg_namespace ns ON ns.oid = t.relnamespace
		JOIN pg_index ix ON ix.indrelid = t.oid
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(ix.indkey)
		WHERE ns.nspname = $1 AND t.relname = $2
		GROUP BY i.relname, ix.indisunique, ix.indisprimary
		ORDER BY i.relname
	`
	rows, err := s.conn.Query(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var indexes []IndexInfoFull
	for rows.Next() {
		var idx IndexInfoFull
		if err := rows.Scan(&idx.Name, &idx.IsUnique, &idx.IsPrimary, &idx.Columns); err != nil {
			return nil, err
		}
		indexes = append(indexes, idx)
	}
	return indexes, rows.Err()
}

func (s *SchemaLoader) ListIndexes(ctx context.Context, schema, table string) ([]IndexInfo, error) {
	query := `
		SELECT indexname, indexdef
		FROM pg_indexes
		WHERE schemaname = $1 AND tablename = $2
		ORDER BY indexname
	`

	rows, err := s.conn.Query(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var indexes []IndexInfo
	for rows.Next() {
		var idx IndexInfo
		var def string
		if err := rows.Scan(&idx.Name, &def); err != nil {
			return nil, err
		}
		idx.Def = def
		idx.Unique = false
		if len(def) > 0 {
			idx.Unique = strings.Contains(def, "UNIQUE")
		}
		indexes = append(indexes, idx)
	}

	return indexes, rows.Err()
}

type TableOverview struct {
	TableName       string
	TableType       string
	Comment         *string
	TotalSize       string
	TableSize       string
	IndexSize       string
	LiveTuples      int64
	DeadTuples      int64
	LastVacuum      *time.Time
	LastAutovacuum  *time.Time
	LastAnalyze     *time.Time
	LastAutoanalyze *time.Time
	ColumnCount     int
	ConstraintCount int
	IndexCount      int
	FKCount         int
}

func (s *SchemaLoader) GetTableOverview(ctx context.Context, schema, table string) (*TableOverview, error) {
	overview := &TableOverview{
		TableName: table,
	}

	// Table type and comment
	err := s.conn.QueryRow(ctx, `
		SELECT
			t.table_type,
			obj_description(c.oid) as comment
		FROM information_schema.tables t
		LEFT JOIN pg_class c ON c.relname = t.table_name
			AND c.relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = t.table_schema)
		WHERE t.table_schema = $1 AND t.table_name = $2
	`, schema, table).Scan(&overview.TableType, &overview.Comment)
	if err != nil {
		return nil, fmt.Errorf("failed to get table type: %w", err)
	}

	// Sizes
	err = s.conn.QueryRow(ctx, `
		SELECT
			pg_size_pretty(pg_total_relation_size(c.oid)),
			pg_size_pretty(pg_table_size(c.oid)),
			pg_size_pretty(pg_indexes_size(c.oid))
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2
	`, schema, table).Scan(&overview.TotalSize, &overview.TableSize, &overview.IndexSize)
	if err != nil {
		return nil, fmt.Errorf("failed to get table sizes: %w", err)
	}

	// Stats
	err = s.conn.QueryRow(ctx, `
		SELECT
			COALESCE(n_live_tup, 0),
			COALESCE(n_dead_tup, 0),
			last_vacuum,
			last_autovacuum,
			last_analyze,
			last_autoanalyze
		FROM pg_stat_user_tables
		WHERE schemaname = $1 AND relname = $2
	`, schema, table).Scan(
		&overview.LiveTuples, &overview.DeadTuples,
		&overview.LastVacuum, &overview.LastAutovacuum,
		&overview.LastAnalyze, &overview.LastAutoanalyze,
	)
	if err != nil {
		// Stats not available (e.g. system tables), continue with defaults
		overview.LiveTuples = -1
	}

	// Counts
	err = s.conn.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2),
			(SELECT count(*) FROM information_schema.table_constraints WHERE table_schema = $1 AND table_name = $2),
			(SELECT count(*) FROM pg_indexes WHERE schemaname = $1 AND tablename = $2),
			(SELECT count(*) FROM information_schema.table_constraints tc
				WHERE tc.constraint_type = 'FOREIGN KEY' AND tc.table_schema = $1 AND tc.table_name = $2)
	`, schema, table).Scan(
		&overview.ColumnCount, &overview.ConstraintCount,
		&overview.IndexCount, &overview.FKCount,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get counts: %w", err)
	}

	return overview, nil
}
