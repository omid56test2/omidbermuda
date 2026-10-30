#!/usr/bin/env bash
set -euo pipefail

IMAGE_TAG="${IMAGE_TAG:-omid-iran-bermuda:1.0.1}"
PORT="${DOCKER_SMOKE_PORT:-18082}"
NAME="omid-bermuda-smoke-${RANDOM}-$$"
TOKEN="$(python3 - <<'PY'
import secrets
print(secrets.token_hex(32))
PY
)"
BASE="http://127.0.0.1:${PORT}"

command -v docker >/dev/null 2>&1 || { echo 'Docker is required.' >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { echo 'curl is required.' >&2; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo 'python3 is required.' >&2; exit 1; }
docker image inspect "$IMAGE_TAG" >/dev/null

docker run -d --name "$NAME" \
  -p "127.0.0.1:${PORT}:8080" \
  -e ADMIN_TOKEN="$TOKEN" \
  -e PUBLIC_BASE_URL="https://docker-smoke.example.test" \
  -e XRAY_AUTOSTART=true \
  -e XRAY_AUTORESTART=true \
  "$IMAGE_TAG" >/dev/null

cleanup() {
  status=$?
  if [[ $status -ne 0 ]]; then
    echo 'Docker integration logs:' >&2
    docker logs "$NAME" >&2 || true
  fi
  docker rm -f "$NAME" >/dev/null 2>&1 || true
  exit "$status"
}
trap cleanup EXIT

ready=0
for _ in $(seq 1 60); do
  if curl -fsS "$BASE/ready" >/dev/null 2>&1; then ready=1; break; fi
  sleep 1
done
[[ "$ready" == 1 ]] || { echo 'Container did not become ready with Xray running.' >&2; exit 1; }
AUTH=( -H "Authorization: Bearer $TOKEN" )
curl -fsS "${AUTH[@]}" "$BASE/admin/xray" > /tmp/omid-bermuda-xray-state.json
python3 - <<'PY'
import json
x=json.load(open('/tmp/omid-bermuda-xray-state.json'))
assert x.get('running') is True, x
PY

CREATE="$(curl -fsS -X POST "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d '{"name":"Docker Smoke","inbound_ids":["bermuda-vless-xhttp","bermuda-vless-ws","bermuda-trojan-ws"],"expires_days":0,"limit_value":0,"limit_unit":"GB","ip_limit":1,"connection_limit":4,"speed_limit_value":0,"speed_limit_unit":"MBIT"}' \
  "$BASE/api/users")"
python3 - "$CREATE" <<'PY2'
import json,sys
created=json.loads(sys.argv[1])
assert created.get('xray_sync') == 'updated', created
PY2
SUB="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["sub_token"])' <<< "$CREATE")"
for _ in $(seq 1 30); do
  if curl -fsS "$BASE/ready" >/dev/null 2>&1; then break; fi
  sleep 1
done
curl -fsS "$BASE/ready" >/dev/null
curl -fsS -H 'Accept: text/plain' "$BASE/sub/$SUB" | python3 -c 'import base64,sys; lines=[x for x in base64.b64decode(sys.stdin.buffer.read()).decode().splitlines() if x]; assert len(lines)==3 and sum(x.startswith("vless://") for x in lines)==2 and sum(x.startswith("trojan://") for x in lines)==1'

echo 'Docker integration passed: image starts Xray, readiness is healthy, user config update restarts Xray, and subscription has three client URIs.'
