package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	_ "github.com/lib/pq"
	"github.com/spf13/cobra"

	"github.com/pthm/melange/lib/cli"
	"github.com/pthm/melange/lib/doctor"
)

var (
	doctorDB       string
	doctorDBSchema string
	doctorSchema   string
	doctorVerbose  bool
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run health checks",
	Long:  `Run health checks on authorization infrastructure.`,
	Example: `  # Run health checks
  melange doctor --db postgres://localhost/mydb

  # Use a different database schema
  melange doctor --db postgres://localhost/mydb --db-schema myschema

  # Run with verbose output
  melange doctor --db postgres://localhost/mydb --verbose`,
	RunE: func(cmd *cobra.Command, args []string) error {
		schemaPath := resolveString(doctorSchema, cfg.Schema)
		verboseFlag := resolveBool(doctorVerbose, cfg.Doctor.Verbose)
		databaseSchema := resolveString(doctorDBSchema, cfg.Database.Schema)

		dsn, err := resolveDSN(doctorDB)
		if err != nil {
			return err
		}

		return runDoctor(dsn, schemaPath, verboseFlag, databaseSchema)
	},
}

func init() {
	f := doctorCmd.Flags()
	f.StringVar(&doctorDB, "db", "", "database URL")
	f.StringVar(&doctorDBSchema, "db-schema", "", "database schema")
	f.StringVar(&doctorSchema, "schema", "", "path to schema.fga file")
	f.BoolVar(&doctorVerbose, "verbose", false, "show detailed output")
}

func runDoctor(dsn, schemaPath string, verboseFlag bool, databaseSchema string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return cli.DBConnectError("connecting to database", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	if !quiet {
		fmt.Println("melange doctor - Health Check")
	}

	d := doctor.New(db, schemaPath)
	d.SetDatabaseSchema(databaseSchema)

	report, err := d.Run(ctx)
	if err != nil {
		return cli.GeneralError("running doctor", err)
	}

	report.Print(os.Stdout, verboseFlag)

	if report.HasErrors() {
		return cli.GeneralError("health checks failed", nil)
	}

	return nil
}
