#!/usr/bin/env bash
# Manual smoke test for the item-editor API against a locally running server
# (go run ./cmd/api). Exercises: proc list, save, list-visibility, icon
# upload/serve, delete. Run from server/: bash scripts/item-editor-smoke.sh
set -euo pipefail
BASE="${BASE:-http://localhost:8080}"

echo "-- procs available:" && curl -sf "$BASE/catalog/procs" | head -c 300 && echo
echo "-- saving smoke_blade..."
curl -sf -X POST "$BASE/items" -H 'Content-Type: application/json' -d '{
  "item": {"id":"smoke_blade","displayName":"Smoke Blade","iconKey":"smoke_blade",
           "kind":"equipment","tier":"rare","category":"Weapon","slotKind":"any","costGold":120,
           "modifiers":{"damage":9},
           "onHitProc":{"chance":0.1,"effect":"fire_bolt_ignite"}},
  "recipe": {"inputs":["broad_sword","fire_ring"],"costGold":150},
  "availability": {"marketplace":true,"wanderingMerchant":false,
                    "lootTable":{"enabled":true,"weight":15},"recipeList":true}
}' && echo
echo "-- visible in catalog:" && curl -sf "$BASE/catalog/items" | grep -o '"id":"smoke_blade"' && echo ok
echo "-- uploading icon..."
# any small png; reuse a shipped one
curl -sf -X POST "$BASE/items/smoke_blade/image" -H 'Content-Type: image/png' \
  --data-binary @../client/src/game-portal/src/assets/items/weapons/common/broad_sword.png || echo "(adjust png path if layout differs)"
echo "-- serving icon:" && curl -sf -D - -o /dev/null "$BASE/catalog/items/smoke_blade/image" | head -2
echo "-- deleting..." && curl -sf -X DELETE "$BASE/items/smoke_blade" && echo
echo "SMOKE OK"
