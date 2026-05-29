package provisioner

import (
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// TestRollbackPostgres verifies that rollbackPostgres issues both a
// DROP DATABASE and a DROP USER statement so that no orphaned resources are
// left behind when provisioning fails after the database and user are created.
func TestRollbackPostgres(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectExec(regexp.QuoteMeta(`DROP DATABASE IF EXISTS "appdb"`)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`DROP USER IF EXISTS "appuser"`)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	rollbackPostgres(db, "appdb", "appuser")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("rollback did not issue the expected statements: %v", err)
	}
}

// TestRollbackPostgresQuotesIdentifiers ensures that rollback identifiers are
// safely escaped via quoteIdentifier(), preventing SQL injection through
// malicious database or user names.
func TestRollbackPostgresQuotesIdentifiers(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	// A name containing a double quote must be doubled when escaped.
	mock.ExpectExec(regexp.QuoteMeta(`DROP DATABASE IF EXISTS "ev""il"`)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`DROP USER IF EXISTS "ro""ot"`)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	rollbackPostgres(db, `ev"il`, `ro"ot`)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("rollback did not escape identifiers as expected: %v", err)
	}
}

// TestRollbackPostgresContinuesOnDropDatabaseError verifies that a failure to
// drop the database does not prevent the user from also being dropped.
func TestRollbackPostgresContinuesOnDropDatabaseError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectExec(regexp.QuoteMeta(`DROP DATABASE IF EXISTS "appdb"`)).
		WillReturnError(errors.New("database in use"))
	mock.ExpectExec(regexp.QuoteMeta(`DROP USER IF EXISTS "appuser"`)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	rollbackPostgres(db, "appdb", "appuser")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("rollback did not attempt to drop the user after a database drop failure: %v", err)
	}
}
