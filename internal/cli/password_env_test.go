package cli

// Scenario: `password_env` de verdad saca la contraseña de la variable de entorno.
//
// The knob was declared in the config struct, given no default, and read by nothing. The
// documented way to keep a password out of config.toml therefore did nothing at all, and
// the failure was silent in the worst way: a user who set `password_env = "PGPASSWORD"`
// and dropped the password from their URL simply could not connect, with nothing anywhere
// saying the knob was unsupported.
//
// The sibling mechanism already worked and set the precedent — project .dbx.toml files
// expand `${env:NAME}` in their DSN — so this is the global config learning the
// convention the project files already had.
//
// What makes it worth a contract test is the shape of a DSN. "Does this DSN have a
// password" is not one question: a DSN may have no userinfo at all, may have a bare user,
// may have user:password, and may carry a port. A check that only looks for ':' before
// the '@' gets two of the four wrong.

import (
	"strings"
	"testing"

	"github.com/buble/dbx/internal/config"
)

// Scenario: La contraseña del entorno entra en el DSN, sea cual sea su forma.
func TestPasswordEnvPutsThePasswordInTheDSN(t *testing.T) {
	t.Run("a URL with no credentials gets a bare user and the password", func(t *testing.T) {
		t.Setenv("DBX_TEST_PW", "s3cret")
		cfg := &config.Config{Connections: []config.ConnectionConfig{{
			Name:        "prod",
			URL:         "postgres://db.example.com:5432/prod",
			PasswordEnv: "DBX_TEST_PW",
		}}}

		dsn, err := dsnFor("prod", cfg)
		if err != nil {
			t.Fatalf("dsnFor: %v", err)
		}
		if !strings.Contains(dsn, ":s3cret@") {
			t.Errorf("the DSN is %q, want the password before the host", dsn)
		}
		// Everything else about the DSN has to survive: the port, the host and the
		// database name are what the user wrote.
		for _, want := range []string{"db.example.com", ":5432", "/prod", "postgres://"} {
			if !strings.Contains(dsn, want) {
				t.Errorf("the DSN is %q, want it to still contain %q", dsn, want)
			}
		}
	})

	t.Run("a URL with a user but no password gets the password appended", func(t *testing.T) {
		t.Setenv("DBX_TEST_PW", "s3cret")
		cfg := &config.Config{Connections: []config.ConnectionConfig{{
			Name:        "prod",
			URL:         "postgres://admin@db.example.com/prod",
			PasswordEnv: "DBX_TEST_PW",
		}}}

		dsn, err := dsnFor("prod", cfg)
		if err != nil {
			t.Fatalf("dsnFor: %v", err)
		}
		if dsn != "postgres://admin:s3cret@db.example.com/prod" {
			t.Errorf("the DSN is %q, want the password after the user", dsn)
		}
	})

	t.Run("a host/port connection gets the password too", func(t *testing.T) {
		t.Setenv("DBX_TEST_PW", "s3cret")
		cfg := &config.Config{Connections: []config.ConnectionConfig{{
			Name:        "prod",
			User:        "admin",
			Host:        "localhost",
			Port:        5433,
			Database:    "prod",
			PasswordEnv: "DBX_TEST_PW",
		}}}

		dsn, err := dsnFor("prod", cfg)
		if err != nil {
			t.Fatalf("dsnFor: %v", err)
		}
		if dsn != "postgres://admin:s3cret@localhost:5433/prod" {
			t.Errorf("the DSN is %q", dsn)
		}
	})

	t.Run("a DSN that ALREADY has a password keeps it", func(t *testing.T) {
		// The file is the more explicit statement. Overwriting it would make
		// password_env silently discard what the user wrote, and the only way to
		// find out would be to compare the two against a real server.
		t.Setenv("DBX_TEST_PW", "from-env")
		cfg := &config.Config{Connections: []config.ConnectionConfig{{
			Name:        "prod",
			URL:         "postgres://admin:from-file@db.example.com/prod",
			PasswordEnv: "DBX_TEST_PW",
		}}}

		dsn, err := dsnFor("prod", cfg)
		if err != nil {
			t.Fatalf("dsnFor: %v", err)
		}
		if strings.Contains(dsn, "from-env") {
			t.Errorf("the DSN is %q, want the password already in the file to win", dsn)
		}
		if !strings.Contains(dsn, "from-file") {
			t.Errorf("the DSN is %q, want the file's password kept", dsn)
		}
	})

	t.Run("a password containing a colon or an at sign is not re-parsed", func(t *testing.T) {
		// The check is "does the userinfo already contain a colon", and that has
		// to be decided BEFORE the password is inserted, or a password with a
		// colon would look like a second userinfo on the next pass.
		for _, pw := range []string{"a:b", "p@ss", "x:y@z"} {
			t.Setenv("DBX_TEST_PW", pw)
			cfg := &config.Config{Connections: []config.ConnectionConfig{{
				Name:        "prod",
				URL:         "postgres://admin@h/prod",
				PasswordEnv: "DBX_TEST_PW",
			}}}

			dsn, err := dsnFor("prod", cfg)
			if err != nil {
				t.Fatalf("dsnFor: %v", err)
			}
			if dsn != "postgres://admin:"+pw+"@h/prod" {
				t.Errorf("the password %q produced the DSN %q", pw, dsn)
			}
		}
	})

	t.Run("with no password_env the DSN is untouched", func(t *testing.T) {
		cfg := &config.Config{Connections: []config.ConnectionConfig{{
			Name: "prod",
			URL:  "postgres://db.example.com/prod",
		}}}

		dsn, err := dsnFor("prod", cfg)
		if err != nil {
			t.Fatalf("dsnFor: %v", err)
		}
		if dsn != "postgres://db.example.com/prod" {
			t.Errorf("the DSN is %q, want it byte for byte as written", dsn)
		}
	})

	t.Run("a password_env naming a variable that is NOT SET changes nothing", func(t *testing.T) {
		// Silently, and that is the right answer: a missing variable is a normal
		// state for a config that is shared between machines, and failing here
		// would mean dbx refuses to run before it has said anything.
		t.Setenv("DBX_TEST_ABSENT", "")
		cfg := &config.Config{Connections: []config.ConnectionConfig{{
			Name:        "prod",
			URL:         "postgres://admin@h/prod",
			PasswordEnv: "DBX_TEST_ABSENT",
		}}}

		dsn, err := dsnFor("prod", cfg)
		if err != nil {
			t.Fatalf("dsnFor: %v", err)
		}
		if dsn != "postgres://admin@h/prod" {
			t.Errorf("the DSN is %q, want an unset variable to add nothing", dsn)
		}
		if strings.Contains(dsn, "@:") || strings.HasSuffix(dsn, "@") {
			t.Errorf("the DSN is %q, want no empty password section", dsn)
		}
	})

	t.Run("only the NAMED connection is affected", func(t *testing.T) {
		// Two connections, one with the knob and one without. A change that read
		// the variable for whichever connection it happened to be holding would
		// leak one connection's password into another's DSN.
		t.Setenv("DBX_TEST_PW", "s3cret")
		cfg := &config.Config{Connections: []config.ConnectionConfig{
			{Name: "plain", URL: "postgres://plain@h/plain"},
			{Name: "secure", URL: "postgres://secure@h/secure", PasswordEnv: "DBX_TEST_PW"},
		}}

		plain, err := dsnFor("plain", cfg)
		if err != nil {
			t.Fatalf("dsnFor: %v", err)
		}
		if strings.Contains(plain, "s3cret") {
			t.Errorf("the plain connection got the password: %q", plain)
		}

		secure, err := dsnFor("secure", cfg)
		if err != nil {
			t.Fatalf("dsnFor: %v", err)
		}
		if !strings.Contains(secure, "s3cret") {
			t.Errorf("the secure connection did not get the password: %q", secure)
		}
	})
}

// Scenario: dsnWithPassword es una FUNCION PURA y se comporta como tal.
//
// Asserted directly rather than only through dsnFor, because every branch here is a
// decision about the SHAPE of a string and a shape is much easier to break than a config
// lookup. The empty-DSN case in particular can only be reached by calling this directly.
func TestDsnWithPasswordIsPure(t *testing.T) {
	for _, tc := range []struct{ name, dsn, pw, want string }{
		{"no password at all", "postgres://h/d", "", "postgres://h/d"},
		{"an empty DSN", "", "pw", ""},
		{"an empty DSN and no password", "", "", ""},
		{"no scheme at all", "h/d", "pw", "h/d"},
		{"a scheme with nothing after it", "postgres://", "pw", "postgres://:pw@"},
		{"an at sign inside the password", "postgres://u@h/d", "a@b", "postgres://u:a@b@h/d"},
		{"a unix socket DSN has no host to attach to", "postgres:///db?host=/var/run", "pw", "postgres://:pw@/db?host=/var/run"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dsnWithPassword(tc.dsn, tc.pw); got != tc.want {
				t.Errorf("dsnWithPassword(%q, %q) = %q, want %q", tc.dsn, tc.pw, got, tc.want)
			}
		})
	}

	t.Run("it does not mutate its input, because strings are immutable but the INTENT matters", func(t *testing.T) {
		// Not a runtime property — a Go string cannot be mutated. Asserted as a
		// reminder in the test rather than the code, because "it modifies the DSN
		// in place" is the kind of claim that gets made in a review of this
		// function and is false.
		dsn := "postgres://h/d"
		_ = dsnWithPassword(dsn, "pw")
		if dsn != "postgres://h/d" {
			t.Errorf("the input DSN became %q", dsn)
		}
	})
}
