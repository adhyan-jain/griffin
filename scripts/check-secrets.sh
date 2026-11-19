#!/usr/bin/env bash
# Secret scanner for staged changes. Blocks sensitive file names and added lines that look like
# credentials. Usage:
#   scripts/check-secrets.sh            scan the staged diff (used by .githooks/pre-commit)
#   scripts/check-secrets.sh --all      scan every tracked file (CI / manual audit)
#   scripts/check-secrets.sh --stdin    scan a unified diff piped on stdin (self-test)
# A line containing "secret-scan:allow" is skipped (use sparingly, with a reason nearby).
set -u

mode="${1:-staged}"

# Sensitive file names (matched on the basename / path of each added or changed file).
bad_name_re='(^|/)([^/]*\.)?env(\.[^/]*)?$|(^|/)(id_rsa.*|id_ed25519.*|credentials\.json|client_secret.*\.json|service[-_]account.*\.json|local\.properties)$|\.(pem|key|p12|pfx|jks|keystore)$'

# High-confidence credential shapes, checked in every file.
strong_re='-----BEGIN ([A-Z]+ )?PRIVATE KEY-----|AKIA[0-9A-Z]{16}|ASIA[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{35}|gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,}|sk-ant-[A-Za-z0-9_-]{20,}|sk-[A-Za-z0-9]{32,}|xox[baprs]-[A-Za-z0-9-]{10,}|eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}|tk_[a-z0-9]{29}|AC[0-9a-f]{32}|postgres(ql)?://[^:/@[:space:]]+:[^@[:space:]]{6,}@[^/[:space:]]*[.a-z]'

# Lower-confidence: a secret-looking name assigned a long literal. Skipped in tests/fixtures.
generic_re='(secret|token|passw(or)?d|api[_-]?key|private[_-]?key)[A-Za-z0-9_]*["'"'"']?[[:space:]]*(:=|=|:)[[:space:]]*["'"'"'][A-Za-z0-9/+_=-]{20,}["'"'"']'

ignored() { [ -f .secret-scan-ignore ] && grep -Fxq -- "$1" <(grep -v '^#' .secret-scan-ignore); }

fail=0
report() { echo "ERROR: [secrets] $1" >&2; fail=1; }

scan_diff() {
  local file="" line
  while IFS= read -r line; do
    case "$line" in
      '+++ b/'*) file="${line#+++ b/}"; continue ;;
      '+++ '*|'--- '*|'@@'*|'diff '*|'index '*) continue ;;
      '+'*) ;;
      *) continue ;;
    esac
    body="${line#+}"
    ignored "$file" && continue
    [[ "$body" == *"secret-scan:allow"* ]] && continue
    case "$file" in *.lock|*package-lock.json|*go.sum|*node_modules/*) continue ;; esac
    if printf '%s' "$body" | grep -Eq -- "$strong_re"; then
      # Local throwaway databases in docs/tests (localhost) are not secrets.
      if printf '%s' "$body" | grep -Eq 'postgres(ql)?://[^@]*@(localhost|127\.0\.0\.1|db|postgres)([:/]|$)' \
         && ! printf '%s' "$body" | grep -Eq -- '-----BEGIN|AKIA|AIza|gh[pousr]_|sk-|xox|eyJ'; then
        continue
      fi
      report "possible credential in $file: $(printf '%s' "$body" | cut -c1-60)..."
      continue
    fi
    case "$file" in *_test.go|*/testdata/*|*Test.kt|*/test/*|*.example) continue ;; esac
    if printf '%s' "$body" | grep -Eiq -- "$generic_re"; then
      report "secret-looking literal assigned in $file: $(printf '%s' "$body" | cut -c1-60)..."
    fi
  done
}

check_names() {
  local f
  while IFS= read -r f; do
    [ -z "$f" ] && continue
    case "$f" in *.env.example|.env.example) continue ;; esac
    if printf '%s' "$f" | grep -Eq -- "$bad_name_re"; then
      report "sensitive file name staged: $f"
    fi
  done
}

case "$mode" in
  --stdin) scan_diff ;;
  --all)
    check_names < <(git ls-files)
    # file:line only, never the matched text, so an audit does not echo secrets into logs.
    while IFS=: read -r f n _; do
      [ -z "$f" ] && continue
      ignored "$f" && continue
      line="$(sed -n "${n}p" "$f")"
      [[ "$line" == *"secret-scan:allow"* ]] && continue
      printf '%s' "$line" | grep -Eq 'postgres(ql)?://[^@]*@(localhost|127\.0\.0\.1|db|postgres)([:/]|$)' \
        && ! printf '%s' "$line" | grep -Eq -- '-----BEGIN|AKIA|AIza|gh[pousr]_|sk-|xox|eyJ' && continue
      report "possible credential at $f:$n"
    done < <(git grep -nIE -e "$strong_re" -- ':!*.lock' ':!*package-lock.json' ':!*go.sum' ':!*/node_modules/*' ':!scripts/check-secrets*.sh' | cut -d: -f1,2)
    ;;
  *)
    # Process substitution (not a pipe) so the failure flag set in the loop is not lost in a subshell.
    check_names < <(git diff --cached --name-only --diff-filter=ACMR)
    scan_diff < <(git diff --cached --no-color -U0 --diff-filter=ACMR --src-prefix=a/ --dst-prefix=b/)
    ;;
esac

if [ "$fail" -ne 0 ]; then
  echo "Commit blocked. Remove the secret (rotate it if it was ever pushed) or, for a verified false" >&2
  echo "positive, add 'secret-scan:allow' to that line." >&2
  exit 1
fi
exit 0
