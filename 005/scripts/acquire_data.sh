#!/usr/bin/env bash
set -euo pipefail

revision="8bc9ce1f97f8a6880c815dfba76d88651ddb761a"
sha256="61ecd48685e4eb34cac226a0651fd6443174f6efaa628aea3b06aef2cb088720"
size="1643989896"
lab_dir="$(cd "$(dirname "$0")/.." && pwd)"
runtime="$lab_dir/runtime"
archive="$runtime/data_anonymized.tar.gz"

mkdir -p "$runtime"
curl -fL --retry 3 "https://huggingface.co/datasets/anandmudgerikar/excytin-bench/resolve/$revision/data_anonymized.tar.gz" -o "$archive"
actual_size="$(wc -c < "$archive" | tr -d ' ')"
test "$actual_size" = "$size" || { echo "size mismatch: $actual_size" >&2; exit 1; }
printf '%s  %s\n' "$sha256" "$archive" | shasum -a 256 -c -

rm -rf "$runtime/extracted"
mkdir -p "$runtime/extracted"
tar -xzf "$archive" -C "$runtime/extracted"
data_root="$(find "$runtime/extracted" -type d -name data_anonymized -print -quit)"
test -n "$data_root" || { echo "archive lacks data_anonymized directory" >&2; exit 1; }
python3 "$lab_dir/scripts/prepare_mysql.py" "$data_root" "$runtime/sql"
ln -sfn "$data_root" "$runtime/data_anonymized"
echo "Prepared eight pinned SecRL incident databases under $runtime"
