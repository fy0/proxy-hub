package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"proxy-hub/model"

	"gorm.io/gorm/logger"
)

func initInMemoryDB(t *testing.T) {
	t.Helper()

	if err := model.InitWithDSN(":memory:", int(logger.Silent), true); err != nil {
		t.Fatalf("InitWithDSN(:memory:) failed: %v", err)
	}
	t.Cleanup(model.DBClose)
}

func TestSQLiteMemory_UserFlow(t *testing.T) {
	initInMemoryDB(t)

	ctx := context.Background()
	created, err := UserCreate(ctx, nil, "alice", "secret", "Alice", "", "", nil)
	if err != nil {
		t.Fatalf("UserCreate failed: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected user id to be set")
	}

	authed, err := UserAuthenticate(ctx, nil, "alice", "secret")
	if err != nil {
		t.Fatalf("UserAuthenticate failed: %v", err)
	}
	if authed.ID != created.ID {
		t.Fatalf("expected authenticated user id %q, got %q", created.ID, authed.ID)
	}

	if err := UserUpdatePassword(ctx, nil, created.ID, "newsecret"); err != nil {
		t.Fatalf("UserUpdatePassword failed: %v", err)
	}
	if _, err := UserAuthenticate(ctx, nil, "alice", "newsecret"); err != nil {
		t.Fatalf("UserAuthenticate with new password failed: %v", err)
	}
	if _, err := UserAuthenticate(ctx, nil, "alice", "secret"); err == nil {
		t.Fatalf("expected old password to be invalid")
	}

	tokenString, err := AccessTokenGenerateWithTTL(ctx, nil, created.ID, time.Hour)
	if err != nil {
		t.Fatalf("AccessTokenGenerateWithTTL failed: %v", err)
	}
	verified, err := AccessTokenVerify(ctx, nil, tokenString)
	if err != nil {
		t.Fatalf("AccessTokenVerify failed: %v", err)
	}
	if verified.ID != created.ID {
		t.Fatalf("expected token user id %q, got %q", created.ID, verified.ID)
	}
}

func TestSQLiteMemory_TransactionRollback(t *testing.T) {
	initInMemoryDB(t)

	ctx := context.Background()
	rollbackErr := errors.New("rollback")

	err := model.Transaction(ctx, func(tx model.DBTx) error {
		_, err := UserCreate(ctx, tx, "bob", "secret", "Bob", "", "", nil)
		if err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("expected rollback error, got %v", err)
	}

	user, err := UserGetByUsername(ctx, nil, "bob")
	if err != nil {
		t.Fatalf("UserGetByUsername failed: %v", err)
	}
	if user != nil {
		t.Fatalf("expected user not to persist after rollback")
	}
}

func TestSQLiteMemory_UserListFilters(t *testing.T) {
	initInMemoryDB(t)

	ctx := context.Background()
	u1, err := UserCreate(ctx, nil, "u1", "secret", "U1", "", "", nil)
	if err != nil {
		t.Fatalf("UserCreate u1 failed: %v", err)
	}
	_, err = UserCreate(ctx, nil, "u2", "secret", "U2", "", "", nil)
	if err != nil {
		t.Fatalf("UserCreate u2 failed: %v", err)
	}

	if _, err := UserDisable(ctx, nil, u1.ID, true); err != nil {
		t.Fatalf("UserDisable failed: %v", err)
	}

	users, total, err := UserList(ctx, nil, &UserListRequest{IncludeDisabled: false}, 1, 10)
	if err != nil {
		t.Fatalf("UserList failed: %v", err)
	}
	if total != 1 || len(users) != 1 {
		t.Fatalf("expected 1 enabled user, got total=%d len=%d", total, len(users))
	}

	users, total, err = UserList(ctx, nil, &UserListRequest{IncludeDisabled: true}, 1, 10)
	if err != nil {
		t.Fatalf("UserList include disabled failed: %v", err)
	}
	if total != 2 || len(users) != 2 {
		t.Fatalf("expected 2 users, got total=%d len=%d", total, len(users))
	}
}
