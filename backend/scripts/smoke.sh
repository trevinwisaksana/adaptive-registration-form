#!/usr/bin/env bash
# Smoke test: drives one whole happy-path flow through the running API with
# curl, per plan.md's "demo: drive a whole flow with curl" milestone.
#
# Usage: BASE_URL=http://localhost:8080 ./scripts/smoke.sh
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
command -v jq >/dev/null || { echo "smoke: jq is required" >&2; exit 1; }

step() { printf '\n\033[1;34m== %s ==\033[0m\n' "$1"; }
pass() { printf '\033[1;32mok\033[0m %s\n' "$1"; }
fail() { printf '\033[1;31mFAIL\033[0m %s\n' "$1"; exit 1; }

step "wait for API"
for i in $(seq 1 30); do
  if curl -sf "$BASE_URL/system" >/dev/null; then break; fi
  sleep 1
  [ "$i" -eq 30 ] && fail "API never became healthy at $BASE_URL"
done
pass "API is up"

step "GET /system (no auth)"
curl -sf "$BASE_URL/system" | jq .

step "POST /sessions"
create_body=$(cat <<'JSON'
{
  "locale": "id",
  "client": {
    "platform": "ios",
    "app_version": "1.4.0",
    "supported_types": ["form", "camera", "signature", "document", "pin", "external"],
    "supported_field_kinds": ["text", "date", "select", "multiselect", "money", "bool"]
  },
  "device_attestation": "smoke-test-device",
  "resume_token": null
}
JSON
)
create_resp=$(curl -sf -X POST "$BASE_URL/sessions" -H 'Content-Type: application/json' -d "$create_body")
echo "$create_resp" | jq .
SESSION_ID=$(echo "$create_resp" | jq -r '.session.id')
TOKEN=$(echo "$create_resp" | jq -r '.token')
FLOW_VERSION=$(echo "$create_resp" | jq -r '.session.flow_version')
[ -n "$SESSION_ID" ] && [ "$SESSION_ID" != "null" ] || fail "no session id in response"
pass "session $SESSION_ID created on flow v$FLOW_VERSION"

auth=(-H "Authorization: Bearer $TOKEN")

submit() {
  local step_id="$1" body="$2"
  curl -sf -X POST "$BASE_URL/sessions/$SESSION_ID/steps/$step_id" \
    "${auth[@]}" -H 'Content-Type: application/json' -H "Idempotency-Key: $(uuidgen 2>/dev/null || echo "$step_id-$RANDOM-$RANDOM")" \
    -d "$body"
}

step "submit personal_details"
# New sessions start on the latest published flow version (v15 in this seed
# set), which requires tax_id (plan.md/contract.md's "regulatory change" demo).
resp=$(submit personal_details '{"answers":{"full_name":"Ada Lovelace","dob":"1990-01-01","tax_id":"09.123.456.7-891.000"}}')
echo "$resp" | jq .
next=$(echo "$resp" | jq -r '.next_step.id')
[ "$next" = "contact_address" ] || fail "expected contact_address next, got $next"
pass "-> $next"

step "submit contact_address"
resp=$(submit contact_address '{"answers":{"address_line1":"Jl. Sudirman 1","country":"ID","region":"JB","city":"JB-BKS","postal_code":"17141"}}')
next=$(echo "$resp" | jq -r '.next_step.id')
[ "$next" = "employment_income" ] || fail "expected employment_income next, got $next"
pass "-> $next"

step "submit employment_income (with npwp since country=ID)"
resp=$(submit employment_income '{"answers":{"employment_status":"employed","employer_name":"Acme Corp","occupation":"SOFTWARE_ENGINEER","annual_income":50000,"source_of_funds":["salary"],"npwp":"09.123.456.7-891.000"}}')
echo "$resp" | jq '.next_step.id, .repairs'
next=$(echo "$resp" | jq -r '.next_step.id')
[ "$next" = "trading_experience" ] || fail "expected trading_experience next, got $next"
pass "-> $next"

step "submit trading_experience (us_person=false, skip FATCA)"
resp=$(submit trading_experience '{"answers":{"experience_level":"intermediate","instruments":["stocks","forex"],"us_person":false,"leverage_acknowledged":true}}')
next=$(echo "$resp" | jq -r '.next_step.id')
total=$(echo "$resp" | jq -r '.progress.total')
[ "$next" = "bank_info" ] || fail "expected bank_info next (FATCA skipped), got $next"
pass "-> $next (progress.total=$total, no FATCA branch)"

step "submit bank_info"
resp=$(submit bank_info '{"answers":{"bank_name":"Bank Mandiri","account_number":"1234567890","account_holder":"Ada Lovelace"}}')
next=$(echo "$resp" | jq -r '.next_step.id')
[ "$next" = "id_card" ] || fail "expected id_card next, got $next"
pass "-> $next"

upload_and_submit() {
  local kind="$1" step_id="$2"
  local slot upload_ref url method
  slot=$(curl -sf -X POST "$BASE_URL/sessions/$SESSION_ID/uploads" "${auth[@]}" -H 'Content-Type: application/json' \
    -d "{\"kind\":\"$kind\",\"content_type\":\"image/png\",\"size_bytes\":1234}")
  upload_ref=$(echo "$slot" | jq -r '.upload_ref')
  url=$(echo "$slot" | jq -r '.url')
  method=$(echo "$slot" | jq -r '.method')
  # 1x1 transparent PNG, valid image bytes for the server's decode check.
  printf '\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\nIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\r\n-\xb4\x00\x00\x00\x00IEND\xaeB`\x82' > /tmp/smoke-$kind.png
  curl -sf -X "$method" "$url" -H 'Content-Type: image/png' --data-binary @/tmp/smoke-$kind.png >/dev/null
  submit "$step_id" "{\"upload_ref\":\"$upload_ref\"}"
}

step "upload + submit id_card"
resp=$(upload_and_submit id_card id_card)
next=$(echo "$resp" | jq -r '.next_step.id')
[ "$next" = "selfie" ] || fail "expected selfie next, got $next"
pass "-> $next"

step "slot overwrite: upload selfie twice before submitting (one object slot per kind, plan.md §2.1)"
slot1=$(curl -sf -X POST "$BASE_URL/sessions/$SESSION_ID/uploads" "${auth[@]}" -H 'Content-Type: application/json' \
  -d '{"kind":"selfie","content_type":"image/png","size_bytes":1234}')
url1=$(echo "$slot1" | jq -r '.url')
printf '\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\nIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\r\n-\xb4\x00\x00\x00\x00IEND\xaeB`\x82' > /tmp/smoke-selfie-1.png
curl -sf -X PUT "$url1" -H 'Content-Type: image/png' --data-binary @/tmp/smoke-selfie-1.png >/dev/null
slot2=$(curl -sf -X POST "$BASE_URL/sessions/$SESSION_ID/uploads" "${auth[@]}" -H 'Content-Type: application/json' \
  -d '{"kind":"selfie","content_type":"image/png","size_bytes":5678}')
url2=$(echo "$slot2" | jq -r '.url')
upload_ref2=$(echo "$slot2" | jq -r '.upload_ref')
[ "$url1" = "$url2" ] || fail "expected the second selfie upload to reuse the same object slot, got $url1 vs $url2"
printf '\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\nIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\r\n-\xb4\x00\x00\x00\x00IEND\xaeB`\x82' > /tmp/smoke-selfie-2.png
curl -sf -X PUT "$url2" -H 'Content-Type: image/png' --data-binary @/tmp/smoke-selfie-2.png >/dev/null
pass "same object slot for both selfie uploads, second overwrote the first"

step "submit selfie (with the second, current upload_ref)"
resp=$(submit selfie "{\"upload_ref\":\"$upload_ref2\"}")
next=$(echo "$resp" | jq -r '.next_step.id')
[ "$next" = "tnc" ] || fail "expected tnc next, got $next"
pass "-> $next"

step "reject stale T&C accept (wrong hash) before accepting for real"
doc_kind=$(echo "$resp" | jq -r '.next_step.doc.kind')
doc_version=$(echo "$resp" | jq -r '.next_step.doc.version')
doc_locale=$(echo "$resp" | jq -r '.next_step.doc.locale')
doc_sha=$(echo "$resp" | jq -r '.next_step.doc.sha256')
stale_code=$(curl -s -o /tmp/smoke-stale.json -w '%{http_code}' -X POST "$BASE_URL/sessions/$SESSION_ID/steps/tnc" \
  "${auth[@]}" -H 'Content-Type: application/json' -H "Idempotency-Key: $(uuidgen 2>/dev/null || echo stale-$RANDOM)" \
  -d "{\"accept\":true,\"doc\":{\"kind\":\"$doc_kind\",\"version\":\"$doc_version\",\"locale\":\"$doc_locale\",\"sha256\":\"0000000000000000000000000000000000000000000000000000000000000000\"}}")
[ "$stale_code" = "409" ] || fail "expected 409 stale_document, got $stale_code: $(cat /tmp/smoke-stale.json)"
stale_err=$(jq -r '.error.code' /tmp/smoke-stale.json)
[ "$stale_err" = "stale_document" ] || fail "expected error.code=stale_document, got $stale_err"
pass "409 stale_document — consent not recorded, current_doc re-served"

step "accept T&C (echoing the served doc pointer)"
resp=$(submit tnc "{\"accept\":true,\"doc\":{\"kind\":\"$doc_kind\",\"version\":\"$doc_version\",\"locale\":\"$doc_locale\",\"sha256\":\"$doc_sha\"}}")
next=$(echo "$resp" | jq -r '.next_step.id')
[ "$next" = "sign" ] || fail "expected sign next, got $next"
pass "-> $next (accepted $doc_kind v$doc_version)"

step "upload + submit signature"
resp=$(upload_and_submit signature sign)
next=$(echo "$resp" | jq -r '.next_step.id')
[ "$next" = "setup_pin" ] || fail "expected setup_pin next, got $next"
pass "-> $next"

step "submit setup_pin"
resp=$(submit setup_pin '{"pin":"142857"}')
echo "$resp" | jq .
next=$(echo "$resp" | jq -r '.next_step')
completed=$(echo "$resp" | jq -r '.progress.completed')
total=$(echo "$resp" | jq -r '.progress.total')
[ "$next" = "null" ] || fail "expected flow complete (next_step null), got $next"
pass "flow complete ($completed/$total) — mock KYC triggered"

step "idempotency replay: resubmitting the same step+key returns the cached response"
# (submit() mints a fresh key each call by design; this call reuses one explicitly)
key="smoke-replay-key"
r1=$(curl -sf -X POST "$BASE_URL/sessions/$SESSION_ID/steps/setup_pin" "${auth[@]}" -H 'Content-Type: application/json' -H "Idempotency-Key: $key" -d '{"pin":"999999"}')
r2=$(curl -sf -X POST "$BASE_URL/sessions/$SESSION_ID/steps/setup_pin" "${auth[@]}" -H 'Content-Type: application/json' -H "Idempotency-Key: $key" -d '{"pin":"999999"}')
[ "$r1" = "$r2" ] || fail "idempotent replay returned a different body"
pass "replay returned identical cached response"

step "mock KYC runs async (~10s after completion) then calls back via /webhooks/mock-kyc"
# The response envelope (contract §1) intentionally doesn't expose raw session
# status — next_step: null + repairs: [] is "nothing left to do" whether
# that's "verifying" or "approved". We wait past the mock adapter's delay and
# re-check the envelope stays terminal (a rejection would surface as a
# redo_step repair instead); the webhook call itself is visible in the API's
# logs ("POST /webhooks/mock-kyc").
sleep 12
resp=$(curl -sf "$BASE_URL/sessions/$SESSION_ID" "${auth[@]}")
next=$(echo "$resp" | jq -r '.next_step')
repairs=$(echo "$resp" | jq -c '.repairs')
[ "$next" = "null" ] && [ "$repairs" = "[]" ] || fail "expected terminal envelope after KYC verdict, got next_step=$next repairs=$repairs"
pass "still terminal after KYC delay — verdict landed without a rejection repair"

printf '\n\033[1;32mSMOKE TEST PASSED\033[0m — full 10-page flow (session %s) completed end to end.\n' "$SESSION_ID"
