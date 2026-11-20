package browser

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var hostSuffixRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)

// cookieDB is where a Chrome profile keeps its cookies (newer Chrome: Network/Cookies, older: Cookies).
func cookieDB(profile string) (string, error) {
	for _, p := range []string{filepath.Join(profile, "Network", "Cookies"), filepath.Join(profile, "Cookies")} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no cookie database in %s", profile)
}

// BorrowCookies copies the login cookies of the given sites (and nothing else) from one Chrome profile into
// another, so Griffin's own browser is signed in wherever the owner already is. The source is only read.
// The destination browser must not be running. Values stay encrypted as they were: the destination Chrome
// must use the same system keyring (ChromeConfig.SystemKeyring) to read them.
func BorrowCookies(srcProfile, dstProfile string, hostSuffixes []string) (int, error) {
	if len(hostSuffixes) == 0 {
		return 0, fmt.Errorf("say which sites to borrow")
	}
	var conds []string
	for _, h := range hostSuffixes {
		if !hostSuffixRe.MatchString(h) {
			return 0, fmt.Errorf("refusing odd site name %q", h)
		}
		conds = append(conds, fmt.Sprintf("host_key LIKE '%%%s'", h))
	}
	where := strings.Join(conds, " OR ")
	src, err := cookieDB(srcProfile)
	if err != nil {
		return 0, err
	}
	if _, err := os.Lstat(filepath.Join(dstProfile, "SingletonLock")); err == nil {
		return 0, fmt.Errorf("Griffin's browser profile is open; close it first")
	}
	dst, err := cookieDB(filepath.Join(dstProfile, "Default"))
	if err != nil {
		return 0, fmt.Errorf("Griffin's browser profile has no cookie database yet (open it once with browser-login)")
	}
	// The source is attached read-only and immutable, so a running Chrome is never disturbed.
	script := fmt.Sprintf(`ATTACH 'file:%s?mode=ro&immutable=1' AS s;
DELETE FROM main.cookies WHERE %s;
INSERT INTO main.cookies SELECT * FROM s.cookies WHERE %s;
SELECT changes();`, strings.ReplaceAll(src, "'", "''"), where, where)
	out, err := exec.Command("sqlite3", "-cmd", ".parameter init", dst, script).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("could not copy the cookies: %v: %s", err, strings.TrimSpace(string(out)))
	}
	var n int
	fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &n)
	return n, nil
}

// ClearStaleLock removes the lock files Chrome leaves in a profile folder when it was killed. It only does
// so when the process named in the lock is gone, so a profile that really is open is never touched.
// It reports whether the profile is free to use.
func ClearStaleLock(profile string) bool {
	lock := filepath.Join(profile, "SingletonLock")
	target, err := os.Readlink(lock)
	if err != nil {
		_, statErr := os.Lstat(lock)
		return statErr != nil // no lock, or a lock we cannot read
	}
	var pid int
	if i := strings.LastIndex(target, "-"); i >= 0 {
		fmt.Sscanf(target[i+1:], "%d", &pid)
	}
	if pid > 0 {
		if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err == nil {
			return false // that Chrome is still running
		}
	}
	for _, f := range []string{"SingletonLock", "SingletonCookie", "SingletonSocket"} {
		_ = os.Remove(filepath.Join(profile, f))
	}
	return true
}
