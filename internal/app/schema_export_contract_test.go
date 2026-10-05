package app

// Scenario: El export que ve la IA y el autocompletado se construye entero, o no vale nada.
//
// buildSchemaExportFull is the bridge between what the database reported and what the rest
// of the app reasons about: the NL→SQL prompt, the autocomplete's column list, the ERE
// diagram. If it drops a column the AI writes a query against a column that does not exist;
// if it drops a foreign key it never mentions referential integrity and happily proposes a
// DELETE that orphans rows.
//
// It is a PURE function — dbName, schemas, two maps in; an export out — and it had no test
// at all. Eighteen statements, one shape, and no reason it should ever have been left
// uncovered.

import (
	"reflect"
	"testing"

	"github.com/buble/dbx/internal/drivers/postgres"
)

// exportFixture is one schema with one table of four columns, one index and one foreign
// key, plus a second schema with no tables at all.
//
// The two maps are in the shapes the caller builds: indexes keyed by schema then table,
// foreign keys keyed by schema then table.
func exportFixture() ([]postgres.SchemaDetail,
	map[string]map[string][]postgres.IndexInfoFull,
	map[string]map[string][]postgres.ForeignKeyInfo) {

	schemas := []postgres.SchemaDetail{
		{
			Name: "public",
			Tables: []postgres.TableDetail{{
				Name:     "orders",
				Type:     "BASE TABLE",
				RowCount: 1234,
				Columns: []postgres.ColumnInfo{
					{Name: "id", DataType: "integer", IsNullable: "NO"},
					{Name: "user_id", DataType: "integer", IsNullable: "NO"},
					{Name: "total", DataType: "numeric", IsNullable: "YES"},
					{Name: "note", DataType: "text", IsNullable: ""},
				},
			}},
		},
		{Name: "empty_schema"},
	}

	indexes := map[string]map[string][]postgres.IndexInfoFull{
		"public": {
			"orders": {
				{Name: "orders_pkey", Columns: []string{"id"}, IsUnique: true, IsPrimary: true},
			},
		},
	}

	fks := map[string]map[string][]postgres.ForeignKeyInfo{
		"public": {
			"orders": {{
				Name:      "orders_user_id_fkey",
				Column:    "user_id",
				RefSchema: "public",
				RefTable:  "users",
				RefColumn: "id",
			}},
		},
	}

	return schemas, indexes, fks
}

func TestBuildSchemaExportFullCarriesEverythingThrough(t *testing.T) {
	schemas, indexes, fksByTable := exportFixture()

	export := buildSchemaExportFull("shopdb", schemas, indexes, fksByTable)

	if export.Database != "shopdb" {
		t.Errorf("the database is %q, want shopdb", export.Database)
	}
	if len(export.Schemas) != 2 {
		t.Fatalf("the export has %d schemas, want both — including the empty one, which is how the user learns the schema exists", len(export.Schemas))
	}
	if export.Schemas[1].Name != "empty_schema" {
		t.Errorf("the second schema is %q", export.Schemas[1].Name)
	}

	pub := export.Schemas[0]
	if len(pub.Tables) != 1 {
		t.Fatalf("public has %d tables", len(pub.Tables))
	}
	tbl := pub.Tables[0]

	t.Run("the table keeps what the loader reported", func(t *testing.T) {
		if tbl.Name != "orders" || tbl.Type != "BASE TABLE" || tbl.RowCount != 1234 {
			t.Errorf("the table is %+v, want orders / BASE TABLE / 1234", tbl)
		}
	})

	t.Run("every column survives, with its type", func(t *testing.T) {
		if len(tbl.Columns) != 4 {
			t.Fatalf("the table has %d columns, want 4", len(tbl.Columns))
		}
		for i, want := range []struct{ name, typ string }{
			{"id", "integer"}, {"user_id", "integer"}, {"total", "numeric"}, {"note", "text"},
		} {
			if tbl.Columns[i].Name != want.name || tbl.Columns[i].DataType != want.typ {
				t.Errorf("column %d is %+v, want %s %s", i, tbl.Columns[i], want.name, want.typ)
			}
		}
	})

	t.Run("nullability comes from the loader's YES and nothing else", func(t *testing.T) {
		// PostgreSQL answers "YES" or "NO", and the conversion is an exact comparison.
		// Pinned because a loose one would mark a NOT NULL column nullable — which
		// reads to the AI as "this may be null, wrap it in IS NULL".
		want := []bool{false, false, true, false}
		for i, w := range want {
			if got := tbl.Columns[i].IsNullable; got != w {
				t.Errorf("column %q says nullable=%t, want %t", tbl.Columns[i].Name, got, w)
			}
		}
	})

	t.Run("the default value is carried", func(t *testing.T) {
		// ColumnInfo.Default is a *string, so "has a default" and "does not have one"
		// are different shapes — and the nil case is the ordinary one.
		yes := "now()"
		withDefault := []postgres.SchemaDetail{{
			Name: "public",
			Tables: []postgres.TableDetail{{
				Name: "t",
				Columns: []postgres.ColumnInfo{
					{Name: "created", DataType: "timestamptz", Default: &yes},
					{Name: "plain", DataType: "text"},
				},
			}},
		}}
		got := buildSchemaExportFull("db", withDefault, nil, nil)
		if len(got.Schemas) != 1 || len(got.Schemas[0].Tables) != 1 {
			t.Fatalf("the export is %+v", got)
		}
		cols := got.Schemas[0].Tables[0].Columns
		if len(cols) != 2 {
			t.Fatalf("the table has %d columns", len(cols))
		}
		// DefaultValue is a *string on both sides, so the pointer is passed through and
		// "no default" stays distinguishable from "the default is the empty string" —
		// which matters, because `DEFAULT ''` is a real and different thing.
		if cols[0].DefaultValue == nil || *cols[0].DefaultValue != "now()" {
			t.Errorf("the default was not carried: %+v", cols[0].DefaultValue)
		}
		if cols[1].DefaultValue != nil {
			t.Errorf("a column with no default has %q", *cols[1].DefaultValue)
		}
	})

	t.Run("the index reaches the table it belongs to", func(t *testing.T) {
		if len(tbl.Indexes) != 1 {
			t.Fatalf("the table has %d indexes, want 1", len(tbl.Indexes))
		}
		idx := tbl.Indexes[0]
		if idx.Name != "orders_pkey" || !idx.IsUnique || !idx.IsPrimary {
			t.Errorf("the index is %+v", idx)
		}
		if len(idx.Columns) != 1 || idx.Columns[0] != "id" {
			t.Errorf("the index columns are %v", idx.Columns)
		}
	})

	t.Run("the foreign key reaches the table it belongs to", func(t *testing.T) {
		if len(tbl.FKs) != 1 {
			t.Fatalf("the table has %d foreign keys, want 1", len(tbl.FKs))
		}
		fk := tbl.FKs[0]
		if fk.Name != "orders_user_id_fkey" || fk.RefTable != "users" || fk.RefSchema != "public" {
			t.Errorf("the foreign key is %+v", fk)
		}
		if fk.Columns != "user_id" || fk.RefColumns != "id" {
			t.Errorf("the foreign key columns are %q -> %q", fk.Columns, fk.RefColumns)
		}
	})

	t.Run("an index or key on ANOTHER table does not leak onto this one", func(t *testing.T) {
		// The maps are keyed by table name, and the lookup is by `td.Name`. A bug that
		// used the schema's first entry instead would give every table in a schema the
		// first table's index — which the AI would then quote as if it belonged here.
		schemas := []postgres.SchemaDetail{{
			Name: "public",
			Tables: []postgres.TableDetail{
				{Name: "a", Columns: []postgres.ColumnInfo{{Name: "id"}}},
				{Name: "b", Columns: []postgres.ColumnInfo{{Name: "id"}}},
			},
		}}
		indexes := map[string]map[string][]postgres.IndexInfoFull{
			"public": {"a": {{Name: "a_pkey", IsPrimary: true}}},
		}
		fks := map[string]map[string][]postgres.ForeignKeyInfo{
			"public": {"a": {{Name: "a_fk", Column: "id", RefTable: "z"}}},
		}

		got := buildSchemaExportFull("db", schemas, indexes, fks)
		a := got.Schemas[0].Tables[0]
		b := got.Schemas[0].Tables[1]
		if len(a.Indexes) != 1 || a.Indexes[0].Name != "a_pkey" {
			t.Errorf("table a has indexes %+v", a.Indexes)
		}
		if len(b.Indexes) != 0 {
			t.Errorf("table b has indexes %+v, want none", b.Indexes)
		}
		if len(b.FKs) != 0 {
			t.Errorf("table b has foreign keys %+v, want none", b.FKs)
		}
	})
}

func TestBuildSchemaExportFullWithNothingToAdd(t *testing.T) {
	// The nil-map cases, which is what the function sees when a schema load failed
	// partway: the loader returns an error and the code stores nil for that schema. The
	// export then has tables with no indexes and no keys, which must not become an error
	// and must not become an empty non-nil slice pretending to be data.
	t.Run("nil maps produce tables with no indexes and no keys", func(t *testing.T) {
		schemas := []postgres.SchemaDetail{{
			Name:   "public",
			Tables: []postgres.TableDetail{{Name: "t", Columns: []postgres.ColumnInfo{{Name: "id"}}}},
		}}
		got := buildSchemaExportFull("db", schemas, nil, nil)

		if len(got.Schemas) != 1 || len(got.Schemas[0].Tables) != 1 {
			t.Fatalf("the export is %+v", got)
		}
		tbl := got.Schemas[0].Tables[0]
		if len(tbl.Indexes) != 0 {
			t.Errorf("the table has indexes %+v from a nil map", tbl.Indexes)
		}
		if len(tbl.FKs) != 0 {
			t.Errorf("the table has foreign keys %+v from a nil map", tbl.FKs)
		}
	})

	t.Run("a map with no entry for the SCHEMA is the same as a nil map", func(t *testing.T) {
		// The maps are indexed by schema name, so an entry for a different schema does
		// not apply — and a schema name that differs only in case must not match, since
		// these names come from the loader as PostgreSQL wrote them.
		schemas := []postgres.SchemaDetail{{
			Name:   "public",
			Tables: []postgres.TableDetail{{Name: "t"}},
		}}
		indexes := map[string]map[string][]postgres.IndexInfoFull{
			"PUBLIC": {"t": {{Name: "t_pkey"}}},
		}
		got := buildSchemaExportFull("db", schemas, indexes, nil)
		if n := len(got.Schemas[0].Tables[0].Indexes); n != 0 {
			t.Errorf("the table has %d indexes from another schema's map", n)
		}
	})

	t.Run("no schemas at all is an empty export, not a nil one", func(t *testing.T) {
		got := buildSchemaExportFull("db", nil, nil, nil)
		if got == nil {
			t.Fatal("the export is nil; every caller dereferences it")
		}
		if got.Database != "db" {
			t.Errorf("the database is %q", got.Database)
		}
		if len(got.Schemas) != 0 {
			t.Errorf("the export has %d schemas", len(got.Schemas))
		}
	})

	t.Run("a table with no columns still appears", func(t *testing.T) {
		// A view with no readable columns, or a table the user cannot select from. It
		// still exists and the AI should be able to name it.
		schemas := []postgres.SchemaDetail{{
			Name:   "public",
			Tables: []postgres.TableDetail{{Name: "empty_table"}},
		}}
		got := buildSchemaExportFull("db", schemas, nil, nil)
		if len(got.Schemas[0].Tables) != 1 {
			t.Fatalf("the schema has %d tables", len(got.Schemas[0].Tables))
		}
		if got.Schemas[0].Tables[0].Name != "empty_table" {
			t.Errorf("the table is %q", got.Schemas[0].Tables[0].Name)
		}
	})
}

// Scenario: El driver NO tiene una segunda fuente de verdad para indexes y claves.
//
// TableDetail carried Indexes and FKs fields alongside the Columns it actually filled, and
// nothing ever wrote or read either one — the export took indexes and keys from separate
// maps, keyed by schema and table. So the struct advertised a source of truth that was
// always empty.
//
// That is the same shape as fkRefTable, which the sidebar stored and never rendered, and
// as the six config keys that were declared with a default and read by nothing. The trap is
// identical: a field that looks like where the data lives, and does not. The fields are
// gone, and the test below fails if they come back and start being populated.
//
// If they are ever reintroduced it has to be with a decision about which one is canonical,
// not as an extra place for a caller to fill in.
func TestTableDetailHasNoSecondSourceOfIndexesOrKeys(t *testing.T) {
	// Compile-time check by reflection: the fields must not exist. A test that reads
	// them by name would be the wrong shape — it would start failing to COMPILE, which is
	// a louder and better signal, but only for the file that mentions them. This asserts
	// the absence in one place instead.
	td := reflect.TypeOf(postgres.TableDetail{})
	for _, name := range []string{"Indexes", "FKs"} {
		if _, ok := td.FieldByName(name); ok {
			t.Errorf("postgres.TableDetail has a %s field again; if it is populated it must be the one the export reads, or it must go", name)
		}
	}
}
