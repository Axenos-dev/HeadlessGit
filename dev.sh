#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

usage() {
	echo "usage: ./dev.sh <command>"
	echo ""
	echo "commands:"
	echo "  up     build and start the stack (docker compose up --build)"
	echo "  gen    generate sqlc code (sqlc generate)"
}

case "${1:-}" in
up)
	docker compose up --build
	;;
gen)
	sqlc generate
	;;
*)
	usage
	exit 1
	;;
esac
