#!/bin/sh
# test - assert greet's output, exiting non-zero on mismatch so `ward exec test`
# fails loudly and the audit row reflects a real pass/fail.
set -eu
got="$(sh greet.sh ward)"
want="hello, ward"
if [ "$got" != "$want" ]; then
	printf 'FAIL: got "%s" want "%s"\n' "$got" "$want" >&2
	exit 1
fi
echo "ok - 1 test passed"
