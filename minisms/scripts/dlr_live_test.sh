#!/usr/bin/env bash
# Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
#
# One-shot live DLR test: sends the 5 handover test messages through MiniSMS
# (POST /api/v1/sms/send), then polls delivery-receipt status for each.
#
# This sends REAL, billable SMS through the live carrier/Kamex gateway. Run it
# only against a host you intend to bill, with real credentials supplied via env.
#
# Requirements: bash, curl, jq.
# Usage:
#   export MINISMS_BASE_URL="https://<your-minisms-host>"   # no trailing slash
#   export MINISMS_API_KEY="<client-api-key>"
#   ./dlr_live_test.sh
set -euo pipefail

: "${MINISMS_BASE_URL:?set MINISMS_BASE_URL, e.g. https://minisms.example.com}"
: "${MINISMS_API_KEY:?set MINISMS_API_KEY to a client API key}"

DEST_PRIMARY="+243993873999"
DEST_SECONDARY="+243982821454"

# from|to per the handover test matrix
TESTS=(
  "ACME|${DEST_PRIMARY}"
  "+17725216279|${DEST_PRIMARY}"
  "+17865865545|${DEST_PRIMARY}"
  "Zaz.Bet|${DEST_PRIMARY}"
  "ACME|${DEST_SECONDARY}"
)

send_one() {
  local from="$1" to="$2"
  curl --fail-with-body -sS -X POST "${MINISMS_BASE_URL}/api/v1/sms/send" \
    -H "X-API-Key: ${MINISMS_API_KEY}" \
    -H "Content-Type: application/json" \
    --data "$(jq -nc --arg from "$from" --arg to "$to" \
      '{from:$from, to:$to, message:"MiniSMS DLR test", dlr:"YES"}')"
}

declare -a IDS FROMS TOS
echo "== Sending 5 messages (~5s apart) =="
for i in "${!TESTS[@]}"; do
  IFS='|' read -r from to <<<"${TESTS[$i]}"
  printf '#%d  from=%-14s to=%s ... ' "$((i+1))" "$from" "$to"
  if resp="$(send_one "$from" "$to")"; then
    id="$(jq -r '.message_id // empty' <<<"$resp")"
    echo "202 message_id=${id:-<none>}"
    IDS+=("$id"); FROMS+=("$from"); TOS+=("$to")
  else
    echo "FAILED -> $resp"
    IDS+=(""); FROMS+=("$from"); TOS+=("$to")
  fi
  [ "$i" -lt 4 ] && sleep 5 || true
done

echo
echo "== Polling DLR status (up to ~60s) =="
for attempt in 1 2 3 4 5 6; do
  pending=0
  echo "-- poll #${attempt} --"
  for i in "${!IDS[@]}"; do
    id="${IDS[$i]}"
    [ -z "$id" ] && { printf '#%d  %-14s -> send failed\n' "$((i+1))" "${FROMS[$i]}"; continue; }
    s="$(curl --fail-with-body -sS "${MINISMS_BASE_URL}/api/v1/sms/status/${id}" \
          -H "X-API-Key: ${MINISMS_API_KEY}")" || { echo "#$((i+1)) status query failed"; pending=1; continue; }
    status="$(jq -r '.status // "?"' <<<"$s")"
    dlr="$(jq -r '.dlr_status // "none"' <<<"$s")"
    rcv="$(jq -r '.dlr_received_at // "-"' <<<"$s")"
    printf '#%d  %-14s status=%-10s dlr_status=%-12s dlr_received_at=%s\n' \
      "$((i+1))" "${FROMS[$i]}" "$status" "$dlr" "$rcv"
    [ "$dlr" = "none" ] || [ "$dlr" = "unknown" ] && pending=1 || true
  done
  [ "$pending" -eq 0 ] && { echo "All messages have a final DLR."; break; }
  [ "$attempt" -lt 6 ] && sleep 10 || true
done

echo
echo "Done. Cross-check FIDs in the gateway access.log; every send should show a 'Sent SMS' and a later 'Receive DLR' line."
