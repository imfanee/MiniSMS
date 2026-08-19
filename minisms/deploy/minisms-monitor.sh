#!/usr/bin/env bash
# Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
# MiniSMS system watchdog. Run every minute from cron. Within each run it probes the health URL every
# few seconds, then checks OS logs, CPU, RAM, disk, PostgreSQL and the MiniSMS service. It emails an
# alert when something is abnormal (and again periodically until it recovers), a recovery notice when it
# clears, and an hourly "all normal" report otherwise. Email needs SMTP settings in the config; without
# them every event is still recorded to the local log and journald.
set -uo pipefail
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

CONF=/etc/minisms-monitor/monitor.conf
# shellcheck disable=SC1090
[ -r "$CONF" ] && . "$CONF"

: "${HEALTHZ_URL:=http://127.0.0.1:8080/healthz}"
: "${SERVICE:=minisms}"
: "${PG_SERVICE:=postgresql@15-main}"
: "${ALERT_EMAIL:=imfanee@gmail.com}"
: "${PROBES:=11}"                 # health probes per run
: "${PROBE_GAP:=5}"               # seconds between probes ("every 5 sec")
: "${HEALTHZ_FAIL_THRESHOLD:=3}"  # failed probes in a run before it counts as abnormal
: "${CPU_LOAD_FACTOR:=2.0}"       # alert when 1-min load > cores * factor
: "${MEM_AVAIL_MIN_PCT:=10}"      # alert when available memory below this %
: "${DISK_MAX_PCT:=90}"           # alert when the mount is above this % used
: "${DISK_MOUNT:=/}"
: "${ERR_SPIKE:=0}"               # alert when journald error lines since last run exceed this (0=off)
: "${EGRESS_SMPP_PORT:=}"         # optional: carrier SMPP port to count egress binds against
: "${EXPECTED_EGRESS_BINDS:=0}"   # 0 = skip the bind check
: "${REPORT_INTERVAL_MIN:=60}"    # hourly OK report
: "${REALERT_INTERVAL_MIN:=15}"   # re-send an alert this often while still abnormal
: "${STATE_DIR:=/var/lib/minisms-monitor}"
: "${LOG:=/var/log/minisms-monitor.log}"
# To enable email set these in the config: SMTP_URL (e.g. smtps://smtp.gmail.com:465), SMTP_USER,
# SMTP_PASS, MAIL_FROM.

mkdir -p "$STATE_DIR"
now=$(date +%s)
host=$(hostname)

# One run at a time; if a slow run is still going, skip this tick.
exec 9>/var/lock/minisms-monitor.lock
flock -n 9 || exit 0

log() { printf '%s %s\n' "$(date -u +%FT%TZ)" "$*" >>"$LOG" 2>/dev/null; }

send_mail() {
  local subject="$1" body="$2"
  log "NOTIFY [$subject]"
  logger -t minisms-monitor -- "$subject" 2>/dev/null || true
  if [ -z "${SMTP_URL:-}" ] || [ -z "${SMTP_USER:-}" ] || [ -z "${SMTP_PASS:-}" ]; then
    log "email not configured (SMTP_* unset) - logged locally only"
    return 0
  fi
  local from="${MAIL_FROM:-$SMTP_USER}" msg
  printf -v msg 'From: MiniSMS Monitor <%s>\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n' \
    "$from" "$ALERT_EMAIL" "$subject" "$(date -R)" "$body"
  if printf '%s' "$msg" | curl -s --url "$SMTP_URL" --ssl-reqd \
      --user "$SMTP_USER:$SMTP_PASS" --mail-from "$from" --mail-rcpt "$ALERT_EMAIL" \
      --upload-file - -m 25 >>"$LOG" 2>&1; then
    log "email sent to $ALERT_EMAIL"
  else
    log "email send FAILED (check SMTP settings)"
  fi
}

problems=()

# --- health URL, probed every $PROBE_GAP seconds ---
fail=0
for ((i = 0; i < PROBES; i++)); do
  code=$(curl -s -o /dev/null -m 4 -w '%{http_code}' "$HEALTHZ_URL" 2>/dev/null || echo 000)
  [ "$code" = "200" ] || fail=$((fail + 1))
  ((i < PROBES - 1)) && sleep "$PROBE_GAP"
done
((fail >= HEALTHZ_FAIL_THRESHOLD)) && problems+=("health check failing: $fail/$PROBES probes not 200 ($HEALTHZ_URL)")

# --- MiniSMS service ---
svc=$(systemctl is-active "$SERVICE" 2>/dev/null || true)
[ "$svc" = "active" ] || problems+=("service $SERVICE is '$svc' (expected active)")

# --- PostgreSQL ---
pg=$(systemctl is-active "$PG_SERVICE" 2>/dev/null || true)
[ "$pg" = "active" ] || problems+=("PostgreSQL $PG_SERVICE is '$pg'")
sudo -u postgres psql -tAc 'SELECT 1' >/dev/null 2>&1 || problems+=("PostgreSQL not answering SELECT 1")

# --- CPU load ---
load1=$(cut -d' ' -f1 /proc/loadavg)
ncpu=$(nproc)
awk -v l="$load1" -v n="$ncpu" -v f="$CPU_LOAD_FACTOR" 'BEGIN{exit !(l > n*f)}' \
  && problems+=("high CPU load: 1-min $load1 on $ncpu cores (> ${CPU_LOAD_FACTOR}x)")

# --- memory ---
read -r mem_total mem_avail < <(awk '/^MemTotal:/{t=$2}/^MemAvailable:/{a=$2}END{print t" "a}' /proc/meminfo)
mem_pct=$((mem_avail * 100 / mem_total))
((mem_pct < MEM_AVAIL_MIN_PCT)) && problems+=("low memory: ${mem_pct}% available (< ${MEM_AVAIL_MIN_PCT}%)")

# --- disk ---
disk_pct=$(df --output=pcent "$DISK_MOUNT" 2>/dev/null | tail -1 | tr -dc '0-9')
[ -n "$disk_pct" ] && ((disk_pct > DISK_MAX_PCT)) && problems+=("disk ${DISK_MOUNT} at ${disk_pct}% (> ${DISK_MAX_PCT}%)")

# --- failed systemd units ---
failed=$(systemctl --failed --no-legend --plain 2>/dev/null | awk '{print $1}' | grep -v '^$' | paste -sd, -)
[ -n "$failed" ] && problems+=("failed systemd units: $failed")

# --- kernel OOM + optional error spike since last run ---
last_ts=$(cat "$STATE_DIR/last_ts" 2>/dev/null || echo $((now - 300)))
oom=$(journalctl -k --since "@$last_ts" --no-pager 2>/dev/null | grep -icE "out of memory|oom-killer|killed process" || true)
((oom > 0)) && problems+=("kernel OOM events since last check: $oom")
err_count=$(journalctl --since "@$last_ts" -p err --no-pager 2>/dev/null | grep -c . || true)
[ "$ERR_SPIKE" -gt 0 ] 2>/dev/null && ((err_count > ERR_SPIKE)) && problems+=("journald error spike: $err_count error lines since last check (> $ERR_SPIKE)")
echo "$now" >"$STATE_DIR/last_ts"

# --- optional SMPP egress binds ---
if [ -n "$EGRESS_SMPP_PORT" ] && ((EXPECTED_EGRESS_BINDS > 0)); then
  pid=$(systemctl show -p MainPID --value "$SERVICE" 2>/dev/null)
  binds=$(ss -tnp 2>/dev/null | grep "pid=${pid}," | grep ":$EGRESS_SMPP_PORT" | grep -c ESTAB || true)
  ((binds == 0)) && problems+=("SMPP egress binds down: 0 up (expected $EXPECTED_EGRESS_BINDS)")
fi

stats="host=$host load=${load1}/${ncpu} mem_avail=${mem_pct}% disk=${disk_pct:-?}% health_fail=${fail}/${PROBES} service=${svc} pg=${pg} recent_errs=${err_count}"

prev=$(cat "$STATE_DIR/status" 2>/dev/null || echo OK)

if ((${#problems[@]} > 0)); then
  echo ABNORMAL >"$STATE_DIR/status"
  body="MiniSMS monitor found ${#problems[@]} problem(s) on ${host} at $(date -u +%FT%TZ):"$'\n\n'
  for p in "${problems[@]}"; do body+=" - ${p}"$'\n'; done
  body+=$'\n'"Snapshot: ${stats}"
  last_alert=$(cat "$STATE_DIR/last_alert" 2>/dev/null || echo 0)
  if [ "$prev" != "ABNORMAL" ] || ((now - last_alert >= REALERT_INTERVAL_MIN * 60)); then
    send_mail "[ALERT] MiniSMS on ${host}: ${#problems[@]} problem(s)" "$body"
    echo "$now" >"$STATE_DIR/last_alert"
  fi
  log "ABNORMAL ${stats} :: ${problems[*]}"
else
  echo OK >"$STATE_DIR/status"
  if [ "$prev" = "ABNORMAL" ]; then
    send_mail "[RECOVERED] MiniSMS on ${host} back to normal" "All checks pass again at $(date -u +%FT%TZ)."$'\n\n'"Snapshot: ${stats}"
  fi
  last_report=$(cat "$STATE_DIR/last_report" 2>/dev/null || echo 0)
  if ((now - last_report >= REPORT_INTERVAL_MIN * 60)); then
    send_mail "[OK] MiniSMS hourly report - ${host}" "All systems normal at $(date -u +%FT%TZ)."$'\n\n'"Snapshot: ${stats}"
    echo "$now" >"$STATE_DIR/last_report"
  fi
  log "OK ${stats}"
fi
