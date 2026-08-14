#!/bin/bash
# NCP API caller — signature v2 (HMAC-SHA256)
#
#   export NCLOUD_ACCESS_KEY=... NCLOUD_SECRET_KEY=...
#   ./ncp-api.sh GET  vpc "/vserver/v2/getRegionList?responseFormatType=json"
#   ./ncp-api.sh GET  cw  "/cw_fea/real/cw/api/schema/system/list"
#   ./ncp-api.sh POST cw  "/cw_fea/real/cw/api/rule/group/metric/search" '{"prodKey":"...","query":""}'
#
# host: vpc | cw | full URL      DEBUG=1 to dump the signing message

set -uo pipefail

usage() { sed -n '2,9p' "$0" | sed 's/^# \{0,1\}//'; exit 1; }
[ $# -ge 3 ] || usage

METHOD=$1
HOST=$2
URI=$3
BODY=${4:-}

: "${NCLOUD_ACCESS_KEY:?export NCLOUD_ACCESS_KEY first}"
: "${NCLOUD_SECRET_KEY:?export NCLOUD_SECRET_KEY first}"

case $HOST in
  vpc) HOST=https://ncloud.apigw.ntruss.com ;;
  cw)  HOST=https://cw.apigw.ntruss.com ;;
esac

[ "${URI:0:1}" = "/" ] || { echo "URI must start with / (got: $URI)" >&2; exit 1; }

TS=$(($(date +%s) * 1000))

# message = "{METHOD} {URI}\n{timestamp}\n{accessKey}" — URI must match the wire URI exactly
MSG=$(printf '%s %s\n%s\n%s' "$METHOD" "$URI" "$TS" "$NCLOUD_ACCESS_KEY")
SIG=$(printf '%s' "$MSG" | openssl dgst -sha256 -hmac "$NCLOUD_SECRET_KEY" -binary | openssl base64 -A)

if [ "${DEBUG:-}" = "1" ]; then
  { echo "--- signing message ---"; printf '%s\n' "$MSG"; echo "--- signature ---"; echo "$SIG"
    echo "--- request ---"; echo "$METHOD ${HOST}${URI}"; [ -n "$BODY" ] && echo "$BODY"; } >&2
fi

ARGS=(-sS -X "$METHOD" "${HOST}${URI}"
  -H "x-ncp-apigw-timestamp: $TS"
  -H "x-ncp-iam-access-key: $NCLOUD_ACCESS_KEY"
  -H "x-ncp-apigw-signature-v2: $SIG")
[ -n "$BODY" ] && ARGS+=(-H 'Content-Type: application/json' -d "$BODY")

OUT=$(mktemp)
trap 'rm -f "$OUT"' EXIT

CODE=$(curl "${ARGS[@]}" -o "$OUT" -w '%{http_code}')

if command -v jq >/dev/null 2>&1 && jq -e . "$OUT" >/dev/null 2>&1; then
  jq . "$OUT"
else
  cat "$OUT"
fi

echo "[http $CODE]" >&2
[ "$CODE" -ge 200 ] && [ "$CODE" -lt 300 ]
