package dbmigrator

import (
	"fmt"
	"path/filepath"
	"slices"

	. "github.com/dave/jennifer/jen"
	"github.com/iancoleman/strcase"

	cli "github.com/threeport/threeport/pkg/cli/v0"
	sdk "github.com/threeport/threeport/pkg/sdk/v0"
	"github.com/threeport/threeport/pkg/sdk/v0/gen"
	"github.com/threeport/threeport/pkg/sdk/v0/util"
)

// GenDbMigratorMain generates source code for the DB migrator main package.
func GenDbMigratorMain(gen *gen.Generator, sdkConfig *sdk.SdkConfig) error {
	f := NewFile("main")
	f.HeaderComment(sdk.HeaderCommentGenNoEdit)

	// set table name for goose table that tracks DB version
	gooseVersionTableName := "threeport_goose_db_version"
	if gen.Module {
		gooseVersionTableName = fmt.Sprintf(
			"threeport_%s_goose_db_version",
			strcase.ToSnake(sdkConfig.ModuleName),
		)
	}

	// set table name for the row every migrator of this API contends for, named
	// per API for the same reason the version table is: threeport and its modules
	// migrate one database, and a module migrator has no reason to wait on
	// threeport's
	migrationLockTableName := "threeport_migration_lock"
	if gen.Module {
		migrationLockTableName = fmt.Sprintf(
			"threeport_%s_migration_lock",
			strcase.ToSnake(sdkConfig.ModuleName),
		)
	}

	f.ImportAlias("github.com/pressly/goose/v3", "goose")
	f.ImportAlias("github.com/pressly/goose/v3/database", "goosedb")
	f.ImportAlias("github.com/threeport/threeport/pkg/cli/v0", "cli")
	f.ImportAlias("github.com/threeport/threeport/pkg/log/v0", "log")
	f.Anon("github.com/lib/pq")

	// the session locker is hand-written in the threeport API server database
	// package, so a module migrator reaches across to threeport for it rather
	// than to its own generated database package
	threeportDbPath := "github.com/threeport/threeport/pkg/api-server/v0/database"

	var installerPath string
	var apiServerDbPath string
	if gen.Module {
		installerPath = fmt.Sprintf("%s/pkg/installer/v0", gen.ModulePath)
		apiServerDbPath = fmt.Sprintf("%s/pkg/api-server/v0/database", gen.ModulePath)
		f.ImportAlias(
			installerPath,
			"installer",
		)
		f.ImportAlias(apiServerDbPath, "database")
		f.ImportAlias(threeportDbPath, "threeportdb")
		f.Anon(fmt.Sprintf("%s/cmd/database-migrator/migrations", gen.ModulePath))
	} else {
		installerPath = "github.com/threeport/threeport/pkg/threeport-installer/v0"
		apiServerDbPath = threeportDbPath
		f.ImportAlias(installerPath, "installer")
		f.ImportAlias(apiServerDbPath, "database")
		f.Anon("github.com/threeport/threeport/cmd/database-migrator/migrations")
	}

	f.Var().Defs(
		Id("gooseCommands").Op("=").Index().String().Values(
			Lit("up"), Lit("up-to"), Lit("up-by-one"), Lit("down"), Lit("down-to"), Lit("redo"), Lit("status"),
		),
		Id("envFile").Op("=").Lit(""),
	)

	f.Func().Id("main").Params().Block(
		Qual("flag", "StringVar").Call(
			Id("&envFile"), Lit("env-file"), Lit(""), Lit("File from which to load environment"),
		),
		Qual("flag", "Parse").Call(),
		Line(),

		Comment("initialize logger"),
		List(Id("logger"), Id("err")).Op(":=").Qual(
			"github.com/threeport/threeport/pkg/log/v0",
			"NewLogger",
		).Call(False()),
		If(Id("err").Op("!=").Nil()).Block(
			Id("returnErr").Call(Lit("could not create logger"), Id("err")),
		),
		Line(),

		Comment("env vars for database connection"),
		If(Id("envFile").Op("!=").Lit("")).Block(
			If(Err().Op(":=").Qual("github.com/joho/godotenv", "Load").Call(
				Id("envFile"),
			), Err().Op("!=").Nil()).Block(
				Id("returnErr").Call(Lit("failed to load environment variables"), Id("err")),
			),
		),
		Line(),

		Id("args").Op(":=").Qual("flag", "Args").Call(),
		Id("command").Op(":=").Id("args").Index(Lit(0)),
		Id("arguments").Op(":=").Index().String().Values(),
		If(Len(Id("args")).Op(">").Lit(1)).Block(
			Id("arguments").Op("=").Id("args").Index(Lit(1).Op(":")),
		),
		Line(),

		Comment("validate command arg"),
		Var().Id("found").Bool(),
		For(List(Id("_"), Id("c")).Op(":=").Range().Id("validArgs").Call()).Block(
			If(Id("command").Op("==").Id("c")).Block(
				Id("found").Op("=").True(),
				Break(),
			),
		),
		If(Op("!").Id("found")).Block(
			Id("returnErr").Call(
				Lit(""), Qual("fmt", "Errorf").Call(Lit("%s is not a valid argument"), Id("command")),
			),
		),
		Line(),

		Switch(Id("command")).Block(
			Case(Lit("initialize")).Block(
				If(Id("err").Op(":=").Id("initializeDb").Call(Id("logger")), Id("err").Op("!=").Nil()).Block(
					Id("returnErr").Call(Lit("failed to initialize database and user"), Id("err")),
				),
				Qual("os", "Exit").Call(Lit(0)),
			),
			Default().Block(
				If(Id("err").Op(":=").Id("migrateDb").Call(
					Id("command"), Id("arguments"), Id("logger"),
				), Id("err").Op("!=").Nil()).Block(
					Id("returnErr").Call(Lit("failed to migrate database schema"), Id("err")),
				),
			),
		),
		Line(),

		Qual("os", "Exit").Call(Lit(0)),
	)
	f.Line()

	f.Comment("migrateDb runs the provided goose command - usually the 'up' command to apply")
	f.Comment("migrations.")
	f.Func().Id("migrateDb").Params(
		Line().Id("command").String(),
		Line().Id("arguments").Index().String(),
		Line().Id("logger").Qual("go.uber.org/zap", "Logger"),
		Line(),
	).Params(Error()).Block(
		Comment("get non-root database connection string"),
		List(Id("dsn"), Id("err")).Op(":=").Qual(apiServerDbPath, "GetDsn").Call(False()),
		If(Id("err").Op("!=").Nil()).Block(
			Id("returnErr").Call(Lit("failed to populate DB DSN from environment"), Id("err")),
		),
		Line(),

		Comment("configure goose driver"),
		List(Id("db"), Id("err")).Op(":=").Qual(
			"github.com/pressly/goose/v3",
			"OpenDBWithDriver",
		).Call(Lit("postgres"), Id("dsn")),
		If(Id("err").Op("!=").Nil()).Block(
			Id("returnErr").Call(Lit("goose failed to open DB"), Id("err")),
		),
		Defer().Func().Params().Block(
			If(Id("err").Op(":=").Id("db").Dot("Close").Call(), Id("err").Op("!=").Nil()).Block(
				Id("returnErr").Call(Lit("goose failed to close DB"), Id("err")),
			),
		).Call(),
		Line(),

		Comment("configure gorm DB"),
		List(Id("gormdb"), Id("err")).Op(":=").Qual("gorm.io/gorm", "Open").Call(
			Qual("gorm.io/driver/postgres", "Open").Call(Id("dsn")),
			Op("&").Qual("gorm.io/gorm", "Config").Values(Dict{
				Id("Logger"): Op("&").Qual(apiServerDbPath, "ZapLogger").Values(Dict{
					Id("Logger"): Op("&").Id("logger"),
				}),
				Id("NowFunc"): Func().Params().Qual("time", "Time").Block(
					Id("utc").Op(",").Op("_").Op(":=").Qual("time", "LoadLocation").Call(Lit("UTC")),
					Return(Qual("time", "Now").Call().Dot("In").Call(
						Id("utc"),
					).Dot("Truncate").Call(Qual("time", "Microsecond"))),
				),
			}),
		),
		If(Id("err").Op("!=").Nil()).Block(
			Id("returnErr").Call(Lit("could not create gorm db object"), Id("err")),
		),
		Line(),

		Comment("hold a lock for the whole run so a rolling update of the API server,"),
		Comment("which starts a second migrator before the first one finishes, cannot"),
		Comment("apply and record the same migration twice"),
		List(Id("sessionLocker"), Id("err")).Op(":=").Qual(
			threeportDbPath,
			"NewCockroachSessionLocker",
		).Call(Lit(migrationLockTableName)),
		If(Id("err").Op("!=").Nil()).Block(
			Id("returnErr").Call(Lit("failed to build migration session locker"), Id("err")),
		),
		Line(),

		Comment("record applied migrations in a table of this API's own so anything"),
		Comment("else migrating the same database keeps a separate ledger. Nothing is"),
		Comment("read from a filesystem because every migration registers itself with"),
		Comment("goose at startup."),
		List(Id("provider"), Id("err")).Op(":=").Qual(
			"github.com/pressly/goose/v3",
			"NewProvider",
		).Call(
			Line().Qual("github.com/pressly/goose/v3", "DialectPostgres"),
			Line().Id("db"),
			Line().Nil(),
			Line().Qual("github.com/pressly/goose/v3", "WithTableName").Call(
				Lit(gooseVersionTableName),
			),
			Line().Qual("github.com/pressly/goose/v3", "WithSessionLocker").Call(
				Id("sessionLocker"),
			),
			Line().Qual("github.com/pressly/goose/v3", "WithVerbose").Call(True()),
			Line(),
		),
		If(Id("err").Op("!=").Nil()).Block(
			Id("returnErr").Call(Lit("failed to build goose migration provider"), Id("err")),
		),
		Line(),

		Comment("run migrations, which read the gorm DB back out of the context under"),
		Comment("this key"),
		Id("ctx").Op(":=").Qual("context", "WithValue").Call(
			Qual("context", "TODO").Call(), Lit("gormdb"), Id("gormdb"),
		),
		If(Id("err").Op(":=").Id("runGooseCommand").Call(
			Id("ctx"), Id("provider"), Id("command"), Id("arguments"),
		), Id("err").Op("!=").Nil()).Block(
			Id("returnErr").Call(
				Qual("fmt", "Sprintf").Call(Lit("goose %s command failed"), Id("command")), Id("err"),
			),
		),
		Line(),

		Id("logger").Dot("Info").Call(Lit("database schema successfully migrated")),
		Line(),

		Return(Nil()),
	)
	f.Line()

	f.Comment("runGooseCommand runs one database-migrator command against the migration")
	f.Comment("provider.")
	f.Func().Id("runGooseCommand").Params(
		Line().Id("ctx").Qual("context", "Context"),
		Line().Id("provider").Op("*").Qual("github.com/pressly/goose/v3", "Provider"),
		Line().Id("command").String(),
		Line().Id("arguments").Index().String(),
		Line(),
	).Params(Error()).Block(
		Switch(Id("command")).Block(
			Case(Lit("up")).Block(
				List(Id("_"), Id("err")).Op(":=").Id("provider").Dot("Up").Call(Id("ctx")),
				Return(Id("err")),
			),
			Case(Lit("up-by-one")).Block(
				List(Id("_"), Id("err")).Op(":=").Id("provider").Dot("UpByOne").Call(Id("ctx")),
				Return(Id("err")),
			),
			Case(Lit("up-to")).Block(
				List(Id("version"), Id("err")).Op(":=").Id("migrationVersion").Call(
					Id("command"), Id("arguments"),
				),
				If(Id("err").Op("!=").Nil()).Block(
					Return(Id("err")),
				),
				List(Id("_"), Id("err")).Op("=").Id("provider").Dot("UpTo").Call(
					Id("ctx"), Id("version"),
				),
				Return(Id("err")),
			),
			Case(Lit("down")).Block(
				List(Id("_"), Id("err")).Op(":=").Id("provider").Dot("Down").Call(Id("ctx")),
				Return(Id("err")),
			),
			Case(Lit("down-to")).Block(
				List(Id("version"), Id("err")).Op(":=").Id("migrationVersion").Call(
					Id("command"), Id("arguments"),
				),
				If(Id("err").Op("!=").Nil()).Block(
					Return(Id("err")),
				),
				List(Id("_"), Id("err")).Op("=").Id("provider").Dot("DownTo").Call(
					Id("ctx"), Id("version"),
				),
				Return(Id("err")),
			),
			Case(Lit("redo")).Block(
				Comment("the provider offers no redo of its own, so roll the newest"),
				Comment("migration back and apply it again, taking the migration lock"),
				Comment("separately for each half"),
				If(
					List(Id("_"), Id("err")).Op(":=").Id("provider").Dot("Down").Call(Id("ctx")),
					Id("err").Op("!=").Nil(),
				).Block(
					Return(Id("err")),
				),
				List(Id("_"), Id("err")).Op(":=").Id("provider").Dot("UpByOne").Call(Id("ctx")),
				Return(Id("err")),
			),
			Case(Lit("status")).Block(
				Return(Id("printMigrationStatus").Call(Id("ctx"), Id("provider"))),
			),
		),
		Line(),

		Return(Qual("fmt", "Errorf").Call(
			Lit("%s is not a goose command"), Id("command"),
		)),
	)
	f.Line()

	f.Comment("migrationVersion reads the migration version a command takes as its only")
	f.Comment("argument.")
	f.Func().Id("migrationVersion").Params(
		Id("command").String(),
		Id("arguments").Index().String(),
	).Params(Int64(), Error()).Block(
		If(Len(Id("arguments")).Op("==").Lit(0)).Block(
			Return(Lit(0), Qual("fmt", "Errorf").Call(
				Lit("%s requires a migration version argument"), Id("command"),
			)),
		),
		Line(),

		List(Id("version"), Id("err")).Op(":=").Qual("strconv", "ParseInt").Call(
			Id("arguments").Index(Lit(0)), Lit(10), Lit(64),
		),
		If(Id("err").Op("!=").Nil()).Block(
			Return(Lit(0), Qual("fmt", "Errorf").Call(
				Lit("migration version must be a number, got %s: %w"),
				Id("arguments").Index(Lit(0)),
				Id("err"),
			)),
		),
		Line(),

		Return(Id("version"), Nil()),
	)
	f.Line()

	f.Comment("printMigrationStatus prints when each known migration was applied, or that")
	f.Comment("it is still pending.")
	f.Func().Id("printMigrationStatus").Params(
		Id("ctx").Qual("context", "Context"),
		Id("provider").Op("*").Qual("github.com/pressly/goose/v3", "Provider"),
	).Params(Error()).Block(
		List(Id("migrationStatus"), Id("err")).Op(":=").Id("provider").Dot("Status").Call(Id("ctx")),
		If(Id("err").Op("!=").Nil()).Block(
			Return(Id("err")),
		),
		Line(),

		Qual("fmt", "Println").Call(Lit("    Applied At                  Migration")),
		Qual("fmt", "Println").Call(Lit("    =======================================")),
		For(List(Id("_"), Id("status")).Op(":=").Range().Id("migrationStatus")).Block(
			Id("appliedAt").Op(":=").Lit("Pending"),
			If(Id("status").Dot("State").Op("==").Qual(
				"github.com/pressly/goose/v3", "StateApplied",
			)).Block(
				Id("appliedAt").Op("=").Id("status").Dot("AppliedAt").Dot("Format").Call(
					Qual("time", "ANSIC"),
				),
			),
			Line(),

			Comment("a migration registered from Go carries no file path, so name it by"),
			Comment("the version that identifies it instead"),
			Id("source").Op(":=").Qual("strconv", "FormatInt").Call(
				Id("status").Dot("Source").Dot("Version"), Lit(10),
			),
			If(Id("status").Dot("Source").Dot("Path").Op("!=").Lit("")).Block(
				Id("source").Op("=").Qual("path/filepath", "Base").Call(
					Id("status").Dot("Source").Dot("Path"),
				),
			),
			Line(),

			Qual("fmt", "Printf").Call(
				Lit("    %-24s -- %s\n"), Id("appliedAt"), Id("source"),
			),
		),
		Line(),

		Return(Nil()),
	)
	f.Line()

	f.Comment("initializeDb creates the database and user using root database user.")
	f.Func().Id("initializeDb").Params(
		Id("logger").Qual("go.uber.org/zap", "Logger"),
	).Params(Error()).Block(
		List(Id("dsn"), Id("err")).Op(":=").Qual(apiServerDbPath, "GetDsn").Call(True()),
		If(Id("err").Op("!=").Nil()).Block(
			Return(Qual("fmt", "Errorf").Call(Lit("failed to get data source string: %w"), Id("err"))),
		),
		Line(),

		Comment("at deployment time, this is the first database connection made so we"),
		Comment("retry the connection for 5 min before returning an error"),
		Var().Id("connAttemptsMax").Op("=").Lit(30),
		Var().Id("connAttemptIntervalSeconds").Op("=").Lit(10),
		Var().Id("connAttempts").Op("=").Lit(0),
		Var().Id("gormDb").Op("*").Qual("gorm.io/gorm", "DB"),
		Var().Id("dbConnErr").Error(),
		For(Id("connAttempts").Op("<").Id("connAttemptsMax")).Block(
			List(Id("db"), Id("err")).Op(":=").Qual("gorm.io/gorm", "Open").Call(
				Qual("gorm.io/driver/postgres", "Open").Call(Id("dsn")),
				Op("&").Qual("gorm.io/gorm", "Config").Values(Dict{
					Id("Logger"): Op("&").Qual(apiServerDbPath, "ZapLogger").Values(Dict{
						Id("Logger"): Op("&").Id("logger"),
					}),
					Id("NowFunc"): Func().Params().Qual("time", "Time").Block(
						Id("utc").Op(",").Op("_").Op(":=").Qual("time", "LoadLocation").Call(Lit("UTC")),
						Return(Qual("time", "Now").Call().Dot("In").Call(Id("utc")).Dot("Truncate").Call(
							Qual("time", "Microsecond"),
						)),
					),
				}),
			),
			If(Id("err").Op("!=").Nil()).Block(
				Id("logger").Dot("Info").Call(Qual("fmt", "Sprintf").Call(
					Line().Lit("failed to make DB connection, retrying in %d seconds"),
					Line().Id("connAttemptIntervalSeconds"),
					Line(),
				)),
				Id("dbConnErr").Op("=").Id("err"),
			).Else().Block(
				Id("gormDb").Op("=").Id("db"),
				Break(),
			),
			Id("connAttempts").Op("++"),
			Qual("time", "Sleep").Call(Qual("time", "Second").Op("*").Qual("time", "Duration").Call(
				Id("connAttemptIntervalSeconds"),
			)),
		),
		If(Id("gormDb").Op("==").Nil()).Block(
			Return(Qual("fmt", "Errorf").Call(
				Lit("timed out after 5 mim attempting to make database connection: %w"), Id("dbConnErr"),
			)),
		),
		Line(),

		Comment("execute SQL init script"),
		Id("sqlFile").Op(":=").Qual("path/filepath", "Join").Call(
			Qual(installerPath, "DbInitLocation"), Qual(installerPath, "DbInitFilename"),
		),
		List(Id("sqlScript"), Id("err")).Op(":=").Qual("io/ioutil", "ReadFile").Call(Id("sqlFile")),
		If(Id("err").Op("!=").Nil()).Block(
			Id("returnErr").Call(Lit("failed to read SQL init script"), Id("err")),
		),
		If(Id("err").Op(":=").Id("gormDb").Dot("Exec").Call(String().Call(
			Id("sqlScript"),
		)).Dot("Error"), Id("err").Op("!=").Nil()).Block(
			Id("returnErr").Call(Lit("failed to execute SQL init script"), Id("err")),
		),
		Line(),

		Id("logger").Dot("Info").Call(Lit("database successfully initialized")),
		Line(),

		Return(Nil()),
	)

	f.Comment("returnErr returns errors with usage info and exits with non-zero")
	f.Func().Id("returnErr").Params(
		Id("msg").String(),
		Id("err").Error(),
	).Block(
		Qual("github.com/threeport/threeport/pkg/cli/v0", "Error").Call(Id("msg"), Id("err")),
		Id("usage").Call(),
		Qual("os", "Exit").Call(Lit(1)),
	)
	f.Line()

	f.Comment("usage prints the usage info for database-migrator")
	f.Func().Id("usage").Params().Block(
		Id("args").Op(":=").Id("validArgs").Call(),
		Qual("fmt", "Printf").Call(
			Lit(`database-migrator initializes and manages the database schema for the Threeport API

usage: database-migrator [-env-file /path/to/environment_file] <arguments>

valid arguments: %s

examples:
	Initialize the database by creating database and user:
	database-migrator -env-file=/etc/threeport/env initialize

	Run database migrations to apply database schema:
	database-migrator -env-file=/etc/threeport/env up
}\n`), Id("args"),
		),
	)
	f.Line()

	f.Comment("validArgs returns all valid arguments to database-migrator")
	f.Func().Id("validArgs").Params().Index().String().Block(
		Return(Append(Id("gooseCommands"), Lit("initialize"))),
	)

	// write code to file if not excluded by SDK config
	genFilepath := filepath.Join("cmd", "database-migrator", "main_gen.go")
	if slices.Contains(sdkConfig.ExcludeFiles, genFilepath) {
		cli.Info(fmt.Sprintf("source code generation skipped for %s", genFilepath))
	} else {
		_, err := util.WriteCodeToFile(f, genFilepath, true)
		if err != nil {
			return fmt.Errorf("failed to write generated code to file %s: %w", genFilepath, err)
		}
		cli.Info(fmt.Sprintf("source code for DB migrator main package written to %s", genFilepath))
	}

	return nil
}

// GenDbMigratorSchemaDriftTest generates a test that checks the schema built by
// the migrations against the columns the persisted models declare.
func GenDbMigratorSchemaDriftTest(gen *gen.Generator, sdkConfig *sdk.SdkConfig) error {
	f := NewFile("main")
	f.HeaderComment(sdk.HeaderCommentGenNoEdit)

	f.ImportAlias("github.com/pressly/goose/v3", "goose")
	f.ImportAlias("gorm.io/driver/sqlite", "sqlite")
	f.ImportAlias("gorm.io/gorm", "gorm")

	gooseVersionTableName := "threeport_goose_db_version"
	if gen.Module {
		gooseVersionTableName = fmt.Sprintf(
			"threeport_%s_goose_db_version",
			strcase.ToSnake(sdkConfig.ModuleName),
		)
	}

	f.Comment("persistedModels returns one instance of every model the API persists.")
	f.Func().Id("persistedModels").Params().Params(Index().Interface()).Block(
		Return().Index().Interface().BlockFunc(func(g *Group) {
			for _, version := range gen.GlobalVersionConfig.Versions {
				for _, name := range version.DatabaseInitNames {
					g.List(
						Op("&").Qual(
							fmt.Sprintf("%s/pkg/api/%s", gen.ModulePath, version.VersionName),
							name,
						).Values().Op(","),
					)
				}
			}
		}),
	)
	f.Line()

	f.Comment("migratedSchema applies every registered migration to a fresh in-memory")
	f.Comment("database and returns a handle on the resulting schema.")
	f.Func().Id("migratedSchema").Params(
		Id("t").Op("*").Qual("testing", "T"),
	).Params(Op("*").Qual("gorm.io/gorm", "DB")).Block(
		Id("t").Dot("Helper").Call(),
		Line(),

		List(Id("gormDb"), Id("err")).Op(":=").Qual("gorm.io/gorm", "Open").Call(
			Qual("gorm.io/driver/sqlite", "Open").Call(Lit(":memory:")),
			Op("&").Qual("gorm.io/gorm", "Config").Values(),
		),
		If(Id("err").Op("!=").Nil()).Block(
			Id("t").Dot("Fatalf").Call(Lit("open sqlite: %v"), Id("err")),
		),
		Line(),

		Comment("share one connection pool so the migrations and the assertions see"),
		Comment("the same in-memory database"),
		List(Id("sqlDb"), Id("err")).Op(":=").Id("gormDb").Dot("DB").Call(),
		If(Id("err").Op("!=").Nil()).Block(
			Id("t").Dot("Fatalf").Call(Lit("resolve sql db: %v"), Id("err")),
		),
		Line(),

		If(Id("err").Op(":=").Qual("github.com/pressly/goose/v3", "SetDialect").Call(
			Lit("sqlite3"),
		), Id("err").Op("!=").Nil()).Block(
			Id("t").Dot("Fatalf").Call(Lit("set goose dialect: %v"), Id("err")),
		),
		Qual("github.com/pressly/goose/v3", "SetTableName").Call(Lit(gooseVersionTableName)),
		Line(),

		Comment("the migrations read the gorm db from the context under the same key"),
		Comment("the deployed migrator sets"),
		Id("ctx").Op(":=").Qual("context", "WithValue").Call(
			Qual("context", "Background").Call(), Lit("gormdb"), Id("gormDb"),
		),
		If(Id("err").Op(":=").Qual("github.com/pressly/goose/v3", "UpContext").Call(
			Id("ctx"), Id("sqlDb"), Lit("."),
		), Id("err").Op("!=").Nil()).Block(
			Id("t").Dot("Fatalf").Call(Lit("apply migrations: %v"), Id("err")),
		),
		Line(),

		Return(Id("gormDb")),
	)
	f.Line()

	f.Comment("TestMigrationsCoverEveryPersistedModel asserts the schema the migration")
	f.Comment("chain builds matches the columns every persisted model declares, reporting")
	f.Comment("both fields left without a column and columns left without a field.")
	f.Func().Id("TestMigrationsCoverEveryPersistedModel").Params(
		Id("t").Op("*").Qual("testing", "T"),
	).Block(
		Comment("build the schema a deployed database would have after an upgrade"),
		Id("gormDb").Op(":=").Id("migratedSchema").Call(Id("t")),
		Line(),

		For(List(Id("_"), Id("model")).Op(":=").Range().Id("persistedModels").Call()).Block(
			Comment("resolve the columns the model's fields declare, which accounts"),
			Comment("for embedded structs, column overrides and excluded fields"),
			Id("stmt").Op(":=").Op("&").Qual("gorm.io/gorm", "Statement").Values(Dict{
				Id("DB"): Id("gormDb"),
			}),
			If(Id("err").Op(":=").Id("stmt").Dot("Parse").Call(
				Id("model"),
			), Id("err").Op("!=").Nil()).Block(
				Id("t").Dot("Fatalf").Call(Lit("parse %T: %v"), Id("model"), Id("err")),
			),
			Id("declared").Op(":=").Make(
				Map(String()).Bool(), Len(Id("stmt").Dot("Schema").Dot("DBNames")),
			),
			For(List(Id("_"), Id("name")).Op(":=").Range().Id("stmt").Dot("Schema").Dot("DBNames")).Block(
				Id("declared").Index(Id("name")).Op("=").True(),
			),
			Line(),

			Comment("a model no migration creates reads as total drift"),
			If(Op("!").Id("gormDb").Dot("Migrator").Call().Dot("HasTable").Call(
				Id("model"),
			)).Block(
				Id("t").Dot("Errorf").Call(
					Lit("no migration creates table %s for %T"),
					Id("stmt").Dot("Schema").Dot("Table"),
					Id("model"),
				),
				Continue(),
			),
			Line(),

			Comment("read the columns the migration chain actually created"),
			List(Id("columnTypes"), Id("err")).Op(":=").Id("gormDb").Dot("Migrator").Call().Dot(
				"ColumnTypes",
			).Call(Id("model")),
			If(Id("err").Op("!=").Nil()).Block(
				Id("t").Dot("Fatalf").Call(Lit("read columns for %T: %v"), Id("model"), Id("err")),
			),
			Id("created").Op(":=").Make(Map(String()).Bool(), Len(Id("columnTypes"))),
			For(List(Id("_"), Id("columnType")).Op(":=").Range().Id("columnTypes")).Block(
				Id("created").Index(Id("columnType").Dot("Name").Call()).Op("=").True(),
			),
			Line(),

			Comment("a declared field with no column means a migration was never written"),
			Var().Id("missingColumns").Index().String(),
			For(Id("name").Op(":=").Range().Id("declared")).Block(
				If(Op("!").Id("created").Index(Id("name"))).Block(
					Id("missingColumns").Op("=").Append(Id("missingColumns"), Id("name")),
				),
			),
			Line(),

			Comment("a column with no declared field means a migration never dropped it"),
			Var().Id("missingFields").Index().String(),
			For(Id("name").Op(":=").Range().Id("created")).Block(
				If(Op("!").Id("declared").Index(Id("name"))).Block(
					Id("missingFields").Op("=").Append(Id("missingFields"), Id("name")),
				),
			),
			Line(),

			Comment("report both directions so one run shows the whole drift"),
			Qual("sort", "Strings").Call(Id("missingColumns")),
			Qual("sort", "Strings").Call(Id("missingFields")),
			If(Len(Id("missingColumns")).Op(">").Lit(0)).Block(
				Id("t").Dot("Errorf").Call(
					Lit("%s has fields with no column: %v"),
					Id("stmt").Dot("Schema").Dot("Table"),
					Id("missingColumns"),
				),
			),
			If(Len(Id("missingFields")).Op(">").Lit(0)).Block(
				Id("t").Dot("Errorf").Call(
					Lit("%s has columns with no field: %v"),
					Id("stmt").Dot("Schema").Dot("Table"),
					Id("missingFields"),
				),
			),
		),
	)

	// write code to file if not excluded by SDK config
	genFilepath := filepath.Join("cmd", "database-migrator", "schema_drift_gen_test.go")
	if slices.Contains(sdkConfig.ExcludeFiles, genFilepath) {
		cli.Info(fmt.Sprintf("source code generation skipped for %s", genFilepath))
	} else {
		_, err := util.WriteCodeToFile(f, genFilepath, true)
		if err != nil {
			return fmt.Errorf("failed to write generated code to file %s: %w", genFilepath, err)
		}
		cli.Info(fmt.Sprintf("source code for DB migrator schema drift test written to %s", genFilepath))
	}

	return nil
}
