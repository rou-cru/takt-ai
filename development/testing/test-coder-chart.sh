#!/usr/bin/env bash
# Offline bounded Helm regression for the optional Coder mode of charts/takt-ai.
#
# Blind authorship from the frozen contracts: #15112 CW-T1-01..05 and CW-T1-09,
# #15116 CH check location, #15110 D1-D6 (dependency, mode coupling, native
# Coder env, RBAC, direct gating and retained claim). The chart and templates
# were not read as a source of expected values; expected values come from the
# contracts above, and the direct baseline comes from the pre-change revision.
#
# Offline: no cluster, registry, network or credential is used. The Coder
# dependency archive must already be resolved into charts/takt-ai/charts by an
# online preparation step that this script never performs. When it is missing,
# the script exits 3 (prerequisite not met, UNVERIFIED) rather than passing.
#
# Exit codes: 0 all checks passed; 1 at least one check failed; 3 no check
# failed and at least one scenario is UNVERIFIED: a missing helm or git, an
# unresolved Coder dependency (the script stops before any assertion), or no
# usable pre-change revision (BASELINE_REF, by default the merge base with
# origin/main) for the direct baseline. The CW-T1-09 scan for
# environment-specific literals across the repository's tracked source files
# is a real check that passes or fails; it never causes exit 3. Read the
# UNVERIFIED lines to tell the exit-3 cases apart.
set -euo pipefail

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
CHART="$ROOT/charts/takt-ai"
NS="takt-phase-one-test"
TMP=$(mktemp -d)
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

FAILURES=0
UNVERIFIED=0
pass() { local check="$1"; printf 'PASS %s\n' "$check"; }
fail() { local check="$1" cause="$2"; printf 'FAIL %s: %s\n' "$check" "$cause"; FAILURES=$((FAILURES + 1)); }
unverified() { local check="$1" cause="$2"; printf 'UNVERIFIED %s: %s\n' "$check" "$cause"; UNVERIFIED=$((UNVERIFIED + 1)); }

command -v helm >/dev/null 2>&1 || { printf 'UNVERIFIED prerequisite: helm is not on PATH\n'; exit 3; }
command -v git >/dev/null 2>&1 || { printf 'UNVERIFIED prerequisite: git is not on PATH (needed for the direct baseline and the CW-T1-09 scan)\n'; exit 3; }
[[ -f "$CHART/Chart.yaml" ]] || { fail chart-present "missing $CHART/Chart.yaml"; exit 1; }

# Prerequisite: the coder dependency archive must be pre-resolved (online step).
# STATUS "ok" (archive resolved and unpacked) and "unpacked" (chart directory present) both satisfy it.
coder_status=$(helm dependency list "$CHART" 2>/dev/null | awk '$1 == "coder" { print $NF }')
if [[ "$coder_status" != "ok" ]] && [[ "$coder_status" != "unpacked" ]]; then
    printf 'UNVERIFIED prerequisite: coder dependency status is "%s"; run "helm dependency build charts/takt-ai" as an online preparation step, then rerun\n' "$coder_status"
    exit 3
fi

# ---------------------------------------------------------------- fixtures
# Synthetic Coder-mode values. Secret references are names only; no value.
write_coder_values() { # write_coder_values <file>
    local file="$1"
    cat > "$file" <<'VALUES'
execution:
  mode: coder
coder:
  enabled: true
  coder:
    envUseClusterAccessURL: false
    env:
      - name: CODER_PG_CONNECTION_URL
        valueFrom:
          secretKeyRef:
            name: synthetic-coder-db
            key: url
      - name: CODER_OIDC_CLIENT_SECRET
        valueFrom:
          secretKeyRef:
            name: synthetic-coder-oidc
            key: client-secret
      - name: CODER_ACCESS_URL
        value: https://coder.example.invalid
      - name: CODER_OIDC_ISSUER_URL
        value: https://idp.example.invalid
      - name: CODER_OIDC_CLIENT_ID
        value: synthetic-client-id
      - name: CODER_DISABLE_PASSWORD_AUTH
        value: "true"
      - name: CODER_OAUTH2_PROVIDER_ENABLE
        value: "true"
VALUES
}
BASE_VALUES="$TMP/coder-base.yaml"
write_coder_values "$BASE_VALUES"

# Dedicated-namespace RBAC profile, appended under coder.coder (after env).
DEDICATED_TAIL="$TMP/dedicated-tail.yaml"
cat > "$DEDICATED_TAIL" <<'TAIL'
    serviceAccount:
      workspacePerms: false
      workspaceNamespaces:
        - name: takt-workspaces
          workspacePerms: true
          enableDeployments: false
          extraRules:
            - apiGroups: ["networking.k8s.io"]
              resources: ["networkpolicies"]
              verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
TAIL

render() { # render <out-prefix> [helm args...]; stdout to <out>.yaml, stderr to <out>.err
    local out="$1"; shift
    helm template takt "$CHART" --namespace "$NS" "$@" > "$out.yaml" 2> "$out.err"
}

# Output of the document(s) that contain a given word, from a rendered file.
docs_containing() { # docs_containing <file> <word>
    local file="$1" word="$2"
    awk -v w="$word" '
        /^---/ { if (doc ~ w) printf "%s", doc; doc = ""; next }
        { doc = doc $0 "\n" }
        END { if (doc ~ w) printf "%s", doc }' "$file"
}

# ------------------------------------------------ CH-01 (CW-T1-01) default
if render "$TMP/direct"; then
    pass "CW-T1-01 default direct chart renders"
else
    fail "CW-T1-01 default direct chart renders" "helm template failed: $(cat "$TMP/direct.err")"
fi

if helm lint "$CHART" >"$TMP/lint.out" 2>&1; then pass "CW-T1-01 helm lint passes"; else fail "CW-T1-01 helm lint passes" "$(cat "$TMP/lint.out")"; fi

if helm show chart "$CHART" > "$TMP/chart.yaml" 2>/dev/null; then
    deps=$(awk '/^dependencies:/{f=1;next} /^[^ -]/{f=0} f && /^- /{n++} END{print n+0}' "$TMP/chart.yaml")
    if [[ "$deps" = "1" ]] \
        && grep -q 'name: coder' "$TMP/chart.yaml" \
        && grep -q 'version: 2.38.0' "$TMP/chart.yaml" \
        && grep -q 'repository: https://helm.coder.com/v2' "$TMP/chart.yaml" \
        && grep -q 'condition: coder.enabled' "$TMP/chart.yaml" \
        && ! grep -q 'libcoder' "$TMP/chart.yaml"; then
        pass "CW-T1-01 sole optional coder 2.38.0 dependency, conditioned on coder.enabled"
    else
        fail "CW-T1-01 sole optional coder 2.38.0 dependency" "dependency list differs from coder 2.38.0 / https://helm.coder.com/v2 / coder.enabled, or includes libcoder"
    fi
else
    fail "CW-T1-01 dependency metadata readable" "helm show chart failed"
fi

if [[ -f "$CHART/Chart.lock" ]]; then pass "CW-T1-01 Chart.lock present"; else fail "CW-T1-01 Chart.lock present" "no Chart.lock in $CHART"; fi

if helm show values "$CHART" 2>/dev/null | grep -q '^execution:' ; then
    if helm show values "$CHART" | awk '/^execution:/{f=1;next} /^[^ ]/{f=0} f' | grep -q 'mode: direct' \
        && helm show values "$CHART" | awk '/^coder:/{f=1;next} /^[^ ]/{f=0} f' | grep -q 'enabled: false'; then
        pass "CW-T1-01 defaults are execution.mode=direct and coder.enabled=false"
    else
        fail "CW-T1-01 defaults" "execution.mode/coder.enabled defaults differ from direct/false"
    fi
else
    fail "CW-T1-01 defaults" "no execution block in default values"
fi

if [[ -f "$TMP/direct.yaml" ]] && ! grep -q 'app.kubernetes.io/name: coder$' "$TMP/direct.yaml"; then
    pass "CW-T1-01 default render has no Coder workload"
else
    fail "CW-T1-01 default render has no Coder workload" "Coder labelled resources rendered by default"
fi

# Direct baseline: identities and environment of the default direct render must
# equal the pre-change revision. BASELINE_REF overrides the default, the merge
# base with origin/main; the resolved dependency archives are reused because the
# baseline checkout carries only Chart.lock.
BASE="$TMP/baseline"
mkdir -p "$BASE"
BASELINE_REF=${BASELINE_REF:-$(git -C "$ROOT" merge-base origin/main HEAD 2>/dev/null || true)}
if [[ -n "$BASELINE_REF" ]] && git -C "$ROOT" archive "$BASELINE_REF" charts/takt-ai | tar -x -C "$BASE" 2>/dev/null && [[ -f "$BASE/charts/takt-ai/Chart.yaml" ]]; then
    if grep -q '^dependencies:' "$BASE/charts/takt-ai/Chart.yaml"; then
        mkdir -p "$BASE/charts/takt-ai/charts"
        cp "$CHART"/charts/*.tgz "$BASE/charts/takt-ai/charts/" 2>/dev/null || true
    fi
    if helm template takt "$BASE/charts/takt-ai" --namespace "$NS" > "$TMP/baseline.yaml" 2>"$TMP/baseline.err"; then
        ident() { local file="$1"; awk '/^kind:/{k=$2} /^  name:/{print k, $2}' "$file" | sort -u; }
        envs() { local file="$1"; grep -E '^ +- name: ' "$file" | sort; }
        if [[ "$(ident "$TMP/direct.yaml")" = "$(ident "$TMP/baseline.yaml")" ]] && [[ "$(envs "$TMP/direct.yaml")" = "$(envs "$TMP/baseline.yaml")" ]]; then
            pass "CW-T1-01 direct identities and environment equal the pre-change baseline ($BASELINE_REF)"
        else
            fail "CW-T1-01 direct baseline" "kind/name identities or env names differ from $BASELINE_REF"
        fi
    else
        unverified "CW-T1-01 direct baseline" "baseline revision $BASELINE_REF does not render standalone: $(head -c 200 "$TMP/baseline.err")"
    fi
else
    unverified "CW-T1-01 direct baseline" "no pre-change revision available (set BASELINE_REF or fetch origin/main)"
fi

# ------------------------------------------------ CH-02 (CW-T1-02) mode coupling
if render "$TMP/coder" -f "$BASE_VALUES"; then
    pass "CW-T1-03 valid Coder configuration renders"
else
    fail "CW-T1-03 valid Coder configuration renders" "$(cat "$TMP/coder.err")"
fi

# Direct workload: the default direct render must carry a Deployment selected by
# app.kubernetes.io/name: takt-ai (positive control), and the coder render must not.
direct_workload() { local file="$1"; docs_containing "$file" 'kind: Deployment' | grep -q 'app.kubernetes.io/name: takt-ai$'; }
if [[ -f "$TMP/direct.yaml" ]] && [[ -f "$TMP/coder.yaml" ]]; then
    if direct_workload "$TMP/direct.yaml" && ! direct_workload "$TMP/coder.yaml"; then
        pass "CW-T1-02 coder mode omits the direct takt-ai Deployment"
    else
        fail "CW-T1-02 coder mode omits the direct takt-ai Deployment" "direct workload missing from direct render, or still rendered in coder mode"
    fi
fi
pvc_doc=$(docs_containing "$TMP/coder.yaml" 'kind: PersistentVolumeClaim' || true)
if [[ -n "$pvc_doc" ]] && printf '%s' "$pvc_doc" | grep -q 'helm.sh/resource-policy: keep'; then
    pass "CW-T1-02 coder mode retains the direct PVC with helm.sh/resource-policy: keep"
else
    fail "CW-T1-02 coder mode retains the direct PVC" "direct PersistentVolumeClaim missing or lacks keep"
fi
if docs_containing "$TMP/coder.yaml" 'helm.sh/hook' | grep -q .; then
    fail "CW-T1-02 no publisher hook or Job" "a Helm hook is rendered"
else
    pass "CW-T1-02 no publisher hook or Job rendered"
fi
if grep -q '^kind: Job$' "$TMP/coder.yaml" "$TMP/direct.yaml" 2>/dev/null; then
    fail "CW-T1-02 no publisher Job" "a Job is rendered by the Takt chart"
else
    pass "CW-T1-02 no publisher Job rendered in either mode"
fi

if render "$TMP/direct-keep" --set persistence.retain=true \
    && docs_containing "$TMP/direct-keep.yaml" 'kind: PersistentVolumeClaim' | grep -q 'helm.sh/resource-policy: keep'; then
    pass "CW-T1-02 direct mode with persistence.retain=true keeps the claim"
else
    fail "CW-T1-02 direct retain" "persistence.retain=true does not add keep"
fi

for args in "--set coder.enabled=false" "--set execution.mode=direct --set coder.enabled=true" "--set execution.mode=bogus" "--set coder.enabled=yes"; do
    # shellcheck disable=SC2086
    if render "$TMP/mismatch" -f "$BASE_VALUES" $args; then
        fail "CW-T1-02 mode coupling rejects: $args" "render succeeded"
    elif grep -Eq 'execution|coder|mode|schema' "$TMP/mismatch.err"; then
        pass "CW-T1-02 mode coupling rejects: $args"
    else
        fail "CW-T1-02 mode coupling rejects: $args" "failed without a mode/schema cause: $(head -c 200 "$TMP/mismatch.err")"
    fi
done

# ------------------------------------------------ CH-03 (CW-T1-03) native env
env_block() { local name="$1" file="$2"; grep -A4 -- "- name: $name\$" "$file" || true; }
if [[ -f "$TMP/coder.yaml" ]]; then
    if env_block CODER_PG_CONNECTION_URL "$TMP/coder.yaml" | grep -q secretKeyRef && ! env_block CODER_PG_CONNECTION_URL "$TMP/coder.yaml" | grep -q '^ *value:'; then
        pass "CW-T1-03 CODER_PG_CONNECTION_URL is a secretKeyRef, not a literal"
    else
        fail "CW-T1-03 CODER_PG_CONNECTION_URL" "not an explicit secretKeyRef"
    fi
    if env_block CODER_OIDC_CLIENT_SECRET "$TMP/coder.yaml" | grep -q secretKeyRef && ! env_block CODER_OIDC_CLIENT_SECRET "$TMP/coder.yaml" | grep -q '^ *value:'; then
        pass "CW-T1-03 CODER_OIDC_CLIENT_SECRET is a secretKeyRef, not a literal"
    else
        fail "CW-T1-03 CODER_OIDC_CLIENT_SECRET" "not an explicit secretKeyRef"
    fi
    for flag in CODER_DISABLE_PASSWORD_AUTH CODER_OAUTH2_PROVIDER_ENABLE; do
        if env_block "$flag" "$TMP/coder.yaml" | grep -Eq 'value: "?true"?'; then
            pass "CW-T1-03 $flag is the string true"
        else
            fail "CW-T1-03 $flag" "not rendered as the string true"
        fi
    done
    if grep -q 'CODER_ACCESS_URL' "$TMP/coder.yaml" && grep -q 'https://coder.example.invalid' "$TMP/coder.yaml"; then
        pass "CW-T1-03 CODER_ACCESS_URL is the explicit HTTPS access URL"
    else
        fail "CW-T1-03 CODER_ACCESS_URL" "explicit access URL not rendered"
    fi
fi

# Negative cases (CW-T1-04): each must fail and name its cause.
neg_case() { # neg_case <label> <token> <command> [args...]; the command prints the variant values file
    local label="$1" token="$2" variant="$TMP/variant.yaml"
    shift 2
    "$@" > "$variant"
    if render "$TMP/neg" -f "$variant"; then
        fail "CW-T1-04 rejects $label" "render succeeded"
    elif grep -q -- "$token" "$TMP/neg.err"; then
        pass "CW-T1-04 rejects $label"
    else
        fail "CW-T1-04 rejects $label" "failed but did not name $token: $(head -c 200 "$TMP/neg.err")"
    fi
}
variant_literal_secret() {
    sed '/name: CODER_OIDC_CLIENT_SECRET/,+4d' "$BASE_VALUES"
    printf '      - name: CODER_OIDC_CLIENT_SECRET\n        value: literal-secret\n'
}
variant_duplicate_access_url() {
    cat "$BASE_VALUES"
    printf '      - name: CODER_ACCESS_URL\n        value: https://other.example.invalid\n'
}
neg_case "missing CODER_PG_CONNECTION_URL" CODER_PG_CONNECTION_URL sed '/name: CODER_PG_CONNECTION_URL/,+4d' "$BASE_VALUES"
neg_case "literal CODER_OIDC_CLIENT_SECRET" CODER_OIDC_CLIENT_SECRET variant_literal_secret
# Drops the "s" of the HTTPS scheme, producing a clear-text access URL.
neg_case "non-HTTPS CODER_ACCESS_URL" CODER_ACCESS_URL sed 's#\(value: http\)s\(://coder\.example\.invalid\)#\1\2#' "$BASE_VALUES"
neg_case "relative CODER_OIDC_ISSUER_URL" CODER_OIDC_ISSUER_URL sed 's#value: https://idp.example.invalid#value: idp.example.invalid#' "$BASE_VALUES"
neg_case "empty CODER_OIDC_CLIENT_ID" CODER_OIDC_CLIENT_ID sed 's#value: synthetic-client-id#value: ""#' "$BASE_VALUES"
neg_case "CODER_OAUTH2_PROVIDER_ENABLE=false" CODER_OAUTH2_PROVIDER_ENABLE sed '/CODER_OAUTH2_PROVIDER_ENABLE/{n;s/"true"/"false"/;}' "$BASE_VALUES"
neg_case "CODER_DISABLE_PASSWORD_AUTH=false" CODER_DISABLE_PASSWORD_AUTH sed '/CODER_DISABLE_PASSWORD_AUTH/{n;s/"true"/"false"/;}' "$BASE_VALUES"
neg_case "duplicate CODER_ACCESS_URL" CODER_ACCESS_URL variant_duplicate_access_url
# A null env removes the inherited list before validation; the required env
# keeps every authentication check from being skipped.
neg_case "null coder.coder.env" env sed '/^    env:/,$c\    env: null' "$BASE_VALUES"

# ------------------------------------------------ CH-04 (CW-T1-05) native RBAC
if [[ -f "$TMP/coder.yaml" ]]; then
    np_doc=$(docs_containing "$TMP/coder.yaml" 'networkpolicies' || true)
    if [[ -n "$np_doc" ]] && printf '%s' "$np_doc" | grep -q "namespace: $NS"; then
        verbs_ok=1
        for v in get list watch create update patch delete; do
            printf '%s' "$np_doc" | grep -Eq "(^|[^a-z])$v([^a-z]|$)" || verbs_ok=0
        done
        if [[ "$verbs_ok" = 1 ]]; then
            pass "CW-T1-05 same-namespace Role grants NetworkPolicy get/list/watch/create/update/patch/delete"
        else
            fail "CW-T1-05 same-namespace NetworkPolicy verbs" "a required verb is missing"
        fi
    else
        fail "CW-T1-05 same-namespace NetworkPolicy grant" "no namespaced networkpolicies rule in $NS"
    fi
    if grep -Eq '^kind: (ClusterRole|ClusterRoleBinding|Namespace)$' "$TMP/coder.yaml"; then
        fail "CW-T1-05 no cluster-scoped or Namespace object" "one is rendered"
    else
        pass "CW-T1-05 no cluster-scoped RBAC or Namespace is rendered"
    fi
    if docs_containing "$TMP/coder.yaml" 'kind: Role' | grep -Eq '^ +- (secrets|deployments)$|resources: \[?"?(secrets|deployments)'; then
        fail "CW-T1-05 no Secret or deployment grant" "the same-namespace profile grants secrets or deployments"
    else
        pass "CW-T1-05 same-namespace profile grants no Secret or deployment rights"
    fi
fi
DED="$TMP/dedicated-values.yaml"; cat "$BASE_VALUES" "$DEDICATED_TAIL" > "$DED"
if render "$TMP/dedicated" -f "$DED"; then
    ded_np=$(docs_containing "$TMP/dedicated.yaml" 'networkpolicies' || true)
    if printf '%s' "$ded_np" | grep -q "namespace: takt-workspaces" && ! grep -q 'workspacePerms: true' "$TMP/dedicated.yaml" 2>/dev/null; then
        pass "CW-T1-05 dedicated profile binds the networkpolicies rule to the workspace namespace"
    else
        fail "CW-T1-05 dedicated profile" "NetworkPolicy grant not bound to takt-workspaces or root workspacePerms still enabled"
    fi
else
    fail "CW-T1-05 dedicated profile renders" "$(head -c 200 "$TMP/dedicated.err")"
fi

# ------------------------------------------------ CH-05 (CW-T1-09) repository scan
# C2 (#15105): environment-specific literals must not occur anywhere in the
# repository's tracked source files. Git metadata, node_modules, the local
# .takt-ai state, generated lockfiles and this script's own seeded self-check
# are not product source. A seeded self-check runs the same pipeline to prove
# the scan detects a literal.
PATTERN='amazonaws\.com|arn:aws|\b[0-9]{12}\b|\beks\b|\bkaf[-.]|azurecr\.io|gcr\.io'
scan_repo() { # scan_repo <root> ; prints matching lines among <root>'s tracked source files
    local root="$1"
    git -C "$root" ls-files -z | while IFS= read -r -d '' f; do
        case "$f" in
            .git/*|*/.git/*) continue ;; # git metadata
            node_modules/*|*/node_modules/*) continue ;; # vendored dependencies
            .takt-ai/*|*/.takt-ai/*) continue ;; # local tool state
            go.sum|*/go.sum|*.lock|*.lock.*|*-lock.yaml|*-lock.json) continue ;; # generated lockfiles
            development/testing/test-coder-chart.sh) continue ;; # this script's own seeded self-check
            *) ;; # product source: scanned
        esac
        printf '%s\0' "$root/$f"
    done | xargs -0 grep -HIInE "$PATTERN" 2>/dev/null || true
}
seedrepo="$TMP/seedrepo"; mkdir -p "$seedrepo"; printf 'host: cluster.amazonaws.com\n' > "$seedrepo/seeded.yaml"
if git -C "$seedrepo" init -q && git -C "$seedrepo" add seeded.yaml && [[ -n "$(scan_repo "$seedrepo")" ]]; then
    pass "CW-T1-09 scan self-check detects a seeded environment literal"
else
    fail "CW-T1-09 scan self-check" "seeded literal not detected"
fi
found=$(scan_repo "$ROOT")
if [[ -z "$found" ]]; then pass "CW-T1-09 no environment-specific literal in the repository's tracked source files"; else fail "CW-T1-09 environment literals" "$(printf '%s' "$found" | head -5)"; fi

echo
if [[ "$FAILURES" -gt 0 ]]; then
    echo "chart checks: $FAILURES failed, $UNVERIFIED unverified"; exit 1
fi
if [[ "$UNVERIFIED" -gt 0 ]]; then
    echo "chart checks: all asserted checks passed; $UNVERIFIED unverified"; exit 3
fi
echo "chart checks: all passed"
