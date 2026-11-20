package browser

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func sqlite(t *testing.T, db, sql string) string {
	t.Helper()
	out, err := exec.Command("sqlite3", db, sql).CombinedOutput()
	if err != nil {
		t.Fatalf("sqlite3: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestBorrowCopiesOnlyTheNamedSitesAndLeavesTheSourceAlone(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
	for _, d := range []string{filepath.Join(src, "Network"), filepath.Join(dst, "Default")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	schema := "CREATE TABLE cookies(host_key TEXT, name TEXT, value BLOB);"
	srcDB, dstDB := filepath.Join(src, "Network", "Cookies"), filepath.Join(dst, "Default", "Cookies")
	sqlite(t, srcDB, schema+"INSERT INTO cookies VALUES('.linkedin.com','li_at','new'),('.bank.example','sid','secret'),('.google.com','SID','g');")
	sqlite(t, dstDB, schema+"INSERT INTO cookies VALUES('.linkedin.com','li_at','old'),('.keep.example','k','v');")

	n, err := BorrowCookies(src, dst, []string{"linkedin.com"})
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if got := sqlite(t, dstDB, "SELECT value FROM cookies WHERE host_key='.linkedin.com'"); got != "new" {
		t.Fatalf("the signed-in cookie must replace the old one, got %q", got)
	}
	if got := sqlite(t, dstDB, "SELECT count(*) FROM cookies WHERE host_key IN ('.bank.example','.google.com')"); got != "0" {
		t.Fatal("only the named sites may be copied")
	}
	if got := sqlite(t, dstDB, "SELECT count(*) FROM cookies WHERE host_key='.keep.example'"); got != "1" {
		t.Fatal("other cookies already in Griffin's profile must stay")
	}
	if got := sqlite(t, srcDB, "SELECT count(*) FROM cookies"); got != "3" {
		t.Fatal("the source must not change")
	}
}

func TestBorrowRefusesOddSiteNamesAndAnOpenProfile(t *testing.T) {
	if _, err := BorrowCookies("a", "b", []string{"x'; DROP TABLE cookies; --"}); err == nil {
		t.Fatal("a site name with quotes must be refused")
	}
	if _, err := BorrowCookies("a", "b", nil); err == nil {
		t.Fatal("no sites is an error")
	}
}

func TestClearStaleLockOnlyRemovesADeadChromesLock(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink("host-99999999", filepath.Join(dir, "SingletonLock")); err != nil {
		t.Fatal(err)
	}
	if !ClearStaleLock(dir) {
		t.Fatal("a lock naming a dead process is stale")
	}
	if _, err := os.Lstat(filepath.Join(dir, "SingletonLock")); err == nil {
		t.Fatal("the stale lock must be removed")
	}
	live := t.TempDir()
	if err := os.Symlink("host-"+itoa(os.Getpid()), filepath.Join(live, "SingletonLock")); err != nil {
		t.Fatal(err)
	}
	if ClearStaleLock(live) {
		t.Fatal("a lock naming a running process is not stale")
	}
	if _, err := os.Lstat(filepath.Join(live, "SingletonLock")); err != nil {
		t.Fatal("a live lock must be left alone")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
