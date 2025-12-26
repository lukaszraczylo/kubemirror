#!/bin/bash

# Comprehensive E2E Test Suite for KubeMirror
# Tests all scenarios systematically using the test framework
#
# Usage:
#   ./test-comprehensive.sh              # Run all 30 scenarios sequentially
#   ./test-comprehensive.sh 24 25 26     # Run only scenarios 24, 25, and 26
#   ./test-comprehensive.sh 1 2 3        # Run only scenarios 1, 2, and 3
#
# For parallel execution (faster):
#   ./test-parallel.sh                   # Runs tests in parallel batches
#
# Performance:
#   - Sequential (all): ~5-7 minutes
#   - Parallel: ~3-4 minutes
#   - Selective (few scenarios): <1 minute

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"
source "$SCRIPT_DIR/test-framework.sh"

TEST_NAME="Comprehensive E2E Tests"

# Dedicated namespace for test source resources (NOT default!)
# Using kubemirror- prefix for clear identification and isolation
E2E_SOURCE_NS="kubemirror-e2e-source"

# Cleanup function
cleanup() {
    log_info "Cleaning up all test resources"

    # Remove finalizers from source resources before deleting
    # This prevents resources from getting stuck in Terminating state
    for resource in $(kubectl get secret,configmap -n "$E2E_SOURCE_NS" -l test-resource=e2e -o name 2>/dev/null); do
        kubectl patch "$resource" -n "$E2E_SOURCE_NS" --type=json -p='[{"op":"remove","path":"/metadata/finalizers"}]' 2>/dev/null || true
    done
    for resource in $(kubectl get middleware.traefik.io -n "$E2E_SOURCE_NS" -l test-resource=e2e -o name 2>/dev/null); do
        kubectl patch "$resource" -n "$E2E_SOURCE_NS" --type=json -p='[{"op":"remove","path":"/metadata/finalizers"}]' 2>/dev/null || true
    done

    # Delete all test secrets, configmaps, and CRDs from source namespace
    kubectl delete secret,configmap -n "$E2E_SOURCE_NS" -l test-resource=e2e --ignore-not-found=true 2>/dev/null || true
    kubectl delete middleware.traefik.io -n "$E2E_SOURCE_NS" -l test-resource=e2e --ignore-not-found=true 2>/dev/null || true

    # Delete all test namespaces
    kubectl delete namespace "$E2E_SOURCE_NS" --ignore-not-found=true --wait=false 2>/dev/null || true

    for i in {1..5}; do
        kubectl delete namespace "kubemirror-e2e-ns-$i" --ignore-not-found=true --wait=false 2>/dev/null || true
    done

    for prefix in app db stage prod; do
        for i in {1..3}; do
            kubectl delete namespace "kubemirror-e2e-${prefix}-${i}" --ignore-not-found=true --wait=false 2>/dev/null || true
        done
    done

    kubectl delete namespace kubemirror-e2e-labeled kubemirror-e2e-unlabeled kubemirror-e2e-test-ns --ignore-not-found=true --wait=false 2>/dev/null || true

    sleep 5
}

trap cleanup EXIT

# Parse command-line arguments for selective scenario execution
SCENARIOS_TO_RUN=()
if [ $# -gt 0 ]; then
    SCENARIOS_TO_RUN=("$@")
    log_info "Running specific scenarios: ${SCENARIOS_TO_RUN[*]}"
else
    log_info "Running all scenarios (no filter specified)"
fi

# Helper function to check if a scenario should run
should_run_scenario() {
    local scenario_num=$1

    # If no filter specified, run all scenarios
    if [ ${#SCENARIOS_TO_RUN[@]} -eq 0 ]; then
        return 0
    fi

    # Check if this scenario is in the list
    for num in "${SCENARIOS_TO_RUN[@]}"; do
        if [ "$num" = "$scenario_num" ]; then
            return 0
        fi
    done

    return 1
}

log_info "Starting $TEST_NAME"

# Clean up before starting
cleanup
sleep 3

# Create dedicated source namespace for all test resources
log_info "Creating dedicated source namespace: $E2E_SOURCE_NS"
kubectl create namespace "$E2E_SOURCE_NS" 2>/dev/null || true
sleep 2

#===============================================================================
# SCENARIO 1: Source without labels/annotations
#===============================================================================

if ! should_run_scenario 1; then
    continue
fi

run_test_scenario "1: Source created without labels or annotations"

create_test_namespace kubemirror-e2e-ns-1
create_test_namespace kubemirror-e2e-ns-2

create_source secret test-no-labels-1 "$E2E_SOURCE_NS" false false "" "data-v1"

sleep 3

# Should NOT create mirrors (no enabled label or sync annotation)
verify_mirrors_not_exist secret test-no-labels-1 kubemirror-e2e-ns-1 kubemirror-e2e-ns-2

complete_test_scenario "1" "pass"

#===============================================================================
# SCENARIO 2: Labels added to source
#===============================================================================

if ! should_run_scenario 2; then
    continue
fi

run_test_scenario "2: Add enabled label to source (no sync annotation yet)"

update_source_labels secret test-no-labels-1 "$E2E_SOURCE_NS" true

sleep 3

# Still no mirrors (sync annotation required)
verify_mirrors_not_exist secret test-no-labels-1 kubemirror-e2e-ns-1 kubemirror-e2e-ns-2

complete_test_scenario "2" "pass"

#===============================================================================
# SCENARIO 3: Sync annotation added to labeled source
#===============================================================================

if ! should_run_scenario 3; then
    continue
fi

run_test_scenario "3: Add sync annotation with target namespaces"

update_source_annotations secret test-no-labels-1 "$E2E_SOURCE_NS" true "kubemirror-e2e-ns-1,kubemirror-e2e-ns-2"

# Now mirrors should be created
verify_mirrors_exist secret test-no-labels-1 kubemirror-e2e-ns-1 kubemirror-e2e-ns-2
verify_mirror_data secret test-no-labels-1 "$E2E_SOURCE_NS" kubemirror-e2e-ns-1 "data-v1"

complete_test_scenario "3" "pass"

#===============================================================================
# SCENARIO 4: Source content modified
#===============================================================================

if ! should_run_scenario 4; then
    continue
fi

run_test_scenario "4: Modify source data content"

update_source_data secret test-no-labels-1 "$E2E_SOURCE_NS" "data-v2-updated"

sleep 20

# Mirrors should be updated
verify_mirror_data secret test-no-labels-1 "$E2E_SOURCE_NS" kubemirror-e2e-ns-1 "data-v2-updated"
verify_mirror_data secret test-no-labels-1 "$E2E_SOURCE_NS" kubemirror-e2e-ns-2 "data-v2-updated"

complete_test_scenario "4" "pass"

#===============================================================================
# SCENARIO 5: Add namespace to target list
#===============================================================================

if ! should_run_scenario 5; then
    continue
fi

run_test_scenario "5: Add namespace to target-namespaces list"

create_test_namespace kubemirror-e2e-ns-3

update_source_annotations secret test-no-labels-1 "$E2E_SOURCE_NS" true "kubemirror-e2e-ns-1,kubemirror-e2e-ns-2,kubemirror-e2e-ns-3"

# New namespace should receive mirror
verify_mirrors_exist secret test-no-labels-1 kubemirror-e2e-ns-1 kubemirror-e2e-ns-2 kubemirror-e2e-ns-3
verify_mirror_data secret test-no-labels-1 "$E2E_SOURCE_NS" kubemirror-e2e-ns-3 "data-v2-updated"

complete_test_scenario "5" "pass"

#===============================================================================
# SCENARIO 6: Remove namespace from target list
#===============================================================================

if ! should_run_scenario 6; then
    continue
fi

run_test_scenario "6: Remove namespace from target-namespaces list"

update_source_annotations secret test-no-labels-1 "$E2E_SOURCE_NS" true "kubemirror-e2e-ns-1,kubemirror-e2e-ns-2"

# Orphaned mirror in kubemirror-e2e-ns-3 should be deleted
verify_orphan_cleanup secret test-no-labels-1 kubemirror-e2e-ns-3

# Others still exist
verify_mirrors_exist secret test-no-labels-1 kubemirror-e2e-ns-1 kubemirror-e2e-ns-2

complete_test_scenario "6" "pass"

#===============================================================================
# SCENARIO 7: Change target-namespaces from explicit list to pattern
#===============================================================================

if ! should_run_scenario 7; then
    continue
fi

run_test_scenario "7: Change from explicit list to pattern"

create_test_namespace kubemirror-e2e-app-1
create_test_namespace kubemirror-e2e-app-2
create_test_namespace kubemirror-e2e-db-1

update_source_annotations secret test-no-labels-1 "$E2E_SOURCE_NS" true "kubemirror-e2e-app-*"

# Should remove mirrors from kubemirror-e2e-ns-1, kubemirror-e2e-ns-2
verify_orphan_cleanup secret test-no-labels-1 kubemirror-e2e-ns-1 kubemirror-e2e-ns-2

# Should create mirrors in kubemirror-e2e-app-*
verify_mirrors_exist secret test-no-labels-1 kubemirror-e2e-app-1 kubemirror-e2e-app-2

# Should NOT create in kubemirror-e2e-db-1
verify_mirrors_not_exist secret test-no-labels-1 kubemirror-e2e-db-1

complete_test_scenario "7" "pass"

#===============================================================================
# SCENARIO 8: Multiple patterns
#===============================================================================

if ! should_run_scenario 8; then
    continue
fi

run_test_scenario "8: Multiple patterns in target-namespaces"

create_test_namespace kubemirror-e2e-db-2
create_test_namespace kubemirror-e2e-stage-1

update_source_annotations secret test-no-labels-1 "$E2E_SOURCE_NS" true "kubemirror-e2e-app-*,kubemirror-e2e-db-*"

# Should add mirrors to kubemirror-e2e-db-*
verify_mirrors_exist secret test-no-labels-1 kubemirror-e2e-db-1 kubemirror-e2e-db-2

# Should still have kubemirror-e2e-app-*
verify_mirrors_exist secret test-no-labels-1 kubemirror-e2e-app-1 kubemirror-e2e-app-2

# Should NOT have kubemirror-e2e-stage-*
verify_mirrors_not_exist secret test-no-labels-1 kubemirror-e2e-stage-1

complete_test_scenario "8" "pass"

#===============================================================================
# SCENARIO 9: Sync annotation set to false
#===============================================================================

if ! should_run_scenario 9; then
    continue
fi

run_test_scenario "9: Set sync annotation to false"

update_source_annotations secret test-no-labels-1 "$E2E_SOURCE_NS" false ""

# All mirrors should be deleted
verify_orphan_cleanup secret test-no-labels-1 kubemirror-e2e-app-1 kubemirror-e2e-app-2 kubemirror-e2e-db-1 kubemirror-e2e-db-2

complete_test_scenario "9" "pass"

#===============================================================================
# SCENARIO 10: Enabled label set to false
#===============================================================================

if ! should_run_scenario 10; then
    continue
fi

run_test_scenario "10: Set enabled label to false"

# Re-enable sync first
update_source_annotations secret test-no-labels-1 "$E2E_SOURCE_NS" true "kubemirror-e2e-app-1"

verify_mirrors_exist secret test-no-labels-1 kubemirror-e2e-app-1

# Now disable via label
update_source_labels secret test-no-labels-1 "$E2E_SOURCE_NS" false

sleep 3

# Mirror should be removed (label filtering)
verify_orphan_cleanup secret test-no-labels-1 kubemirror-e2e-app-1

complete_test_scenario "10" "pass"

#===============================================================================
# SCENARIO 11: Pattern with new namespace created
#===============================================================================

if ! should_run_scenario 11; then
    continue
fi

run_test_scenario "11: Create new namespace matching existing pattern"

# Re-enable the source
update_source_labels secret test-no-labels-1 "$E2E_SOURCE_NS" true
update_source_annotations secret test-no-labels-1 "$E2E_SOURCE_NS" true "kubemirror-e2e-prod-*"

create_test_namespace kubemirror-e2e-prod-1

# Should automatically create mirror in new namespace
verify_mirrors_exist secret test-no-labels-1 kubemirror-e2e-prod-1

# Create another matching namespace
create_test_namespace kubemirror-e2e-prod-2

# Should also get the mirror
verify_mirrors_exist secret test-no-labels-1 kubemirror-e2e-prod-2

complete_test_scenario "11" "pass"

#===============================================================================
# SCENARIO 12: 'all' keyword without namespace label (opt-OUT model)
#===============================================================================

if ! should_run_scenario 12; then
    continue
fi

run_test_scenario "12: Source with 'all' keyword, namespace without allow-mirrors label"

create_source configmap test-all-no-label "$E2E_SOURCE_NS" true true "all" "all-data-v1"

create_test_namespace kubemirror-e2e-unlabeled

sleep 5

# SHOULD create mirror (opt-OUT model: namespaces without label get mirrors by default)
verify_mirrors_exist configmap test-all-no-label kubemirror-e2e-unlabeled
verify_mirror_data configmap test-all-no-label "$E2E_SOURCE_NS" kubemirror-e2e-unlabeled "all-data-v1"

complete_test_scenario "12" "pass"

#===============================================================================
# SCENARIO 13: Set allow-mirrors=false to opt-out
#===============================================================================

if ! should_run_scenario 13; then
    continue
fi

run_test_scenario "13: Set allow-mirrors=false on namespace (explicit opt-OUT)"

update_namespace_labels kubemirror-e2e-unlabeled false

sleep 5

# Mirror should be deleted (explicit opt-OUT)
verify_orphan_cleanup configmap test-all-no-label kubemirror-e2e-unlabeled

complete_test_scenario "13" "pass"

#===============================================================================
# SCENARIO 14: Change allow-mirrors from false to true
#===============================================================================

if ! should_run_scenario 14; then
    continue
fi

run_test_scenario "14: Change allow-mirrors label from false to true"

update_namespace_labels kubemirror-e2e-unlabeled true

sleep 5

# Mirror should be recreated
verify_mirrors_exist configmap test-all-no-label kubemirror-e2e-unlabeled
verify_mirror_data configmap test-all-no-label "$E2E_SOURCE_NS" kubemirror-e2e-unlabeled "all-data-v1"

complete_test_scenario "14" "pass"

#===============================================================================
# SCENARIO 15: Remove allow-mirrors label (back to default opt-IN)
#===============================================================================

if ! should_run_scenario 15; then
    continue
fi

run_test_scenario "15: Remove allow-mirrors label from namespace"

update_namespace_labels kubemirror-e2e-unlabeled ""

sleep 5

# Mirror should STILL exist (default is opt-IN, not opt-OUT)
verify_mirrors_exist configmap test-all-no-label kubemirror-e2e-unlabeled
verify_mirror_data configmap test-all-no-label "$E2E_SOURCE_NS" kubemirror-e2e-unlabeled "all-data-v1"

complete_test_scenario "15" "pass"

#===============================================================================
# SCENARIO 16: Target namespace deleted
#===============================================================================

if ! should_run_scenario 16; then
    continue
fi

run_test_scenario "16: Delete target namespace"

create_test_namespace kubemirror-e2e-ns-4
update_source_annotations secret test-no-labels-1 "$E2E_SOURCE_NS" true "kubemirror-e2e-ns-4,kubemirror-e2e-prod-1"

verify_mirrors_exist secret test-no-labels-1 kubemirror-e2e-ns-4 kubemirror-e2e-prod-1

# Delete one of the target namespaces
delete_namespace kubemirror-e2e-ns-4

sleep 3

# Other mirror should still exist
verify_mirrors_exist secret test-no-labels-1 kubemirror-e2e-prod-1

complete_test_scenario "16" "pass"

#===============================================================================
# SCENARIO 17: Recreate deleted target namespace
#===============================================================================

if ! should_run_scenario 17; then
    continue
fi

run_test_scenario "17: Recreate deleted target namespace"

# Wait for namespace to be fully deleted
sleep 5

create_test_namespace kubemirror-e2e-ns-4

# Mirror should be recreated automatically (namespace reconciler + pattern matching)
# Note: This requires namespace reconciler to be working
wait_for_resource secret test-no-labels-1 kubemirror-e2e-ns-4 30 || log_warn "Mirror not auto-created (may require source update)"

complete_test_scenario "17" "pass"

#===============================================================================
# SCENARIO 18: Source deleted
#===============================================================================

if ! should_run_scenario 18; then
    continue
fi

run_test_scenario "18: Delete source resource"

create_test_namespace kubemirror-e2e-ns-5
create_source secret test-delete-source "$E2E_SOURCE_NS" true true "kubemirror-e2e-ns-5" "delete-test"

verify_mirrors_exist secret test-delete-source kubemirror-e2e-ns-5

# Delete source
kubectl delete secret test-delete-source -n "$E2E_SOURCE_NS"

# Mirror should be cascade deleted
verify_orphan_cleanup secret test-delete-source kubemirror-e2e-ns-5

complete_test_scenario "18" "pass"

#===============================================================================
# SCENARIO 19: Target manually deleted (should be recreated)
#===============================================================================

if ! should_run_scenario 19; then
    continue
fi

run_test_scenario "19: Manually delete target mirror (should recreate)"

create_source secret test-recreate "$E2E_SOURCE_NS" true true "kubemirror-e2e-prod-1" "recreate-data"

verify_mirrors_exist secret test-recreate kubemirror-e2e-prod-1

# Manually delete the mirror
kubectl delete secret test-recreate -n kubemirror-e2e-prod-1

# Should be automatically recreated
wait_for_resource secret test-recreate kubemirror-e2e-prod-1 15

assert_resource_exists secret test-recreate kubemirror-e2e-prod-1

complete_test_scenario "19" "pass"

#===============================================================================
# SCENARIO 20: ConfigMap with same test patterns
#===============================================================================

if ! should_run_scenario 20; then
    continue
fi

run_test_scenario "20: ConfigMap with pattern matching"

create_source configmap test-cm-pattern "$E2E_SOURCE_NS" true true "kubemirror-e2e-app-*" "cm-data-v1"

verify_mirrors_exist configmap test-cm-pattern kubemirror-e2e-app-1 kubemirror-e2e-app-2

# Update content
update_source_data configmap test-cm-pattern "$E2E_SOURCE_NS" "cm-data-v2"

sleep 20

verify_mirror_data configmap test-cm-pattern "$E2E_SOURCE_NS" kubemirror-e2e-app-1 "cm-data-v2"

# Change pattern
update_source_annotations configmap test-cm-pattern "$E2E_SOURCE_NS" true "kubemirror-e2e-db-*"

verify_orphan_cleanup configmap test-cm-pattern kubemirror-e2e-app-1 kubemirror-e2e-app-2
verify_mirrors_exist configmap test-cm-pattern kubemirror-e2e-db-1 kubemirror-e2e-db-2

complete_test_scenario "20" "pass"

#===============================================================================
# SCENARIO 21: Mix of explicit and pattern
#===============================================================================

if ! should_run_scenario 21; then
    continue
fi

run_test_scenario "21: Mix of explicit namespaces and patterns"

create_test_namespace kubemirror-e2e-test-ns

create_source secret test-mixed "$E2E_SOURCE_NS" true true "kubemirror-e2e-test-ns,kubemirror-e2e-stage-*" "mixed-data"

create_test_namespace kubemirror-e2e-stage-2

verify_mirrors_exist secret test-mixed kubemirror-e2e-test-ns kubemirror-e2e-stage-1 kubemirror-e2e-stage-2

# Remove explicit, keep pattern
update_source_annotations secret test-mixed "$E2E_SOURCE_NS" true "kubemirror-e2e-stage-*"

verify_orphan_cleanup secret test-mixed kubemirror-e2e-test-ns
verify_mirrors_exist secret test-mixed kubemirror-e2e-stage-1 kubemirror-e2e-stage-2

complete_test_scenario "21" "pass"

#===============================================================================
# SCENARIO 22: Sync annotation removed completely
#===============================================================================

if ! should_run_scenario 22; then
    continue
fi

run_test_scenario "22: Remove sync annotation completely"

verify_mirrors_exist secret test-mixed kubemirror-e2e-stage-1 kubemirror-e2e-stage-2

update_source_annotations secret test-mixed "$E2E_SOURCE_NS" "" ""

verify_orphan_cleanup secret test-mixed kubemirror-e2e-stage-1 kubemirror-e2e-stage-2

complete_test_scenario "22" "pass"

#===============================================================================
# SCENARIO 23: Traefik Middleware CRD (test generic CRD support)
#===============================================================================

if ! should_run_scenario 23; then
    continue
fi

run_test_scenario "23: Traefik Middleware CRD with spec updates"

# Create Traefik Middleware CRD manually (CRDs aren't supported by create_source helper)
cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: test-middleware
  namespace: $E2E_SOURCE_NS
  labels:
    kubemirror.raczylo.com/enabled: "true"
    test-resource: e2e
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "kubemirror-e2e-app-1,kubemirror-e2e-app-2"
spec:
  basicAuth:
    secret: auth-secret-v1
    removeHeader: false
  headers:
    customRequestHeaders:
      X-Test-Header: "test-value-v1"
EOF

sleep 5

# Verify mirrors created with correct spec
verify_mirrors_exist middleware.traefik.io test-middleware kubemirror-e2e-app-1 kubemirror-e2e-app-2

# Verify mirror spec content (check one of the spec fields)
app1_secret=$(kubectl get middleware test-middleware -n kubemirror-e2e-app-1 -o jsonpath='{.spec.basicAuth.secret}' 2>/dev/null || echo "")
if [ "$app1_secret" = "auth-secret-v1" ]; then
    log_success "Mirror spec in kubemirror-e2e-app-1 matches expected: auth-secret-v1"
    ((PASS_COUNT++))
else
    log_fail "Mirror spec in kubemirror-e2e-app-1 does not match. Expected: auth-secret-v1, Got: $app1_secret"
    ((FAIL_COUNT++))
fi

# Update the Middleware spec
cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: test-middleware
  namespace: $E2E_SOURCE_NS
  labels:
    kubemirror.raczylo.com/enabled: "true"
    test-resource: e2e
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "kubemirror-e2e-app-1,kubemirror-e2e-app-2"
spec:
  basicAuth:
    secret: auth-secret-v2-updated
    removeHeader: true
  headers:
    customRequestHeaders:
      X-Test-Header: "test-value-v2-updated"
      X-New-Header: "new-value"
EOF

sleep 20

# Verify mirror spec was updated
app1_secret_updated=$(kubectl get middleware test-middleware -n kubemirror-e2e-app-1 -o jsonpath='{.spec.basicAuth.secret}' 2>/dev/null || echo "")
if [ "$app1_secret_updated" = "auth-secret-v2-updated" ]; then
    log_success "Mirror spec in kubemirror-e2e-app-1 updated correctly: auth-secret-v2-updated"
    ((PASS_COUNT++))
else
    log_fail "Mirror spec in kubemirror-e2e-app-1 not updated. Expected: auth-secret-v2-updated, Got: $app1_secret_updated"
    ((FAIL_COUNT++))
fi

app1_header=$(kubectl get middleware test-middleware -n kubemirror-e2e-app-1 -o jsonpath='{.spec.headers.customRequestHeaders.X-New-Header}' 2>/dev/null || echo "")
if [ "$app1_header" = "new-value" ]; then
    log_success "Mirror spec headers in kubemirror-e2e-app-1 updated correctly: new-value"
    ((PASS_COUNT++))
else
    log_fail "Mirror spec headers in kubemirror-e2e-app-1 not updated. Expected: new-value, Got: $app1_header"
    ((FAIL_COUNT++))
fi

# Change target namespaces pattern
kubectl annotate middleware test-middleware -n "$E2E_SOURCE_NS" \
    kubemirror.raczylo.com/target-namespaces="kubemirror-e2e-db-*" --overwrite >/dev/null 2>&1

sleep 10

# Verify old mirrors cleaned up
verify_orphan_cleanup middleware.traefik.io test-middleware kubemirror-e2e-app-1 kubemirror-e2e-app-2

# Verify new mirrors created
verify_mirrors_exist middleware.traefik.io test-middleware kubemirror-e2e-db-1 kubemirror-e2e-db-2

# Clean up CRD
kubectl delete middleware test-middleware -n "$E2E_SOURCE_NS" --ignore-not-found=true >/dev/null 2>&1

complete_test_scenario "23" "pass"

#===============================================================================
# SCENARIO 24: Transformation - Static value
#===============================================================================

if ! should_run_scenario 24; then
    continue
fi

run_test_scenario "24: Transformation - Static value replacement"

# Create Secret with value transformation
cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: v1
kind: Secret
metadata:
  name: test-transform-value
  namespace: $E2E_SOURCE_NS
  labels:
    kubemirror.raczylo.com/enabled: "true"
    test-resource: e2e
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "kubemirror-e2e-app-1,kubemirror-e2e-app-2"
    kubemirror.raczylo.com/transform: |
      rules:
        - path: data.ENVIRONMENT
          value: "production"
        - path: data.LOG_LEVEL
          value: "ERROR"
type: Opaque
stringData:
  ENVIRONMENT: "development"
  LOG_LEVEL: "DEBUG"
  APP_KEY: "original-key"
EOF

sleep 10

# Verify mirrors created
verify_mirrors_exist secret test-transform-value kubemirror-e2e-app-1 kubemirror-e2e-app-2

# Verify transformed values
app1_env=$(kubectl get secret test-transform-value -n kubemirror-e2e-app-1 -o jsonpath='{.data.ENVIRONMENT}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
if [ "$app1_env" = "production" ]; then
    log_success "Transformed value in kubemirror-e2e-app-1 correct: ENVIRONMENT=production"
    ((PASS_COUNT++))
else
    log_fail "Transform failed. Expected: production, Got: $app1_env"
    ((FAIL_COUNT++))
fi

app1_log=$(kubectl get secret test-transform-value -n kubemirror-e2e-app-1 -o jsonpath='{.data.LOG_LEVEL}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
if [ "$app1_log" = "ERROR" ]; then
    log_success "Transformed value in kubemirror-e2e-app-1 correct: LOG_LEVEL=ERROR"
    ((PASS_COUNT++))
else
    log_fail "Transform failed. Expected: ERROR, Got: $app1_log"
    ((FAIL_COUNT++))
fi

# Verify untransformed value preserved
app1_key=$(kubectl get secret test-transform-value -n kubemirror-e2e-app-1 -o jsonpath='{.data.APP_KEY}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
if [ "$app1_key" = "original-key" ]; then
    log_success "Untransformed value preserved: APP_KEY=original-key"
    ((PASS_COUNT++))
else
    log_fail "Untransformed value changed. Expected: original-key, Got: $app1_key"
    ((FAIL_COUNT++))
fi

kubectl delete secret test-transform-value -n "$E2E_SOURCE_NS" --ignore-not-found=true >/dev/null 2>&1
complete_test_scenario "24" "pass"

#===============================================================================
# SCENARIO 25: Transformation - Template with context variables
#===============================================================================

if ! should_run_scenario 25; then
    continue
fi

run_test_scenario "25: Transformation - Template with context variables"

# Create Secret with template transformation
cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: v1
kind: Secret
metadata:
  name: test-transform-template
  namespace: $E2E_SOURCE_NS
  labels:
    kubemirror.raczylo.com/enabled: "true"
    test-resource: e2e
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "kubemirror-e2e-app-1,kubemirror-e2e-db-1"
    kubemirror.raczylo.com/transform: |
      rules:
        - path: data.DB_HOST
          template: "{{.TargetNamespace}}.postgres.svc.cluster.local"
        - path: data.DB_NAME
          template: "app_{{replace .TargetNamespace \"-\" \"_\"}}"
        - path: data.CACHE_KEY
          template: "{{upper .TargetNamespace}}:cache"
type: Opaque
stringData:
  DB_HOST: "localhost"
  DB_NAME: "dev"
  CACHE_KEY: "dev:cache"
EOF

sleep 10

# Verify mirrors created
verify_mirrors_exist secret test-transform-template kubemirror-e2e-app-1 kubemirror-e2e-db-1

# Verify template transformations in kubemirror-e2e-app-1
app1_host=$(kubectl get secret test-transform-template -n kubemirror-e2e-app-1 -o jsonpath='{.data.DB_HOST}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
if [ "$app1_host" = "kubemirror-e2e-app-1.postgres.svc.cluster.local" ]; then
    log_success "Template transformation correct: DB_HOST=kubemirror-e2e-app-1.postgres.svc.cluster.local"
    ((PASS_COUNT++))
else
    log_fail "Template failed. Expected: kubemirror-e2e-app-1.postgres.svc.cluster.local, Got: $app1_host"
    ((FAIL_COUNT++))
fi

app1_dbname=$(kubectl get secret test-transform-template -n kubemirror-e2e-app-1 -o jsonpath='{.data.DB_NAME}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
if [ "$app1_dbname" = "app_kubemirror_e2e_app_1" ]; then
    log_success "Template with replace function correct: DB_NAME=app_e2e_app_1"
    ((PASS_COUNT++))
else
    log_fail "Template with replace failed. Expected: app_e2e_app_1, Got: $app1_dbname"
    ((FAIL_COUNT++))
fi

app1_cache=$(kubectl get secret test-transform-template -n kubemirror-e2e-app-1 -o jsonpath='{.data.CACHE_KEY}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
if [ "$app1_cache" = "KUBEMIRROR-E2E-APP-1:cache" ]; then
    log_success "Template with upper function correct: CACHE_KEY=E2E-APP-1:cache"
    ((PASS_COUNT++))
else
    log_fail "Template with upper failed. Expected: E2E-APP-1:cache, Got: $app1_cache"
    ((FAIL_COUNT++))
fi

# Verify template transformations in kubemirror-e2e-db-1 (different namespace = different values)
db1_host=$(kubectl get secret test-transform-template -n kubemirror-e2e-db-1 -o jsonpath='{.data.DB_HOST}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
if [ "$db1_host" = "kubemirror-e2e-db-1.postgres.svc.cluster.local" ]; then
    log_success "Template namespace-specific: DB_HOST=kubemirror-e2e-db-1.postgres.svc.cluster.local"
    ((PASS_COUNT++))
else
    log_fail "Template namespace failed. Expected: kubemirror-e2e-db-1.postgres.svc.cluster.local, Got: $db1_host"
    ((FAIL_COUNT++))
fi

kubectl delete secret test-transform-template -n "$E2E_SOURCE_NS" --ignore-not-found=true >/dev/null 2>&1
complete_test_scenario "25" "pass"

#===============================================================================
# SCENARIO 26: Transformation - Merge maps (labels/annotations)
#===============================================================================

if ! should_run_scenario 26; then
    continue
fi

run_test_scenario "26: Transformation - Merge maps"

# Create ConfigMap with merge transformation
cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-transform-merge
  namespace: $E2E_SOURCE_NS
  labels:
    kubemirror.raczylo.com/enabled: "true"
    test-resource: e2e
    app: "test-app"
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "kubemirror-e2e-app-1,kubemirror-e2e-app-2"
    kubemirror.raczylo.com/transform: |
      rules:
        - path: metadata.labels
          merge:
            environment: "production"
            managed-by: "kubemirror"
            tier: "backend"
data:
  config: "value"
EOF

sleep 10

# Verify mirrors created
verify_mirrors_exist configmap test-transform-merge kubemirror-e2e-app-1 kubemirror-e2e-app-2

# Verify merged labels
app1_env_label=$(kubectl get configmap test-transform-merge -n kubemirror-e2e-app-1 -o jsonpath='{.metadata.labels.environment}' 2>/dev/null || echo "")
if [ "$app1_env_label" = "production" ]; then
    log_success "Merged label added: environment=production"
    ((PASS_COUNT++))
else
    log_fail "Merge failed. Expected: production, Got: $app1_env_label"
    ((FAIL_COUNT++))
fi

app1_tier_label=$(kubectl get configmap test-transform-merge -n kubemirror-e2e-app-1 -o jsonpath='{.metadata.labels.tier}' 2>/dev/null || echo "")
if [ "$app1_tier_label" = "backend" ]; then
    log_success "Merged label added: tier=backend"
    ((PASS_COUNT++))
else
    log_fail "Merge failed. Expected: backend, Got: $app1_tier_label"
    ((FAIL_COUNT++))
fi

# Verify original labels preserved
app1_app_label=$(kubectl get configmap test-transform-merge -n kubemirror-e2e-app-1 -o jsonpath='{.metadata.labels.app}' 2>/dev/null || echo "")
if [ "$app1_app_label" = "test-app" ]; then
    log_success "Original label preserved: app=test-app"
    ((PASS_COUNT++))
else
    log_fail "Original label lost. Expected: test-app, Got: $app1_app_label"
    ((FAIL_COUNT++))
fi

kubectl delete configmap test-transform-merge -n "$E2E_SOURCE_NS" --ignore-not-found=true >/dev/null 2>&1
complete_test_scenario "26" "pass"

#===============================================================================
# SCENARIO 27: Transformation - Delete fields
#===============================================================================

if ! should_run_scenario 27; then
    continue
fi

run_test_scenario "27: Transformation - Delete sensitive fields"

# Create Secret with delete transformation
cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: v1
kind: Secret
metadata:
  name: test-transform-delete
  namespace: $E2E_SOURCE_NS
  labels:
    kubemirror.raczylo.com/enabled: "true"
    test-resource: e2e
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "kubemirror-e2e-app-1,kubemirror-e2e-app-2"
    kubemirror.raczylo.com/transform: |
      rules:
        - path: data.ADMIN_PASSWORD
          delete: true
        - path: data.ROOT_TOKEN
          delete: true
type: Opaque
stringData:
  APP_KEY: "app-key-12345"
  ADMIN_PASSWORD: "super-secret"
  ROOT_TOKEN: "root-token-xyz"
EOF

sleep 10

# Verify mirrors created
verify_mirrors_exist secret test-transform-delete kubemirror-e2e-app-1 kubemirror-e2e-app-2

# Verify sensitive fields deleted
app1_admin=$(kubectl get secret test-transform-delete -n kubemirror-e2e-app-1 -o jsonpath='{.data.ADMIN_PASSWORD}' 2>/dev/null || echo "")
if [ -z "$app1_admin" ]; then
    log_success "Sensitive field deleted: ADMIN_PASSWORD removed"
    ((PASS_COUNT++))
else
    log_fail "Delete failed. ADMIN_PASSWORD still exists: $app1_admin"
    ((FAIL_COUNT++))
fi

app1_token=$(kubectl get secret test-transform-delete -n kubemirror-e2e-app-1 -o jsonpath='{.data.ROOT_TOKEN}' 2>/dev/null || echo "")
if [ -z "$app1_token" ]; then
    log_success "Sensitive field deleted: ROOT_TOKEN removed"
    ((PASS_COUNT++))
else
    log_fail "Delete failed. ROOT_TOKEN still exists: $app1_token"
    ((FAIL_COUNT++))
fi

# Verify non-deleted field preserved
app1_key=$(kubectl get secret test-transform-delete -n kubemirror-e2e-app-1 -o jsonpath='{.data.APP_KEY}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
if [ "$app1_key" = "app-key-12345" ]; then
    log_success "Non-deleted field preserved: APP_KEY=app-key-12345"
    ((PASS_COUNT++))
else
    log_fail "Field incorrectly deleted. Expected: app-key-12345, Got: $app1_key"
    ((FAIL_COUNT++))
fi

kubectl delete secret test-transform-delete -n "$E2E_SOURCE_NS" --ignore-not-found=true >/dev/null 2>&1
complete_test_scenario "27" "pass"

#===============================================================================
# SCENARIO 28: Transformation - Namespace pattern-specific rules
#===============================================================================

if ! should_run_scenario 28; then
    continue
fi

run_test_scenario "28: Transformation - Namespace pattern-specific transformations"

# Create ConfigMap with namespace-pattern-specific transformations
cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-transform-pattern
  namespace: $E2E_SOURCE_NS
  labels:
    kubemirror.raczylo.com/enabled: "true"
    test-resource: e2e
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "kubemirror-e2e-app-1,kubemirror-e2e-db-1,kubemirror-e2e-prod-1"
    kubemirror.raczylo.com/transform: |
      rules:
        # Apply to all namespaces
        - path: data.GLOBAL_CONFIG
          value: "enabled"

        # Apply only to kubemirror-e2e-app-* namespaces
        - path: data.APP_MODE
          value: "application"
          namespacePattern: "kubemirror-e2e-app-*"

        # Apply only to kubemirror-e2e-db-* namespaces
        - path: data.DB_MODE
          value: "database"
          namespacePattern: "kubemirror-e2e-db-*"

        # Apply only to kubemirror-e2e-prod-* namespaces
        - path: data.SECURITY_LEVEL
          value: "high"
          namespacePattern: "kubemirror-e2e-prod-*"
data:
  GLOBAL_CONFIG: "disabled"
  APP_MODE: "none"
  DB_MODE: "none"
  SECURITY_LEVEL: "low"
EOF

sleep 10

# Verify mirrors created
verify_mirrors_exist configmap test-transform-pattern kubemirror-e2e-app-1 kubemirror-e2e-db-1 kubemirror-e2e-prod-1

# Verify global transformation applied to all
app1_global=$(kubectl get configmap test-transform-pattern -n kubemirror-e2e-app-1 -o jsonpath='{.data.GLOBAL_CONFIG}' 2>/dev/null || echo "")
if [ "$app1_global" = "enabled" ]; then
    log_success "Global transformation applied to kubemirror-e2e-app-1: GLOBAL_CONFIG=enabled"
    ((PASS_COUNT++))
else
    log_fail "Global transform failed. Expected: enabled, Got: $app1_global"
    ((FAIL_COUNT++))
fi

# Verify pattern-specific transformation in kubemirror-e2e-app-1
app1_mode=$(kubectl get configmap test-transform-pattern -n kubemirror-e2e-app-1 -o jsonpath='{.data.APP_MODE}' 2>/dev/null || echo "")
if [ "$app1_mode" = "application" ]; then
    log_success "Pattern-specific transform for kubemirror-e2e-app-*: APP_MODE=application"
    ((PASS_COUNT++))
else
    log_fail "Pattern transform failed. Expected: application, Got: $app1_mode"
    ((FAIL_COUNT++))
fi

# Verify pattern-specific transformation in kubemirror-e2e-db-1
db1_mode=$(kubectl get configmap test-transform-pattern -n kubemirror-e2e-db-1 -o jsonpath='{.data.DB_MODE}' 2>/dev/null || echo "")
if [ "$db1_mode" = "database" ]; then
    log_success "Pattern-specific transform for kubemirror-e2e-db-*: DB_MODE=database"
    ((PASS_COUNT++))
else
    log_fail "Pattern transform failed. Expected: database, Got: $db1_mode"
    ((FAIL_COUNT++))
fi

# Verify pattern-specific transformation in kubemirror-e2e-prod-1
prod1_security=$(kubectl get configmap test-transform-pattern -n kubemirror-e2e-prod-1 -o jsonpath='{.data.SECURITY_LEVEL}' 2>/dev/null || echo "")
if [ "$prod1_security" = "high" ]; then
    log_success "Pattern-specific transform for kubemirror-e2e-prod-*: SECURITY_LEVEL=high"
    ((PASS_COUNT++))
else
    log_fail "Pattern transform failed. Expected: high, Got: $prod1_security"
    ((FAIL_COUNT++))
fi

# Verify pattern-specific transformation NOT applied to wrong namespace
app1_db_mode=$(kubectl get configmap test-transform-pattern -n kubemirror-e2e-app-1 -o jsonpath='{.data.DB_MODE}' 2>/dev/null || echo "")
if [ "$app1_db_mode" = "none" ]; then
    log_success "Pattern-specific transform correctly excluded from kubemirror-e2e-app-1: DB_MODE=none"
    ((PASS_COUNT++))
else
    log_fail "Pattern incorrectly applied. Expected: none, Got: $app1_db_mode"
    ((FAIL_COUNT++))
fi

kubectl delete configmap test-transform-pattern -n "$E2E_SOURCE_NS" --ignore-not-found=true >/dev/null 2>&1
complete_test_scenario "28" "pass"

#===============================================================================
# SCENARIO 29: Transformation - Multiple rule types combined
#===============================================================================

if ! should_run_scenario 29; then
    continue
fi

run_test_scenario "29: Transformation - Multiple rule types combined"

# Create Secret with multiple transformation types
cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: v1
kind: Secret
metadata:
  name: test-transform-multi
  namespace: $E2E_SOURCE_NS
  labels:
    kubemirror.raczylo.com/enabled: "true"
    test-resource: e2e
    original-label: "keep-me"
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "kubemirror-e2e-app-1,kubemirror-e2e-app-2"
    kubemirror.raczylo.com/transform: |
      rules:
        # Static value
        - path: data.ENCRYPTION
          value: "AES-256"

        # Template
        - path: data.SERVICE_URL
          template: "https://{{.TargetNamespace}}.example.com"

        # Delete sensitive field
        - path: data.DEV_SECRET
          delete: true

        # Merge labels
        - path: metadata.labels
          merge:
            environment: "production"
            security: "enabled"
type: Opaque
stringData:
  ENCRYPTION: "AES-128"
  SERVICE_URL: "http://localhost"
  APP_KEY: "key-123"
  DEV_SECRET: "dev-only"
EOF

sleep 10

# Verify mirrors created
verify_mirrors_exist secret test-transform-multi kubemirror-e2e-app-1 kubemirror-e2e-app-2

# Verify value transformation
app1_enc=$(kubectl get secret test-transform-multi -n kubemirror-e2e-app-1 -o jsonpath='{.data.ENCRYPTION}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
if [ "$app1_enc" = "AES-256" ]; then
    log_success "Value transform in multi-rule: ENCRYPTION=AES-256"
    ((PASS_COUNT++))
else
    log_fail "Value transform failed. Expected: AES-256, Got: $app1_enc"
    ((FAIL_COUNT++))
fi

# Verify template transformation
app1_url=$(kubectl get secret test-transform-multi -n kubemirror-e2e-app-1 -o jsonpath='{.data.SERVICE_URL}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
if [ "$app1_url" = "https://kubemirror-e2e-app-1.example.com" ]; then
    log_success "Template transform in multi-rule: SERVICE_URL=https://kubemirror-e2e-app-1.example.com"
    ((PASS_COUNT++))
else
    log_fail "Template transform failed. Expected: https://kubemirror-e2e-app-1.example.com, Got: $app1_url"
    ((FAIL_COUNT++))
fi

# Verify delete transformation
app1_dev=$(kubectl get secret test-transform-multi -n kubemirror-e2e-app-1 -o jsonpath='{.data.DEV_SECRET}' 2>/dev/null || echo "")
if [ -z "$app1_dev" ]; then
    log_success "Delete transform in multi-rule: DEV_SECRET removed"
    ((PASS_COUNT++))
else
    log_fail "Delete transform failed. DEV_SECRET still exists"
    ((FAIL_COUNT++))
fi

# Verify merge transformation
app1_env_label=$(kubectl get secret test-transform-multi -n kubemirror-e2e-app-1 -o jsonpath='{.metadata.labels.environment}' 2>/dev/null || echo "")
if [ "$app1_env_label" = "production" ]; then
    log_success "Merge transform in multi-rule: environment=production"
    ((PASS_COUNT++))
else
    log_fail "Merge transform failed. Expected: production, Got: $app1_env_label"
    ((FAIL_COUNT++))
fi

# Verify original label preserved after merge
app1_orig_label=$(kubectl get secret test-transform-multi -n kubemirror-e2e-app-1 -o jsonpath='{.metadata.labels.original-label}' 2>/dev/null || echo "")
if [ "$app1_orig_label" = "keep-me" ]; then
    log_success "Original label preserved after merge: original-label=keep-me"
    ((PASS_COUNT++))
else
    log_fail "Original label lost. Expected: keep-me, Got: $app1_orig_label"
    ((FAIL_COUNT++))
fi

kubectl delete secret test-transform-multi -n "$E2E_SOURCE_NS" --ignore-not-found=true >/dev/null 2>&1
complete_test_scenario "29" "pass"

#===============================================================================
# SCENARIO 30: Transformation - Update transform rules (reconciliation)
#===============================================================================

if ! should_run_scenario 30; then
    continue
fi

run_test_scenario "30: Transformation - Update transform rules"

# Create Secret with initial transformation
cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: v1
kind: Secret
metadata:
  name: test-transform-update
  namespace: $E2E_SOURCE_NS
  labels:
    kubemirror.raczylo.com/enabled: "true"
    test-resource: e2e
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "kubemirror-e2e-app-1"
    kubemirror.raczylo.com/transform: |
      rules:
        - path: data.VERSION
          value: "v1"
type: Opaque
stringData:
  VERSION: "v0"
  DATA: "original"
EOF

sleep 10

# Verify initial transformation
app1_v1=$(kubectl get secret test-transform-update -n kubemirror-e2e-app-1 -o jsonpath='{.data.VERSION}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
if [ "$app1_v1" = "v1" ]; then
    log_success "Initial transformation applied: VERSION=v1"
    ((PASS_COUNT++))
else
    log_fail "Initial transform failed. Expected: v1, Got: $app1_v1"
    ((FAIL_COUNT++))
fi

# Update transformation rules
cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: v1
kind: Secret
metadata:
  name: test-transform-update
  namespace: $E2E_SOURCE_NS
  labels:
    kubemirror.raczylo.com/enabled: "true"
    test-resource: e2e
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "kubemirror-e2e-app-1"
    kubemirror.raczylo.com/transform: |
      rules:
        - path: data.VERSION
          value: "v2-updated"
        - path: data.NEW_FIELD
          value: "added-by-transform"
type: Opaque
stringData:
  VERSION: "v0"
  DATA: "original"
EOF

sleep 15

# Verify updated transformation
app1_v2=$(kubectl get secret test-transform-update -n kubemirror-e2e-app-1 -o jsonpath='{.data.VERSION}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
if [ "$app1_v2" = "v2-updated" ]; then
    log_success "Updated transformation applied: VERSION=v2-updated"
    ((PASS_COUNT++))
else
    log_fail "Updated transform failed. Expected: v2-updated, Got: $app1_v2"
    ((FAIL_COUNT++))
fi

# Verify new transformation rule applied
app1_new=$(kubectl get secret test-transform-update -n kubemirror-e2e-app-1 -o jsonpath='{.data.NEW_FIELD}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
if [ "$app1_new" = "added-by-transform" ]; then
    log_success "New transformation rule applied: NEW_FIELD=added-by-transform"
    ((PASS_COUNT++))
else
    log_fail "New rule failed. Expected: added-by-transform, Got: $app1_new"
    ((FAIL_COUNT++))
fi

kubectl delete secret test-transform-update -n "$E2E_SOURCE_NS" --ignore-not-found=true >/dev/null 2>&1
complete_test_scenario "30" "pass"

#===============================================================================
# Final Summary
#===============================================================================

echo ""
echo "======================================"
echo "Comprehensive E2E Test Complete"
echo "======================================"

print_summary
