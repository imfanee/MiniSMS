#!/usr/bin/env bash
# Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
#
# minisms-monitor: a lightweight health daemon for MiniSMS. On a fixed interval it inspects the
# service, SMPP binds, DLR health, send-queue depth, recent errors and disk, then appends a timestamped
# findings block (OK / WARN / ALERT) to a log you can tail at any time. No secrets are written.
#
# Config (all optional, read from the environment or the MiniSMS env file):
#   MONITOR_ENV_FILE   MiniSMS env file to source DATABASE_URL/PORT/... from (default /etc/minisms/minisms.env)
#   MONITOR_LOG        output log file (default /var/log/minisms/monitor.log)
#   MONITOR_INTERVAL   seconds between checks (default 300)
#   MONITOR_MAX_BYTES  rotate the log at this size (default 20MB), keeping 3 generations
#   MONITOR_UNIT       systemd unit to inspect (default minisms)
#   MONITOR_QUEUE_WARN warn when queued backlog exceeds this (default 1000)
set -uo pipefail

ENV_FILE="${MONITOR_ENV_FILE:-/etc/minisms/minisms.env}"
LOG="${MONITOR_LOG:-/var/log/minisms/monitor.log}"
INTERVAL="${MONITOR_INTERVAL:-300}"
MAX_BYTES="${MONITOR_MAX_BYTES:-$((20*1024*1024))}"
UNIT="${MONITOR_UNIT:-minisms}"
QUEUE_WARN="${MONITOR_QUEUE_WARN:-1000}"
STATE_DIR="$(dirname "$LOG")/.monitor_state"

# Pull only the keys we need from the env file, without exporting the whole (secret-bearing) file.
getenv() { grep -oE "^$1=.*" "$ENV_FILE" 2>/dev/null | head -1 | cut -d= -f2-; }
PGURL="$(getenv DATABASE_URL)"
PORT="$(getenv PORT)"; PORT="${PORT:-8080}"
INGRESS_ADDR="$(getenv SMPP_LISTEN_ADDR)"; INGRESS_PORT="${INGRESS_ADDR##*:}"; INGRESS_PORT="${INGRESS_PORT:-2775}"
WIRE_DIR="$(getenv WIRE_LOG_DIR)"; WIRE_DIR="${WIRE_DIR:-/var/log/minisms}"

mkdir -p "$STATE_DIR" 2>/dev/null || true

rotate() {
  [ -f "$LOG" ] || return 0
  local sz; sz=$(stat -c%s "$LOG" 2>/dev/null || echo 0)
  if [ "$sz" -gt "$MAX_BYTES" ]; then
    rm -f "$LOG.3"; mv -f "$LOG.2" "$LOG.3" 2>/dev/null || true
    mv -f "$LOG.1" "$LOG.2" 2>/dev/null || true; mv -f "$LOG" "$LOG.1" 2>/dev/null || true
  fi
}

psql_val() { [ -n "$PGURL" ] && psql "$PGURL" -tAc "$1" 2>/dev/null || echo ""; }

# read/write a small integer state value between runs (for deltas)
state_get() { cat "$STATE_DIR/$1" 2>/dev/null || echo ""; }
state_set() { echo "$2" > "$STATE_DIR/$1" 2>/dev/null || true; }

check_once() {
  local worst="OK" out="" ts
  ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
  bump() { case "$1" in ALERT) worst="ALERT";; WARN) [ "$worst" = ALERT ] || worst="WARN";; esac; }
  add() { out+="  [$1] $2"$'\n'; bump "$1"; }

  # 1) service + health
  local active health
  active=$(systemctl is-active "$UNIT" 2>/dev/null || echo unknown)
  [ "$active" = active ] && add OK "service: active" || add ALERT "service: $active"
  health=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://127.0.0.1:$PORT/healthz" 2>/dev/null || echo 000)
  [ "$health" = 200 ] && add OK "healthz: 200" || add ALERT "healthz: $health"

  # 2) SMPP binds (egress to active carriers, ingress from clients)
  local pid want_egress have_egress ingress
  pid=$(systemctl show -p MainPID --value "$UNIT" 2>/dev/null)
  want_egress=$(psql_val "SELECT COALESCE(SUM(GREATEST(COALESCE(smpp_bind_count,1),1)),0) FROM carriers WHERE status='active' AND egress_transport='smpp'")
  have_egress=0
  if [ -n "$pid" ] && [ "$pid" != 0 ]; then
    while read -r hp; do
      [ -z "$hp" ] && continue
      local n; n=$(ss -tnp 2>/dev/null | grep "pid=$pid," | grep -c "$hp")
      have_egress=$((have_egress + n))
    done < <(psql_val "SELECT smpp_host||':'||smpp_port FROM carriers WHERE status='active' AND egress_transport='smpp' AND smpp_host IS NOT NULL")
  fi
  if [ -n "$want_egress" ] && [ "$want_egress" -gt 0 ]; then
    if [ "$have_egress" -ge "$want_egress" ]; then add OK "egress binds: $have_egress/$want_egress up"
    elif [ "$have_egress" -gt 0 ]; then add WARN "egress binds: $have_egress/$want_egress up (some down)"
    else add ALERT "egress binds: 0/$want_egress up (carrier link down)"; fi
  fi
  ingress=$(ss -tn state established "( sport = :$INGRESS_PORT )" 2>/dev/null | grep -c ':')
  add OK "ingress client binds: $ingress"

  # 3) send queue depth (trend)
  local queued sending prevq
  queued=$(psql_val "SELECT count(*) FROM sms_logs WHERE status='queued'")
  sending=$(psql_val "SELECT count(*) FROM sms_logs WHERE status='sending'")
  prevq=$(state_get queued); state_set queued "${queued:-0}"
  if [ -n "$queued" ]; then
    local trend=""; [ -n "$prevq" ] && trend=" (was $prevq)"
    if [ "$queued" -gt "$QUEUE_WARN" ]; then add WARN "send queue: queued=$queued sending=$sending$trend"
    else add OK "send queue: queued=$queued sending=$sending$trend"; fi
  fi

  # 4) DLR health: accepted-no-DLR backlog (2d) + unmatched-DLR delta
  local backlog unmatched prevu
  backlog=$(psql_val "SELECT count(*) FROM sms_logs WHERE status='accepted' AND dlr_requested AND dlr_received_at IS NULL AND received_at >= now()-interval '2 days'")
  [ -n "$backlog" ] && { [ "$backlog" -gt 0 ] && add WARN "accepted-no-DLR (2d): $backlog awaiting carrier receipt" || add OK "accepted-no-DLR (2d): 0"; }
  unmatched=$(journalctl -u "$UNIT" --since "@$(( $(date +%s) - INTERVAL ))" --no-pager 2>/dev/null | grep -c "deliver_sm unmatched")
  prevu=$(state_get unmatched); state_set unmatched "$unmatched"
  [ "${unmatched:-0}" -gt 0 ] && add WARN "unmatched DLRs this interval: $unmatched (carrier sent receipts we could not correlate)" || add OK "unmatched DLRs this interval: 0"

  # 5) recent errors in the service log
  local errs
  errs=$(journalctl -u "$UNIT" -p err --since "@$(( $(date +%s) - INTERVAL ))" --no-pager 2>/dev/null | grep -vc "^-- ")
  [ "${errs:-0}" -gt 0 ] && add WARN "service errors this interval: $errs (journalctl -u $UNIT -p err)" || add OK "service errors this interval: 0"

  # 6) disk + wire-log footprint
  local diskpct wsize
  diskpct=$(df --output=pcent "$WIRE_DIR" 2>/dev/null | tail -1 | tr -dc '0-9')
  wsize=$(du -sh "$WIRE_DIR" 2>/dev/null | cut -f1)
  if [ -n "$diskpct" ]; then
    if [ "$diskpct" -ge 90 ]; then add ALERT "disk ${WIRE_DIR%/*}: ${diskpct}% used (wire logs ${wsize:-?})"
    elif [ "$diskpct" -ge 80 ]; then add WARN "disk: ${diskpct}% used (wire logs ${wsize:-?})"
    else add OK "disk: ${diskpct}% used (wire logs ${wsize:-?})"; fi
  fi

  rotate
  { echo "$ts  OVERALL=$worst"; printf '%s' "$out"; } >> "$LOG"
}

echo "$(date -u +%FT%TZ)  minisms-monitor started (interval=${INTERVAL}s log=$LOG)" >> "$LOG"
while true; do
  check_once
  sleep "$INTERVAL"
done
