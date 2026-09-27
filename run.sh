#!/usr/bin/env sh
# advance-wars — home-lab launch (NEX secure server). cert.pem/key.pem and
# nextendo_secret.key ship on this testing branch, so it runs from the repo.
# AUTH_PORT is the HTTPS auth port the console reaches (443 in production, behind
# the sni-router). Binding 443 needs privilege; override AUTH_PORT for a plain
# local run, e.g. AUTH_PORT=8443 ./run.sh, or grant it:
#   sudo setcap 'cap_net_bind_service=+ep' $(command -v go)   # dev only
set -e
export CERT_FILE="${CERT_FILE:-cert.pem}"
export KEY_FILE="${KEY_FILE:-key.pem}"
export AUTH_PORT="${AUTH_PORT:-443}"
export SECURE_PORT="${SECURE_PORT:-60015}"
export DASH_PORT="${DASH_PORT:-8097}"
export NEXTENDO_SECRET_FILE="${NEXTENDO_SECRET_FILE:-nextendo_secret.key}"
echo "[advance-wars] auth :$AUTH_PORT  secure udp :$SECURE_PORT  dash :$DASH_PORT"
exec go run .
