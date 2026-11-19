#!/usr/bin/env bash
# Self-test for check-secrets.sh. Fake credentials are assembled at runtime so this file itself
# does not contain secret-shaped literals.
set -u
here="$(cd "$(dirname "$0")" && pwd)"
scan="$here/check-secrets.sh"
fails=0

mk() { printf 'diff --git a/%s b/%s\n--- /dev/null\n+++ b/%s\n@@ -0,0 +1 @@\n+%s\n' "$1" "$1" "$1" "$2"; }
expect() { # expect <block|pass> <file> <line>
  mk "$2" "$3" | "$scan" --stdin >/dev/null 2>&1
  rc=$?
  if { [ "$1" = block ] && [ $rc -eq 0 ]; } || { [ "$1" = pass ] && [ $rc -ne 0 ]; }; then
    echo "FAIL: expected $1 for $2: $3"; fails=$((fails+1))
  fi
}

aws="AKIA$(printf 'A1B2C3D4E5F6G7H8')"
google="AIza$(printf 'SyA1B2C3D4E5F6G7H8I9J0K1L2M3N4O5P6Q')"
ghp="ghp_$(printf 'a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6q7R8')"
pem="-----BEGIN $(printf 'RSA') PRIVATE KEY-----"
jwt="eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abcdefghijk123"

expect block main.go "x := \"$aws\""
expect block notes.md "key $google"
expect block cfg.yaml "token: $ghp"
expect block a.txt "$pem"
expect block a.go "var t = \"$jwt\""
expect block a.go 'apiKey := "abcdefghijklmnopqrstuvwx1234"'
expect block a.env 'POSTGRES_URL=postgres://griffin:Sup3rS3cretPw@db.example.com/app'
expect pass a.go 'x := os.Getenv("API_KEY")'
expect pass a.go 'apiKey := "short"'
expect pass a_test.go 'token := "abcdefghijklmnopqrstuvwx1234"'
expect pass docs.md 'url postgres://postgres:postgres@localhost:5432/griffin_test'
expect pass a.go "k := \"$aws\" // secret-scan:allow documented fake"

# File-name rules via the staged path (--stdin only scans content; test names separately).
for n in .env prod.env.local server.pem id_rsa deploy.key; do
  printf '%s\n' "$n" | bash -c 'source <(sed -n "/^bad_name_re=/p;/^fail=0/p;/^report()/p;/^check_names()/,/^}/p" "'"$scan"'"); check_names; exit $fail' >/dev/null 2>&1
  [ $? -eq 0 ] && { echo "FAIL: file name $n not blocked"; fails=$((fails+1)); }
done
printf '.env.example\n' | bash -c 'source <(sed -n "/^bad_name_re=/p;/^fail=0/p;/^report()/p;/^check_names()/,/^}/p" "'"$scan"'"); check_names; exit $fail' >/dev/null 2>&1 || { echo "FAIL: .env.example wrongly blocked"; fails=$((fails+1)); }

if [ $fails -eq 0 ]; then echo "check-secrets self-test: all passed"; else echo "$fails failure(s)"; exit 1; fi
