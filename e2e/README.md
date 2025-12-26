# KubeMirror E2E Tests

Comprehensive, DRY (Don't Repeat Yourself) test framework for KubeMirror functionality.

## Overview

The test suite uses a **data-driven framework** approach where test scenarios are systematically defined and executed using reusable functions. This ensures comprehensive coverage of all edge cases without code duplication.

## Prerequisites

- Kubernetes cluster running (tested with docker-desktop)
- kubectl configured and pointing to docker-desktop context
- Go 1.21+ installed
- curl (for health checks)

## Test Architecture

### Test Framework Components

1. **common.sh**: Base utilities (logging, assertions, cleanup)
2. **test-framework.sh**: DRY test framework functions (resource creation, updates, verification)
3. **test-comprehensive.sh**: Comprehensive test scenarios using the framework
4. **run-all-tests.sh**: Main test runner (builds binary, starts controller, runs tests)

### Test Framework Functions

The framework provides reusable functions for all operations:

```bash
# Resource lifecycle
create_source <type> <name> <namespace> <has_label> <has_annotation> <targets> <data>
update_source_labels <type> <name> <namespace> <enabled_value>
update_source_annotations <type> <name> <namespace> <sync_value> <targets>
update_source_data <type> <name> <namespace> <new_data>

# Namespace operations
create_test_namespace <name> <allow_mirrors_label>
update_namespace_labels <namespace> <allow_mirrors_value>
delete_namespace <namespace>

# Verification functions
verify_mirrors_exist <type> <name> <namespace1> <namespace2> ...
verify_mirrors_not_exist <type> <name> <namespace1> <namespace2> ...
verify_mirror_data <type> <source_name> <source_ns> <target_ns> <expected_data>
verify_orphan_cleanup <type> <name> <namespace1> <namespace2> ...
```

## Comprehensive Test Suite

The comprehensive test suite (`test-comprehensive.sh`) covers **22 systematic scenarios**:

### Source Lifecycle Scenarios

1. **Source without labels/annotations**: No mirrors created
2. **Add enabled label**: Still no mirrors (sync annotation required)
3. **Add sync annotation**: Mirrors created in targets
4. **Modify source data**: Mirrors updated
5. **Set sync to false**: All mirrors deleted
6. **Set enabled to false**: All mirrors deleted

### Target Namespace Management

7. **Add namespace to list**: New mirror created
8. **Remove namespace from list**: Orphaned mirror deleted
9. **Change list to pattern**: Old mirrors deleted, new pattern mirrors created
10. **Multiple patterns**: Mirrors in all matching namespaces

### Pattern Matching

11. **Create namespace matching pattern**: Automatic mirror creation
12. **Mix explicit + pattern**: Both types work together
13. **Change pattern**: Orphaned mirrors cleaned up

### 'all' Keyword with Opt-in

14. **'all' without namespace label**: No mirror created
15. **Add allow-mirrors label**: Mirror created
16. **Remove allow-mirrors label**: Mirror deleted
17. **Change label true→false**: Mirror deleted

### Edge Cases

18. **Target namespace deleted**: Other mirrors unaffected
19. **Recreate deleted namespace**: Mirror recreated
20. **Source deleted**: Cascade deletion of all mirrors
21. **Target manually deleted**: Automatic recreation
22. **Remove sync annotation**: All mirrors deleted

### Resource Types

All scenarios tested with both **Secrets** and **ConfigMaps**.

## Running Tests

### Run Complete Test Suite

```bash
cd e2e
./run-all-tests.sh
```

This will:
1. Check you're on docker-desktop context
2. Build the KubeMirror binary
3. Start the controller in background
4. Run comprehensive test scenarios (22+ scenarios)
5. Report detailed results with pass/fail for each
6. Clean up all resources automatically

### Run Individual Test Scenarios

```bash
# Must have KubeMirror controller running first
cd /Users/nvm/Documents/projects/private/kube-mirror
./kubemirror --max-targets=100 --worker-threads=5 > /tmp/kubemirror-test.log 2>&1 &

# Then run the test
cd e2e
./test-comprehensive.sh
```

## Test Output

Each test produces colored output:
- 🔵 **[INFO]**: Informational messages
- ✅ **[PASS]**: Test passed
- ❌ **[FAIL]**: Test failed
- ⚠️  **[WARN]**: Warning messages

Example output:
```
======================================
KubeMirror E2E Test Suite
======================================

[INFO] Step 1: Checking Kubernetes context
[PASS] Running on docker-desktop context
[INFO] Step 2: Building KubeMirror binary
[PASS] KubeMirror binary built successfully
[INFO] Step 3: Starting KubeMirror controller
[INFO] KubeMirror started with PID: 12345
[PASS] Controller is healthy

======================================
Running Test Suite 1: Basic Mirroring
======================================
[INFO] Starting Basic Mirroring tests
[INFO] Test 1: Mirror Secret to explicit namespace list
[PASS] Resource secret/test-explicit-list-secret exists in namespace e2e-target-1
[PASS] Resource secret/test-explicit-list-secret exists in namespace e2e-target-2
...

======================================
Test Summary
======================================
Total Tests: 45
Passed:      45
Failed:      0
======================================
All tests passed!
```

## Test Resources

Tests create temporary resources:
- **Namespaces**: `e2e-*` prefixed
- **Secrets**: `test-*` prefixed in default namespace
- **ConfigMaps**: `test-*` prefixed in default namespace

All resources are cleaned up automatically on test completion.

## Troubleshooting

### Tests fail with "context not docker-desktop"

Switch to docker-desktop context:
```bash
kubectl config use-context docker-desktop
```

### Tests timeout waiting for resources

Controller may not be running or not reconciling. Check:
```bash
# Check if controller is running
ps aux | grep kubemirror

# Check controller logs
tail -f /tmp/kubemirror-e2e-test.log

# Check controller health
curl http://localhost:8081/healthz
```

### Cleanup hanging

If tests get interrupted, manually clean up:
```bash
# Delete all e2e test namespaces
kubectl delete namespace -l kubemirror-e2e-test=true

# Delete test resources in default namespace
kubectl delete secret,configmap -n default -l kubemirror.raczylo.com/enabled=true

# Kill controller if still running
pkill kubemirror
```

### Individual test fails

Run test with verbose output to see which assertion failed:
```bash
bash -x ./test-basic-mirroring.sh
```

Check controller logs for errors:
```bash
grep -i error /tmp/kubemirror-e2e-test.log
```

## Adding New Test Scenarios

The DRY framework makes it easy to add new test scenarios. Here's how:

### Example: Add a new scenario

```bash
# In test-comprehensive.sh, add a new scenario block:

run_test_scenario "23: Your new scenario description"

# Use framework functions to set up test conditions
create_test_namespace e2e-new-ns
create_source secret test-new default true true "e2e-new-ns" "test-data"

# Perform the action you want to test
update_source_annotations secret test-new default true "e2e-new-ns,e2e-new-ns-2"

# Verify expected results
verify_mirrors_exist secret test-new e2e-new-ns e2e-new-ns-2

complete_test_scenario "23" "pass"
```

### Framework Functions Reference

**Resource Creation:**
```bash
create_source secret my-secret default true true "ns1,ns2" "data"
#             ↑      ↑         ↑       ↑    ↑    ↑         ↑
#             type   name      ns      lbl  ann  targets   data
```

**Resource Updates:**
```bash
update_source_labels secret my-secret default true     # Set enabled=true
update_source_labels secret my-secret default false    # Set enabled=false
update_source_labels secret my-secret default ""       # Remove label

update_source_annotations secret my-secret default true "ns1,ns2"  # Enable sync
update_source_annotations secret my-secret default false ""        # Set sync=false
update_source_annotations secret my-secret default "" ""           # Remove annotation

update_source_data secret my-secret default "new-data-v2"
```

**Namespace Operations:**
```bash
create_test_namespace my-ns true     # Create with allow-mirrors=true
create_test_namespace my-ns false    # Create with allow-mirrors=false
create_test_namespace my-ns ""       # Create with no label

update_namespace_labels my-ns true   # Set allow-mirrors=true
update_namespace_labels my-ns false  # Set allow-mirrors=false
update_namespace_labels my-ns ""     # Remove label
```

**Verification:**
```bash
verify_mirrors_exist secret my-secret ns1 ns2 ns3
verify_mirrors_not_exist secret my-secret ns4 ns5
verify_mirror_data secret my-secret default target-ns "expected-data"
verify_orphan_cleanup secret my-secret orphan-ns1 orphan-ns2
```

## Test Coverage Summary

| Category | Scenarios | Details |
|----------|-----------|---------|
| Source lifecycle | 6 | No labels → add label → add annotation → modify → disable |
| Target management | 4 | Add/remove namespaces, change list to pattern, multiple patterns |
| Pattern matching | 3 | New namespace creation, pattern changes, mixed explicit+pattern |
| 'all' keyword opt-in | 4 | No label, add label, remove label, change true→false |
| Edge cases | 5 | Namespace deletion, recreation, source deletion, target recreation |
| **Total** | **22** | **All with Secrets and ConfigMaps** |

## Test Methodology

### Systematic Approach

The test framework follows a systematic approach:

1. **State Setup**: Create namespaces and resources in known state
2. **Action**: Perform the operation being tested (create, update, delete, label change)
3. **Verification**: Assert expected outcomes using verification functions
4. **Cleanup**: Automatic cleanup via trap handlers

### DRY Principles

- **Reusable functions**: All operations abstracted into framework functions
- **Data-driven**: Test scenarios are data, not code
- **Composable**: Combine framework functions to create complex scenarios
- **Maintainable**: Add new scenarios without duplicating code

### Coverage Strategy

Tests systematically cover:
- **Happy path**: Expected behavior under normal conditions
- **Edge cases**: Boundary conditions and unusual states
- **Error conditions**: Invalid inputs, missing resources, conflicts
- **State transitions**: All possible state changes (no labels → labels → annotations, etc.)
- **Concurrent operations**: Namespace creation during reconciliation, multiple updates

## Test Utilities Reference

### Common Utilities (`common.sh`)

**Logging:**
- `log_info <message>`: Blue informational message
- `log_success <message>`: Green success message (increments pass count)
- `log_fail <message>`: Red failure message (increments fail count)
- `log_warn <message>`: Yellow warning message

**Assertions:**
- `assert_resource_exists <type> <name> <namespace>`
- `assert_resource_not_exists <type> <name> <namespace>`
- `assert_annotation_exists <type> <name> <namespace> <annotation_key>`
- `assert_label_exists <type> <name> <namespace> <label_key> <expected_value>`
- `assert_data_matches <type> <source_name> <source_ns> <target_name> <target_ns> <data_key>`

**Waiting:**
- `wait_for_resource <type> <name> <namespace> [timeout]`
- `wait_for_resource_deletion <type> <name> <namespace> [timeout]`

**Utilities:**
- `cleanup_namespace <namespace>`
- `cleanup_resource <type> <name> <namespace>`
- `check_context`: Verify running on docker-desktop
- `print_summary`: Print test results summary

## CI/CD Integration

To run tests in CI:

```bash
#!/bin/bash
set -e

# Start kind cluster or use existing k8s
kind create cluster --name kubemirror-test

# Switch context
kubectl config use-context kind-kubemirror-test

# Run tests
cd e2e
./run-all-tests.sh

# Cleanup
kind delete cluster --name kubemirror-test
```

## Performance Notes

- Comprehensive test suite: ~3-5 minutes
- Controller startup: ~10 seconds
- Resource reconciliation: typically <5 seconds per operation
- Total assertions: 60+ across all scenarios
- Each scenario includes setup, action, verification, and cleanup phases

## Test Isolation and Cleanup

- **Automatic cleanup**: All resources cleaned up via trap handlers
- **Namespace isolation**: Tests use `e2e-*` prefixed namespaces
- **Sequential execution**: Tests run sequentially to avoid race conditions
- **Idempotent**: Tests can be re-run without manual cleanup
- **Resource labeling**: Test resources labeled for easy identification

## Known Limitations

- Tests assume clean docker-desktop cluster (or equivalent local cluster)
- Some scenarios require waiting for reconciliation (30s default timeout)
- Tests are sequential (not parallel) to ensure deterministic behavior
- Controller must be stopped between runs if running manually (run-all-tests.sh handles this)
