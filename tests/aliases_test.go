package tests

import (
	"context"
	"testing"

	"github.com/griffin/core/internal/repository"
)

func TestAliasesStoredPerOwner(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	a := CreateTestUser(t, env.DB, "alias-a@example.com")
	b := CreateTestUser(t, env.DB, "alias-b@example.com")
	repo := repository.NewPostgresAliasRepository(env.DB.Pool)

	if err := repo.Put(ctx, a, "mummy", "mom"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Put(ctx, a, "mummy", "mother"); err != nil { // replaces
		t.Fatal(err)
	}
	if err := repo.Put(ctx, b, "mummy", "amma"); err != nil {
		t.Fatal(err)
	}
	ma, _ := repo.List(ctx, a)
	mb, _ := repo.List(ctx, b)
	if len(ma) != 1 || ma["mummy"] != "mother" || mb["mummy"] != "amma" {
		t.Fatalf("a=%v b=%v", ma, mb)
	}
	if ok, err := repo.Delete(ctx, a, "mummy"); err != nil || !ok {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if ok, _ := repo.Delete(ctx, a, "mummy"); ok {
		t.Fatal("second delete must report not found")
	}
	if mb, _ = repo.List(ctx, b); mb["mummy"] != "amma" {
		t.Fatal("the other owner's alias must survive")
	}
}
