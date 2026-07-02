#!/bin/sh
# greet - the tiny CLI ward's example repo wraps. Deliberately dependency-free
# (pure POSIX sh) so `ward exec build|test|vet` run anywhere with no toolchain.
# See ../../docs/example-repo.md.
name="${1:-world}"
printf 'hello, %s\n' "$name"
