#!/usr/bin/env bash
set -euo pipefail

hf_revision="0fa6744b0e1d8b5b65cad0d5356a8e968bd22b02"
mitre_commit="68d2992ea01249fc72e966f569a0d0b663d98581"
lab_dir="$(cd "$(dirname "$0")/.." && pwd)"
runtime="$lab_dir/runtime"
kusto="$runtime/kusto_data"
refs="$runtime/references"
mkdir -p "$kusto" "$refs"

download_hf() {
  local remote="$1" local_path="$2"
  curl -fL --retry 3 "https://huggingface.co/datasets/arjun180-new/cti_realm/resolve/$hf_revision/$remote" -o "$local_path"
}

for name in aadserviceprincipalsigninlogs aksaudit aksauditadmin auditlogs azureactivity azurediagnostics devicefileevents deviceprocessevents microsoftgraphactivitylogs officeactivity signinlogs storageboblogs; do
  download_hf "cti_realm/kusto_data/$name.jsonl" "$kusto/$name.jsonl"
done
download_hf "cti_realm/cti_reports/reports.jsonl" "$refs/reports.jsonl"
download_hf "cti_realm/data/sigma_rules.json" "$refs/sigma_rules.json"

download_mitre() {
  local domain="$1" sha256="$2"
  local destination="$refs/$domain.json"
  curl -fL --retry 3 "https://raw.githubusercontent.com/mitre/cti/$mitre_commit/$domain/$domain.json" -o "$destination"
  printf '%s  %s\n' "$sha256" "$destination" | shasum -a 256 -c -
}
download_mitre enterprise-attack fc783039f17fba646f79448f1322996457c658a9474f6d14c3bc924a2cf1c97d
download_mitre mobile-attack f61e0a1d9bc828f95df50463c73e48ea57df5d7b0c2d7982ebfa349409dfb785
download_mitre ics-attack 02c991737cba05492e5d17c38643a2f1c1d7e3536bae43fa8d62b02fadcd9c0f

(cd "$runtime" && find kusto_data references -type f -print0 | sort -z | xargs -0 shasum -a 256) > "$runtime/SHA256SUMS"
echo "Prepared pinned CTI-REALM runtime data under $runtime"
