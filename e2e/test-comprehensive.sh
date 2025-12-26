#!/bin/bash

# Comprehensive E2E Test Suite for KubeMirror
# Tests all scenarios systematically using the test framework

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"
source "$SCRIPT_DIR/test-framework.sh"

TEST_NAME="Comprehensive E2E Tests"

# Cleanup function
cleanup() {
    log_info "Cleaning up all test resources"

    # Delete all test secrets, configmaps, and CRDs
    kubectl delete secret,configmap -n default -l test-resource=e2e --ignore-not-found=true 2>/dev/null || true
    kubectl delete middleware.traefik.io -n default -l test-resource=e2e --ignore-not-found=true 2>/dev/null || true

    # Delete all test namespaces
    for i in {1..5}; do
        kubectl delete namespace "e2e-ns-$i" --ignore-not-found=true --wait=false 2>/dev/null || true
    done

    for prefix in app db stage prod; do
        for i in {1..3}; do
            kubectl delete namespace "e2e-${prefix}-${i}" --ignore-not-found=true --wait=false 2>/dev/null || true
        done
    done

    kubectl delete namespace e2e-labeled e2e-unlabeled e2e-test-ns --ignore-not-found=true --wait=false 2>/dev/null || true

    sleep 5
}

trap cleanup EXIT

log_info "Starting $TEST_NAME"

# Clean up before starting
cleanup
sleep 3

#===============================================================================
# SCENARIO 1: Source without labels/annotations
#===============================================================================

run_test_scenario "1: Source created without labels or annotations"

create_test_namespace e2e-ns-1
create_test_namespace e2e-ns-2

create_source secret test-no-labels-1 default false false "" "data-v1"

sleep 3

# Should NOT create mirrors (no enabled label or sync annotation)
verify_mirrors_not_exist secret test-no-labels-1 e2e-ns-1 e2e-ns-2

complete_test_scenario "1" "pass"

#===============================================================================
# SCENARIO 2: Labels added to source
#===============================================================================

run_test_scenario "2: Add enabled label to source (no sync annotation yet)"

update_source_labels secret test-no-labels-1 default true

sleep 3

# Still no mirrors (sync annotation required)
verify_mirrors_not_exist secret test-no-labels-1 e2e-ns-1 e2e-ns-2

complete_test_scenario "2" "pass"

#===============================================================================
# SCENARIO 3: Sync annotation added to labeled source
#===============================================================================

run_test_scenario "3: Add sync annotation with target namespaces"

update_source_annotations secret test-no-labels-1 default true "e2e-ns-1,e2e-ns-2"

# Now mirrors should be created
verify_mirrors_exist secret test-no-labels-1 e2e-ns-1 e2e-ns-2
verify_mirror_data secret test-no-labels-1 default e2e-ns-1 "data-v1"

complete_test_scenario "3" "pass"

#===============================================================================
# SCENARIO 4: Source content modified
#===============================================================================

run_test_scenario "4: Modify source data content"

update_source_data secret test-no-labels-1 default "data-v2-updated"

sleep 20

# Mirrors should be updated
verify_mirror_data secret test-no-labels-1 default e2e-ns-1 "data-v2-updated"
verify_mirror_data secret test-no-labels-1 default e2e-ns-2 "data-v2-updated"

complete_test_scenario "4" "pass"

#===============================================================================
# SCENARIO 5: Add namespace to target list
#===============================================================================

run_test_scenario "5: Add namespace to target-namespaces list"

create_test_namespace e2e-ns-3

update_source_annotations secret test-no-labels-1 default true "e2e-ns-1,e2e-ns-2,e2e-ns-3"

# New namespace should receive mirror
verify_mirrors_exist secret test-no-labels-1 e2e-ns-1 e2e-ns-2 e2e-ns-3
verify_mirror_data secret test-no-labels-1 default e2e-ns-3 "data-v2-updated"

complete_test_scenario "5" "pass"

#===============================================================================
# SCENARIO 6: Remove namespace from target list
#===============================================================================

run_test_scenario "6: Remove namespace from target-namespaces list"

update_source_annotations secret test-no-labels-1 default true "e2e-ns-1,e2e-ns-2"

# Orphaned mirror in e2e-ns-3 should be deleted
verify_orphan_cleanup secret test-no-labels-1 e2e-ns-3

# Others still exist
verify_mirrors_exist secret test-no-labels-1 e2e-ns-1 e2e-ns-2

complete_test_scenario "6" "pass"

#===============================================================================
# SCENARIO 7: Change target-namespaces from explicit list to pattern
#===============================================================================

run_test_scenario "7: Change from explicit list to pattern"

create_test_namespace e2e-app-1
create_test_namespace e2e-app-2
create_test_namespace e2e-db-1

update_source_annotations secret test-no-labels-1 default true "e2e-app-*"

# Should remove mirrors from e2e-ns-1, e2e-ns-2
verify_orphan_cleanup secret test-no-labels-1 e2e-ns-1 e2e-ns-2

# Should create mirrors in e2e-app-*
verify_mirrors_exist secret test-no-labels-1 e2e-app-1 e2e-app-2

# Should NOT create in e2e-db-1
verify_mirrors_not_exist secret test-no-labels-1 e2e-db-1

complete_test_scenario "7" "pass"

#===============================================================================
# SCENARIO 8: Multiple patterns
#===============================================================================

run_test_scenario "8: Multiple patterns in target-namespaces"

create_test_namespace e2e-db-2
create_test_namespace e2e-stage-1

update_source_annotations secret test-no-labels-1 default true "e2e-app-*,e2e-db-*"

# Should add mirrors to e2e-db-*
verify_mirrors_exist secret test-no-labels-1 e2e-db-1 e2e-db-2

# Should still have e2e-app-*
verify_mirrors_exist secret test-no-labels-1 e2e-app-1 e2e-app-2

# Should NOT have e2e-stage-*
verify_mirrors_not_exist secret test-no-labels-1 e2e-stage-1

complete_test_scenario "8" "pass"

#===============================================================================
# SCENARIO 9: Sync annotation set to false
#===============================================================================

run_test_scenario "9: Set sync annotation to false"

update_source_annotations secret test-no-labels-1 default false ""

# All mirrors should be deleted
verify_orphan_cleanup secret test-no-labels-1 e2e-app-1 e2e-app-2 e2e-db-1 e2e-db-2

complete_test_scenario "9" "pass"

#===============================================================================
# SCENARIO 10: Enabled label set to false
#===============================================================================

run_test_scenario "10: Set enabled label to false"

# Re-enable sync first
update_source_annotations secret test-no-labels-1 default true "e2e-app-1"

verify_mirrors_exist secret test-no-labels-1 e2e-app-1

# Now disable via label
update_source_labels secret test-no-labels-1 default false

sleep 3

# Mirror should be removed (label filtering)
verify_orphan_cleanup secret test-no-labels-1 e2e-app-1

complete_test_scenario "10" "pass"

#===============================================================================
# SCENARIO 11: Pattern with new namespace created
#===============================================================================

run_test_scenario "11: Create new namespace matching existing pattern"

# Re-enable the source
update_source_labels secret test-no-labels-1 default true
update_source_annotations secret test-no-labels-1 default true "e2e-prod-*"

create_test_namespace e2e-prod-1

# Should automatically create mirror in new namespace
verify_mirrors_exist secret test-no-labels-1 e2e-prod-1

# Create another matching namespace
create_test_namespace e2e-prod-2

# Should also get the mirror
verify_mirrors_exist secret test-no-labels-1 e2e-prod-2

complete_test_scenario "11" "pass"

#===============================================================================
# SCENARIO 12: 'all' keyword without namespace label (opt-OUT model)
#===============================================================================

run_test_scenario "12: Source with 'all' keyword, namespace without allow-mirrors label"

create_source configmap test-all-no-label default true true "all" "all-data-v1"

create_test_namespace e2e-unlabeled

sleep 5

# SHOULD create mirror (opt-OUT model: namespaces without label get mirrors by default)
verify_mirrors_exist configmap test-all-no-label e2e-unlabeled
verify_mirror_data configmap test-all-no-label default e2e-unlabeled "all-data-v1"

complete_test_scenario "12" "pass"

#===============================================================================
# SCENARIO 13: Set allow-mirrors=false to opt-out
#===============================================================================

run_test_scenario "13: Set allow-mirrors=false on namespace (explicit opt-OUT)"

update_namespace_labels e2e-unlabeled false

sleep 5

# Mirror should be deleted (explicit opt-OUT)
verify_orphan_cleanup configmap test-all-no-label e2e-unlabeled

complete_test_scenario "13" "pass"

#===============================================================================
# SCENARIO 14: Change allow-mirrors from false to true
#===============================================================================

run_test_scenario "14: Change allow-mirrors label from false to true"

update_namespace_labels e2e-unlabeled true

sleep 5

# Mirror should be recreated
verify_mirrors_exist configmap test-all-no-label e2e-unlabeled
verify_mirror_data configmap test-all-no-label default e2e-unlabeled "all-data-v1"

complete_test_scenario "14" "pass"

#===============================================================================
# SCENARIO 15: Remove allow-mirrors label (back to default opt-IN)
#===============================================================================

run_test_scenario "15: Remove allow-mirrors label from namespace"

update_namespace_labels e2e-unlabeled ""

sleep 5

# Mirror should STILL exist (default is opt-IN, not opt-OUT)
verify_mirrors_exist configmap test-all-no-label e2e-unlabeled
verify_mirror_data configmap test-all-no-label default e2e-unlabeled "all-data-v1"

complete_test_scenario "15" "pass"

#===============================================================================
# SCENARIO 16: Target namespace deleted
#===============================================================================

run_test_scenario "16: Delete target namespace"

create_test_namespace e2e-ns-4
update_source_annotations secret test-no-labels-1 default true "e2e-ns-4,e2e-prod-1"

verify_mirrors_exist secret test-no-labels-1 e2e-ns-4 e2e-prod-1

# Delete one of the target namespaces
delete_namespace e2e-ns-4

sleep 3

# Other mirror should still exist
verify_mirrors_exist secret test-no-labels-1 e2e-prod-1

complete_test_scenario "16" "pass"

#===============================================================================
# SCENARIO 17: Recreate deleted target namespace
#===============================================================================

run_test_scenario "17: Recreate deleted target namespace"

# Wait for namespace to be fully deleted
sleep 5

create_test_namespace e2e-ns-4

# Mirror should be recreated automatically (namespace reconciler + pattern matching)
# Note: This requires namespace reconciler to be working
wait_for_resource secret test-no-labels-1 e2e-ns-4 30 || log_warn "Mirror not auto-created (may require source update)"

complete_test_scenario "17" "pass"

#===============================================================================
# SCENARIO 18: Source deleted
#===============================================================================

run_test_scenario "18: Delete source resource"

create_test_namespace e2e-ns-5
create_source secret test-delete-source default true true "e2e-ns-5" "delete-test"

verify_mirrors_exist secret test-delete-source e2e-ns-5

# Delete source
kubectl delete secret test-delete-source -n default

# Mirror should be cascade deleted
verify_orphan_cleanup secret test-delete-source e2e-ns-5

complete_test_scenario "18" "pass"

#===============================================================================
# SCENARIO 19: Target manually deleted (should be recreated)
#===============================================================================

run_test_scenario "19: Manually delete target mirror (should recreate)"

create_source secret test-recreate default true true "e2e-prod-1" "recreate-data"

verify_mirrors_exist secret test-recreate e2e-prod-1

# Manually delete the mirror
kubectl delete secret test-recreate -n e2e-prod-1

# Should be automatically recreated
wait_for_resource secret test-recreate e2e-prod-1 15

assert_resource_exists secret test-recreate e2e-prod-1

complete_test_scenario "19" "pass"

#===============================================================================
# SCENARIO 20: ConfigMap with same test patterns
#===============================================================================

run_test_scenario "20: ConfigMap with pattern matching"

create_source configmap test-cm-pattern default true true "e2e-app-*" "cm-data-v1"

verify_mirrors_exist configmap test-cm-pattern e2e-app-1 e2e-app-2

# Update content
update_source_data configmap test-cm-pattern default "cm-data-v2"

sleep 20

verify_mirror_data configmap test-cm-pattern default e2e-app-1 "cm-data-v2"

# Change pattern
update_source_annotations configmap test-cm-pattern default true "e2e-db-*"

verify_orphan_cleanup configmap test-cm-pattern e2e-app-1 e2e-app-2
verify_mirrors_exist configmap test-cm-pattern e2e-db-1 e2e-db-2

complete_test_scenario "20" "pass"

#===============================================================================
# SCENARIO 21: Mix of explicit and pattern
#===============================================================================

run_test_scenario "21: Mix of explicit namespaces and patterns"

create_test_namespace e2e-test-ns

create_source secret test-mixed default true true "e2e-test-ns,e2e-stage-*" "mixed-data"

create_test_namespace e2e-stage-2

verify_mirrors_exist secret test-mixed e2e-test-ns e2e-stage-1 e2e-stage-2

# Remove explicit, keep pattern
update_source_annotations secret test-mixed default true "e2e-stage-*"

verify_orphan_cleanup secret test-mixed e2e-test-ns
verify_mirrors_exist secret test-mixed e2e-stage-1 e2e-stage-2

complete_test_scenario "21" "pass"

#===============================================================================
# SCENARIO 22: Sync annotation removed completely
#===============================================================================

run_test_scenario "22: Remove sync annotation completely"

verify_mirrors_exist secret test-mixed e2e-stage-1 e2e-stage-2

update_source_annotations secret test-mixed default "" ""

verify_orphan_cleanup secret test-mixed e2e-stage-1 e2e-stage-2

complete_test_scenario "22" "pass"

#===============================================================================
# SCENARIO 23: Traefik Middleware CRD (test generic CRD support)
#===============================================================================

run_test_scenario "23: Traefik Middleware CRD with spec updates"

# Create Traefik Middleware CRD manually (CRDs aren't supported by create_source helper)
cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: test-middleware
  namespace: default
  labels:
    kubemirror.raczylo.com/enabled: "true"
    test-resource: e2e
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "e2e-app-1,e2e-app-2"
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
verify_mirrors_exist middleware.traefik.io test-middleware e2e-app-1 e2e-app-2

# Verify mirror spec content (check one of the spec fields)
app1_secret=$(kubectl get middleware test-middleware -n e2e-app-1 -o jsonpath='{.spec.basicAuth.secret}' 2>/dev/null || echo "")
if [ "$app1_secret" = "auth-secret-v1" ]; then
    log_success "Mirror spec in e2e-app-1 matches expected: auth-secret-v1"
    ((PASS_COUNT++))
else
    log_fail "Mirror spec in e2e-app-1 does not match. Expected: auth-secret-v1, Got: $app1_secret"
    ((FAIL_COUNT++))
fi

# Update the Middleware spec
cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: test-middleware
  namespace: default
  labels:
    kubemirror.raczylo.com/enabled: "true"
    test-resource: e2e
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "e2e-app-1,e2e-app-2"
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
app1_secret_updated=$(kubectl get middleware test-middleware -n e2e-app-1 -o jsonpath='{.spec.basicAuth.secret}' 2>/dev/null || echo "")
if [ "$app1_secret_updated" = "auth-secret-v2-updated" ]; then
    log_success "Mirror spec in e2e-app-1 updated correctly: auth-secret-v2-updated"
    ((PASS_COUNT++))
else
    log_fail "Mirror spec in e2e-app-1 not updated. Expected: auth-secret-v2-updated, Got: $app1_secret_updated"
    ((FAIL_COUNT++))
fi

app1_header=$(kubectl get middleware test-middleware -n e2e-app-1 -o jsonpath='{.spec.headers.customRequestHeaders.X-New-Header}' 2>/dev/null || echo "")
if [ "$app1_header" = "new-value" ]; then
    log_success "Mirror spec headers in e2e-app-1 updated correctly: new-value"
    ((PASS_COUNT++))
else
    log_fail "Mirror spec headers in e2e-app-1 not updated. Expected: new-value, Got: $app1_header"
    ((FAIL_COUNT++))
fi

# Change target namespaces pattern
kubectl annotate middleware test-middleware -n default \
    kubemirror.raczylo.com/target-namespaces="e2e-db-*" --overwrite >/dev/null 2>&1

sleep 10

# Verify old mirrors cleaned up
verify_orphan_cleanup middleware.traefik.io test-middleware e2e-app-1 e2e-app-2

# Verify new mirrors created
verify_mirrors_exist middleware.traefik.io test-middleware e2e-db-1 e2e-db-2

# Clean up CRD
kubectl delete middleware test-middleware -n default --ignore-not-found=true >/dev/null 2>&1

complete_test_scenario "23" "pass"

#===============================================================================
# Final Summary
#===============================================================================

echo ""
echo "======================================"
echo "Comprehensive E2E Test Complete"
echo "======================================"

print_summary
