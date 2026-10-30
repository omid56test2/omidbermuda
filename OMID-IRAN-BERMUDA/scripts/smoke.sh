#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
for tool in curl python3; do command -v "$tool" >/dev/null 2>&1 || { echo "Missing required tool: $tool" >&2; exit 1; }; done
PORT="${SMOKE_PORT:-18081}"
TMP="$(mktemp -d)"
TOKEN="$(python3 - <<'PY'
import secrets
print(secrets.token_hex(32))
PY
)"
BIN="$TMP/omid-iran-bermuda"
go build -o "$BIN" ./cmd/server
PORT="$PORT" ADMIN_TOKEN="$TOKEN" DATA_FILE="$TMP/state.json" XRAY_CONFIG="$TMP/xray.json" PUBLIC_BASE_URL="https://smoke.example.test" \
  "$BIN" >"$TMP/server.log" 2>&1 &
PID=$!
cleanup(){ kill -TERM "$PID" 2>/dev/null || true; wait "$PID" 2>/dev/null || true; rm -rf "$TMP"; }
trap cleanup EXIT
BASE="http://127.0.0.1:$PORT"
for _ in $(seq 1 60); do if curl -fsS "$BASE/health" >/dev/null 2>&1; then break; fi; sleep .2; done
curl -fsS "$BASE/health" >/dev/null
code="$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/api/users")"
[[ "$code" == "401" ]] || { echo "Expected unauthenticated API 401; got $code" >&2; exit 1; }
AUTH=( -H "Authorization: Bearer $TOKEN" )
INBOUNDS="$(curl -fsS "${AUTH[@]}" "$BASE/api/inbounds")"
python3 - "$INBOUNDS" <<'PY'
import json,sys
x=json.loads(sys.argv[1])['inbounds']
assert len(x)==3 and all(i['listen']=='127.0.0.1' for i in x)
PY
CREATE="$(curl -fsS -X POST "$BASE/api/users" "${AUTH[@]}" -H 'Content-Type: application/json' -d '{"name":"Smoke User","inbound_ids":["bermuda-vless-xhttp","bermuda-vless-ws","bermuda-trojan-ws"],"expires_days":0,"limit_value":0,"limit_unit":"GB","ip_limit":1,"connection_limit":4,"speed_limit_value":0,"speed_limit_unit":"MBIT"}')"
SUB="$(python3 -c 'import json,sys;print(json.load(sys.stdin)["sub_token"])' <<< "$CREATE")"
USER="$(python3 -c 'import json,sys;print(json.load(sys.stdin)["user_id"])' <<< "$CREATE")"
curl -fsS -H 'Accept: text/plain' "$BASE/sub/$SUB" > "$TMP/sub.b64"
python3 - "$TMP/sub.b64" <<'PY'
import base64,sys
raw=base64.b64decode(open(sys.argv[1],'rb').read()).decode()
lines=[x for x in raw.splitlines() if x]
assert len(lines)==3, (len(lines), raw)
assert any(x.startswith('vless://') for x in lines)
assert any(x.startswith('trojan://') for x in lines)
PY
curl -fsS -H 'Accept: text/html' "$BASE/sub/$SUB" > "$TMP/sub.html"
grep -q 'نمایش QR' "$TMP/sub.html"
curl -fsS "$BASE/assets/bermuda-qr.js" >/dev/null
curl -fsS "${AUTH[@]}" "$BASE/admin/backup" > "$TMP/backup.json"
curl -fsS -X POST "$BASE/admin/restore" "${AUTH[@]}" -H 'Content-Type: application/json' --data-binary "@$TMP/backup.json" >/dev/null
curl -fsS "${AUTH[@]}" "$BASE/admin/system" >/dev/null
curl -fsS "${AUTH[@]}" "$BASE/api/users" > "$TMP/users.json"
python3 - "$TMP/users.json" "$USER" "$TMP/xray.json" <<'PY'
import json,sys
users=json.load(open(sys.argv[1]))['users']
u=next(x for x in users if x['user_id']==sys.argv[2])
for c in u.get('configs',[]):
    assert not c.get('secret') and not c.get('route_token')
cfg=json.load(open(sys.argv[3]))
assert len(cfg['inbounds'])==3
assert cfg['api']['listen']=='127.0.0.1:10085'
assert all(i['listen']=='127.0.0.1' for i in cfg['inbounds'])
PY
ROUTE="$(python3 - "$TMP/state.json" "$USER" <<'PY'
import json,sys
s=json.load(open(sys.argv[1]));u=s['users'][sys.argv[2]]
print(u['configs'][0]['route_token'])
PY
)"
valid="$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/connect/$ROUTE")"
unknown="$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/connect/not-a-route")"
[[ "$valid" == "502" ]] || { echo "Expected known route to reach absent loopback Xray (502); got $valid" >&2; exit 1; }
[[ "$unknown" == "404" ]] || { echo "Expected unknown route 404; got $unknown" >&2; exit 1; }
echo 'HTTP smoke test passed: auth, users, three inbounds, subscription, QR asset, backup/restore, runtime metrics, config and Gateway route matching.'
