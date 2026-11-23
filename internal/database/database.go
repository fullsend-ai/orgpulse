package database

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/go-sql-driver/mysql"
)

// tlsConfigName is the name this package registers its custom TLS config
// under via mysql.RegisterTLSConfig. Only relevant when a CA bundle path is
// actually provided -- see buildDSN.
const tlsConfigName = "orgpulse-rds"

// buildDSN constructs the MySQL DSN for OpenDB. When caBundlePath is empty,
// the DSN has no tls parameter at all -- the driver's default (plain,
// unencrypted) behavior, unchanged from before TLS support existed, so local
// dev and any MySQL not enforcing TLS keep working exactly as before.
//
// When caBundlePath is set, it's read as a PEM CA bundle and registered as a
// named TLS config, and the DSN requests it by name. This is real certificate
// verification against that specific CA (not tls=skip-verify) -- needed
// because RDS's CA isn't in the system trust store.
func buildDSN(host string, port int, user, password, dbName, caBundlePath string) (string, error) {
	tlsParam := ""
	if caBundlePath != "" {
		pem, err := os.ReadFile(caBundlePath)
		if err != nil {
			return "", fmt.Errorf("read TLS CA bundle %q: %w", caBundlePath, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return "", fmt.Errorf("no valid certificates found in TLS CA bundle %q", caBundlePath)
		}
		if err := mysql.RegisterTLSConfig(tlsConfigName, &tls.Config{RootCAs: pool}); err != nil {
			return "", fmt.Errorf("register TLS config: %w", err)
		}
		tlsParam = "&tls=" + tlsConfigName
	}

	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&multiStatements=true%s",
		user, password, host, port, dbName, tlsParam), nil
}

// OpenDB opens and pings a MySQL connection pool. Shared by NewMySQLStore
// (which assumes the schema is already current) and Migrate (which applies
// pending schema changes) so both go through identical DSN/TLS/pool setup.
func OpenDB(host string, port int, user, password, dbName, tlsCABundlePath string) (*sql.DB, error) {
	dsn, err := buildDSN(host, port, user, password, dbName, tlsCABundlePath)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}

type Store interface {
	GetOrgs(ctx context.Context) ([]Org, error)
	GetProducts(ctx context.Context) ([]Product, error)
	GetTeams(ctx context.Context) ([]Team, error)
	Close() error
}

type ConfigStore interface {
	ListOrgRows(ctx context.Context) ([]OrgRow, error)
	GetOrgRow(ctx context.Context, id string) (*OrgRow, error)
	CreateOrg(ctx context.Context, input OrgRow) error
	UpdateOrg(ctx context.Context, id string, input OrgRow) error
	DeleteOrg(ctx context.Context, id string) error

	ListProductRows(ctx context.Context) ([]ProductRow, error)
	GetProductRow(ctx context.Context, id string) (*ProductRow, error)
	CreateProduct(ctx context.Context, input ProductRow) error
	UpdateProduct(ctx context.Context, id string, input ProductRow) error
	DeleteProduct(ctx context.Context, id string) error

	ListTeamRows(ctx context.Context) ([]TeamRow, error)
	GetTeamRow(ctx context.Context, id string) (*TeamRow, error)
	CreateTeam(ctx context.Context, input TeamRow) error
	UpdateTeam(ctx context.Context, id string, input TeamRow) error
	DeleteTeam(ctx context.Context, id string) error

	GetOrgProductLinks(ctx context.Context, orgID string) ([]RelEntry, error)
	ReplaceOrgProductLinks(ctx context.Context, orgID string, entries []RelEntry) error
	GetProductTeamLinks(ctx context.Context, productID string) ([]RelEntry, error)
	ReplaceProductTeamLinks(ctx context.Context, productID string, entries []RelEntry) error
	ListAllTeamProductLinks(ctx context.Context) ([]TeamProductLink, error)
	GetTeamProductLabels(ctx context.Context, teamID string) ([]string, error)
	ReplaceTeamProductLabels(ctx context.Context, teamID string, labels []string) error

	GetEntityConfigKV(ctx context.Context, entityType, entityID string) (map[string]string, error)
	ReplaceEntityConfig(ctx context.Context, entityType, entityID string, config map[string]string) error
	SetEntityConfigKey(ctx context.Context, entityType, entityID, key, value string) error
	SetEntityConfigKeys(ctx context.Context, entityType, entityID string, values map[string]string) error
	DeleteEntityConfigKey(ctx context.Context, entityType, entityID, key string) error
	DeleteEntityConfig(ctx context.Context, entityType, entityID string) error

	ListDashboardRows(ctx context.Context, ownerType, ownerID string) ([]DashboardRow, error)
	GetDashboardRow(ctx context.Context, id int64) (*DashboardRow, error)
	CreateDashboard(ctx context.Context, input DashboardRow) (int64, error)
	UpdateDashboard(ctx context.Context, id int64, input DashboardRow) error
	DeleteDashboard(ctx context.Context, id int64) error

	ListSourceRows(ctx context.Context, teamID string) ([]SourceRow, error)
	GetSourceRow(ctx context.Context, teamID, sourceID string) (*SourceRow, error)
	CreateSource(ctx context.Context, teamID string, input SourceRow) error
	UpdateSource(ctx context.Context, teamID, sourceID string, input SourceRow) error
	DeleteSource(ctx context.Context, teamID, sourceID string) error

	GetProductOverrideRows(ctx context.Context, productID string) ([]OverrideRepoRow, error)
	ReplaceProductOverrides(ctx context.Context, productID string, overrides []OverrideRepoRow) error

	ListSubscriptionRows(ctx context.Context) ([]SubscriptionRow, error)
	GetSubscriptionRow(ctx context.Context, id int64) (*SubscriptionRow, error)
	CreateSubscription(ctx context.Context, input SubscriptionRow) (int64, error)
	UpdateSubscription(ctx context.Context, id int64, input SubscriptionRow) error
	UpdateSubscriptionLastRunAt(ctx context.Context, id int64, t time.Time) error
	DeleteSubscription(ctx context.Context, id int64) error

	ListAlertRows(ctx context.Context) ([]AlertRow, error)
	GetAlertRow(ctx context.Context, id int64) (*AlertRow, error)
	CreateAlert(ctx context.Context, input AlertRow) (int64, error)
	UpdateAlert(ctx context.Context, id int64, input AlertRow) error
	DeleteAlert(ctx context.Context, id int64) error

	ListCannedQueryRows(ctx context.Context) ([]CannedQueryRow, error)
	GetCannedQueryRow(ctx context.Context, id int64) (*CannedQueryRow, error)
	CreateCannedQuery(ctx context.Context, input CannedQueryRow) (int64, error)
	UpdateCannedQuery(ctx context.Context, id int64, input CannedQueryRow) error
	DeleteCannedQuery(ctx context.Context, id int64) error

	ListCannedScriptRows(ctx context.Context) ([]CannedScriptRow, error)
	GetCannedScriptRow(ctx context.Context, id int64) (*CannedScriptRow, error)
	CreateCannedScript(ctx context.Context, input CannedScriptRow) (int64, error)
	UpdateCannedScript(ctx context.Context, id int64, input CannedScriptRow) error
	DeleteCannedScript(ctx context.Context, id int64) error

	ListJsonSchemaRows(ctx context.Context) ([]JsonSchemaRow, error)
	GetJsonSchemaRow(ctx context.Context, id int64) (*JsonSchemaRow, error)
	CreateJsonSchema(ctx context.Context, input JsonSchemaRow) (int64, error)
	UpdateJsonSchema(ctx context.Context, id int64, input JsonSchemaRow) error
	DeleteJsonSchema(ctx context.Context, id int64) error
	GetJsonSchemaByName(ctx context.Context, name string) (*JsonSchemaRow, error)

	ListStoredProcedureRows(ctx context.Context, workflowID int64) ([]StoredProcedureRow, error)
	ListAllStoredProcedureRows(ctx context.Context) ([]StoredProcedureRow, error)
	GetStoredProcedureRow(ctx context.Context, id int64) (*StoredProcedureRow, error)
	CreateStoredProcedure(ctx context.Context, input StoredProcedureRow) (int64, error)
	UpdateStoredProcedure(ctx context.Context, id int64, input StoredProcedureRow) error
	DeleteStoredProcedure(ctx context.Context, id int64) error

	ListWorkflowRows(ctx context.Context) ([]WorkflowRow, error)
	GetWorkflowRow(ctx context.Context, id int64) (*WorkflowRow, error)
	GetWorkflowByName(ctx context.Context, name string) (*WorkflowRow, error)
	CreateWorkflow(ctx context.Context, input WorkflowRow) (int64, error)
	UpdateWorkflow(ctx context.Context, id int64, input WorkflowRow) error
	DeleteWorkflow(ctx context.Context, id int64) error
	SaveWorkflowDraft(ctx context.Context, id int64, input WorkflowRow) error
	PublishWorkflow(ctx context.Context, id int64, publishNote string) (*WorkflowRow, error)
	ListWorkflowVersions(ctx context.Context, workflowID int64) ([]WorkflowVersionRow, error)
	GetWorkflowVersion(ctx context.Context, workflowID int64, version int) (*WorkflowVersionRow, error)
	SetProductionVersion(ctx context.Context, workflowID int64, version int) error
	ArchiveWorkflow(ctx context.Context, id int64) error
	UnarchiveWorkflow(ctx context.Context, id int64) error
	GetWorkflowByNameAndVersion(ctx context.Context, name string, version int) (*WorkflowRow, error)
	GetProductionWorkflowByName(ctx context.Context, name string) (*WorkflowRow, error)

	GetOrgForProduct(ctx context.Context, productID string) (string, error)

	ValidateAPIKey(ctx context.Context, keyHash string) (*APIKeyInfo, error)
	ListAPIKeys(ctx context.Context) ([]APIKeyInfo, error)
	CreateAPIKey(ctx context.Context, name, keyHash string, isAdmin bool, scopedUser string) (int64, error)
	DeleteAPIKey(ctx context.Context, id int64) error

	ClearConfig(ctx context.Context) error
	ClearSeeds(ctx context.Context) error
	ClearWorkflows(ctx context.Context) error

	PingContext(ctx context.Context) error
}

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type MySQLStore struct {
	db      querier
	rawDB   *sql.DB
	inTx    bool
	secrets *secretBox
}

// NewMySQLStore connects to the configuration database. It assumes the
// schema is already at the version this binary expects -- schema changes are
// applied separately via Migrate (see cmd/migrate), not on every connect.
func NewMySQLStore(host string, port int, user, password, dbName, tlsCABundlePath string) (*MySQLStore, error) {
	db, err := OpenDB(host, port, user, password, dbName, tlsCABundlePath)
	if err != nil {
		return nil, err
	}

	slog.Info("database connected", "host", host, "port", port, "database", dbName)
	return &MySQLStore{db: db, rawDB: db}, nil
}

func (s *MySQLStore) Close() error {
	return s.rawDB.Close()
}

func (s *MySQLStore) RunInTransaction(ctx context.Context, fn func(*MySQLStore) error) error {
	tx, err := s.rawDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackTx(tx)
	txStore := &MySQLStore{db: tx, rawDB: s.rawDB, inTx: true, secrets: s.secrets}
	if err := fn(txStore); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *MySQLStore) Exec(ctx context.Context, query string, args ...any) error {
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *MySQLStore) beginOrReuse(ctx context.Context) (*sql.Tx, bool, error) {
	if s.inTx {
		return s.db.(*sql.Tx), false, nil
	}
	tx, err := s.rawDB.BeginTx(ctx, nil)
	return tx, true, err
}

func (s *MySQLStore) PingContext(ctx context.Context) error {
	return s.rawDB.PingContext(ctx)
}

func (s *MySQLStore) DB() *sql.DB {
	return s.rawDB
}
