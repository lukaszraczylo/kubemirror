#!/bin/bash

# E2E Test Runner - Runs all KubeMirror E2E tests
# Builds the binary, starts the controller, runs tests, and cleans up

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

source "$SCRIPT_DIR/common.sh"

KUBEMIRROR_PID=""
KUBEMIRROR_LOG="/tmp/kubemirror-e2e-test.log"
KUBEMIRROR_BINARY="$PROJECT_ROOT/kubemirror"

# Cleanup function
cleanup() {
    log_info "Cleaning up..."

    if [ -n "$KUBEMIRROR_PID" ] && kill -0 "$KUBEMIRROR_PID" 2>/dev/null; then
        log_info "Stopping KubeMirror controller (PID: $KUBEMIRROR_PID)"
        kill "$KUBEMIRROR_PID" 2>/dev/null || true
        wait "$KUBEMIRROR_PID" 2>/dev/null || true
    fi

    # Clean up any test namespaces that might be left
    log_info "Cleaning up test namespaces"
    kubectl delete namespace -l kubemirror-e2e-test=true --wait=false 2>/dev/null || true

    # Give time for cleanup
    sleep 2
}

trap cleanup EXIT

# Main execution
main() {
    echo "======================================"
    echo "KubeMirror E2E Test Suite"
    echo "======================================"
    echo ""

    # Step 1: Check context
    log_info "Step 1: Checking Kubernetes context"
    check_context

    # Step 2: Build KubeMirror
    log_info "Step 2: Building KubeMirror binary"
    cd "$PROJECT_ROOT" || exit 1

    if ! go build -o kubemirror ./cmd/kubemirror; then
        log_fail "Failed to build KubeMirror"
        exit 1
    fi

    log_success "KubeMirror binary built successfully"

    # Step 3: Install Traefik CRDs (needed for scenario 23)
    log_info "Step 3: Installing Traefik CRDs"
    kubectl apply -f https://raw.githubusercontent.com/traefik/traefik/master/docs/content/reference/dynamic-configuration/kubernetes-crd-definition-v1.yml
    log_success "Traefik CRDs installed"

    # Step 4: Start KubeMirror controller
    log_info "Step 4: Starting KubeMirror controller"

    rm -f "$KUBEMIRROR_LOG"

    "$KUBEMIRROR_BINARY" \
        --metrics-bind-address=:8080 \
        --health-probe-bind-address=:8081 \
        --max-targets=100 \
        --worker-threads=5 \
        --verify-source-freshness=true \
        --lazy-watcher-init=true \
        --watcher-scan-interval=500ms \
        >"$KUBEMIRROR_LOG" 2>&1 &

    KUBEMIRROR_PID=$!

    log_info "KubeMirror started with PID: $KUBEMIRROR_PID"
    log_info "Log file: $KUBEMIRROR_LOG"

    # Wait for controller to be ready
    log_info "Waiting for controller to be ready..."
    sleep 10

    if ! kill -0 "$KUBEMIRROR_PID" 2>/dev/null; then
        log_fail "KubeMirror controller failed to start"
        log_info "Last 20 lines of log:"
        tail -20 "$KUBEMIRROR_LOG"
        exit 1
    fi

    # Check health endpoint
    local retries=0
    while [ $retries -lt 10 ]; do
        if curl -s http://localhost:8081/healthz >/dev/null 2>&1; then
            log_success "Controller is healthy"
            break
        fi
        sleep 2
        ((retries++))
    done

    if [ $retries -eq 10 ]; then
        log_warn "Controller health check timeout (non-fatal)"
    fi

    # Give controller time to set up watches
    sleep 5

    # Step 5: Run test suites
    log_info "Step 5: Running test suites"
    echo ""

    local test_results=0

    # Comprehensive Test Suite
    echo "======================================"
    echo "Running Comprehensive E2E Test Suite"
    echo "======================================"
    echo "This will test all scenarios systematically:"
    echo "  - Source lifecycle (no labels → labels → annotations)"
    echo "  - Target namespace changes (add/remove from list)"
    echo "  - Pattern matching and changes"
    echo "  - 'all' keyword with namespace opt-in/opt-out"
    echo "  - Content updates and propagation"
    echo "  - Orphaned mirror cleanup"
    echo "  - Namespace creation/deletion/label changes"
    echo ""

    if bash "$SCRIPT_DIR/test-comprehensive.sh"; then
        log_success "Comprehensive Test Suite PASSED"
    else
        log_fail "Comprehensive Test Suite FAILED"
        test_results=1
    fi
    echo ""

    # # Lazy Watcher Initialization Test
    # echo "======================================"
    # echo "Running Lazy Watcher Initialization Test"
    # echo "======================================"
    # echo "This will test:"
    # echo "  - Initial state with minimal controllers registered"
    # echo "  - Dynamic controller registration on resource creation"
    # echo "  - Memory efficiency of lazy initialization"
    # echo ""

    # if bash "$SCRIPT_DIR/test-lazy-watcher-init.sh"; then
    #     log_success "Lazy Watcher Test PASSED"
    # else
    #     log_fail "Lazy Watcher Test FAILED"
    #     test_results=1
    # fi
    # echo ""

    # Step 6: Final summary
    echo "======================================"
    echo "E2E Test Run Complete"
    echo "======================================"

    if [ $test_results -eq 0 ]; then
        echo -e "${GREEN}All test suites passed!${NC}"
        log_info "Controller log available at: $KUBEMIRROR_LOG"
        return 0
    else
        echo -e "${RED}Some test suites failed!${NC}"
        log_info "Controller log available at: $KUBEMIRROR_LOG"
        log_info "Last 10 lines of controller log:"
        tail -10 "$KUBEMIRROR_LOG"
        return 1
    fi
}

main "$@"
