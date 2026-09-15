package onlineusers

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestGroupAccessUsesExactIndexedRelationQuery(t *testing.T) {
	service, mock, closeDB := newMockService(t, time.Now())
	defer closeDB()

	mock.ExpectQuery(regexp.QuoteMeta(groupAccessQuery)).
		WithArgs(int64(11), int64(22)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "status", "authorized"}).
			AddRow(int64(1), "one", "one@example.test", "active", true).
			AddRow(int64(2), "", "two@example.test", "disabled", false))

	users, err := service.ListGroupAccessUsers(context.Background(), 22, 11)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 || users[0].ID != 1 || !users[0].Authorized || users[1].Authorized || users[1].Status != "disabled" {
		t.Fatalf("unexpected group access users: %#v", users)
	}
	assertExpectations(t, mock)
}

func TestGroupAccessRejectsMissingDatabaseAndInvalidIDs(t *testing.T) {
	service, err := Open("", discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListGroupAccessUsers(context.Background(), 2, 1); err == nil {
		t.Fatal("missing database was accepted")
	}

	configured, _, closeDB := newMockService(t, time.Now())
	defer closeDB()
	if _, err := configured.ListGroupAccessUsers(context.Background(), 0, 1); err == nil {
		t.Fatal("invalid target group was accepted")
	}
}
