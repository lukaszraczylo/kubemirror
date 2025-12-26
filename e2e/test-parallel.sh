#!/bin/bash

# Parallel E2E Test Runner for KubeMirror
# Runs independent tests in parallel batches for faster execution
#
# Usage:
#   ./test-parallel.sh              # Run all tests in parallel batches
#
# Performance:
#   - Sequential execution: ~5-7 minutes for all 30 scenarios
#   - Parallel execution: ~3-4 minutes (batched by independence)
#
# Batching Strategy:
#   - Sequential: Scenarios 1-11 (core lifecycle - must run sequentially)
#   - Parallel Batch 1: Scenarios 12-15 (namespace labels)
#   - Parallel Batch 2: Scenarios 16-19 (deletion scenarios)
#   - Parallel Batch 3: Scenarios 20-23 (mixed resources)
#   - Parallel Batch 4: Scenarios 24-27 (transformations part 1)
#   - Parallel Batch 5: Scenarios 28-30 (transformations part 2)
#
# Individual scenario logs: e2e/.test-scenario-{num}.log

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

TEST_NAME="Parallel E2E Tests"

# Colors
CYAN='\033[0;36m'
NC='\033[0m'

# Batch execution tracking
BATCH_RESULTS=()
TOTAL_START_TIME=$(date +%s)

# Run a single scenario by number
run_scenario() {
    local scenario_num=$1
    local log_file="$SCRIPT_DIR/.test-scenario-${scenario_num}.log"

    echo -e "${CYAN}[BATCH]${NC} Starting scenario $scenario_num in background"

    # Run the specific scenario using the comprehensive test script
    # Pass scenario number as argument to run only that scenario
    bash "$SCRIPT_DIR/test-comprehensive.sh" "$scenario_num" > "$log_file" 2>&1
    local exit_code=$?

    # Store result
    echo "$scenario_num:$exit_code" >> "$SCRIPT_DIR/.batch-results.tmp"

    if [ $exit_code -eq 0 ]; then
        echo -e "${GREEN}[PASS]${NC} Scenario $scenario_num completed successfully"
    else
        echo -e "${RED}[FAIL]${NC} Scenario $scenario_num failed (exit code: $exit_code)"
    fi

    return $exit_code
}

# Run multiple scenarios in parallel
run_parallel_batch() {
    local batch_name=$1
    shift
    local scenarios=("$@")

    echo ""
    echo -e "${CYAN}======================================${NC}"
    echo -e "${CYAN}Batch: $batch_name${NC}"
    echo -e "${CYAN}Scenarios: ${scenarios[*]}${NC}"
    echo -e "${CYAN}======================================${NC}"

    local batch_start=$(date +%s)

    # Clear batch results file
    > "$SCRIPT_DIR/.batch-results.tmp"

    # Start all scenarios in background
    local pids=()
    for scenario in "${scenarios[@]}"; do
        run_scenario "$scenario" &
        pids+=($!)
    done

    # Wait for all to complete
    local batch_failed=0
    for pid in "${pids[@]}"; do
        wait $pid || batch_failed=1
    done

    local batch_end=$(date +%s)
    local batch_duration=$((batch_end - batch_start))

    echo -e "${CYAN}Batch '$batch_name' completed in ${batch_duration}s${NC}"

    return $batch_failed
}

# Main execution
main() {
    log_info "Starting $TEST_NAME"
    log_info "This runner executes independent tests in parallel for faster results"

    # Ensure controller is running
    if ! pgrep -f "kubemirror" > /dev/null; then
        log_fail "KubeMirror controller is not running!"
        log_info "Please start the controller first: ./e2e/run-all-tests.sh"
        exit 1
    fi

    # Clean up any previous test artifacts
    log_info "Cleaning up previous test artifacts"
    rm -f "$SCRIPT_DIR/.test-scenario-"*.log
    rm -f "$SCRIPT_DIR/.batch-results.tmp"

    local overall_failed=0

    # ========================================================================
    # SEQUENTIAL BATCH: Core Lifecycle (Scenarios 1-11)
    # These tests modify the same resource and MUST run sequentially
    # ========================================================================
    echo ""
    echo -e "${CYAN}======================================${NC}"
    echo -e "${CYAN}Sequential Batch: Core Lifecycle${NC}"
    echo -e "${CYAN}Scenarios: 1-11 (sequential execution required)${NC}"
    echo -e "${CYAN}======================================${NC}"

    local seq_start=$(date +%s)
    bash "$SCRIPT_DIR/test-comprehensive.sh" 1 2 3 4 5 6 7 8 9 10 11
    if [ $? -ne 0 ]; then
        overall_failed=1
        log_fail "Sequential batch (1-11) failed"
    fi
    local seq_end=$(date +%s)
    echo -e "${CYAN}Sequential batch completed in $((seq_end - seq_start))s${NC}"

    # ========================================================================
    # PARALLEL BATCH 1: Namespace Label Tests (Scenarios 12-15)
    # ========================================================================
    run_parallel_batch "Namespace Labels" 12 13 14 15
    [ $? -ne 0 ] && overall_failed=1

    # ========================================================================
    # PARALLEL BATCH 2: Deletion Scenarios (Scenarios 16-19)
    # ========================================================================
    run_parallel_batch "Deletion Scenarios" 16 17 18 19
    [ $? -ne 0 ] && overall_failed=1

    # ========================================================================
    # PARALLEL BATCH 3: Mixed Resources (Scenarios 20-23)
    # ========================================================================
    run_parallel_batch "Mixed Resources" 20 21 22 23
    [ $? -ne 0 ] && overall_failed=1

    # ========================================================================
    # PARALLEL BATCH 4: Transformations Part 1 (Scenarios 24-27)
    # ========================================================================
    run_parallel_batch "Transformations 1" 24 25 26 27
    [ $? -ne 0 ] && overall_failed=1

    # ========================================================================
    # PARALLEL BATCH 5: Transformations Part 2 (Scenarios 28-30)
    # ========================================================================
    run_parallel_batch "Transformations 2" 28 29 30
    [ $? -ne 0 ] && overall_failed=1

    # ========================================================================
    # SUMMARY
    # ========================================================================
    local total_end=$(date +%s)
    local total_duration=$((total_end - TOTAL_START_TIME))

    echo ""
    echo -e "${CYAN}======================================${NC}"
    echo -e "${CYAN}Test Execution Summary${NC}"
    echo -e "${CYAN}======================================${NC}"
    echo -e "Total execution time: ${total_duration}s"
    echo -e "Sequential scenarios: 1-11"
    echo -e "Parallel batches: 5 batches (scenarios 12-30)"

    if [ $overall_failed -eq 0 ]; then
        echo -e "${GREEN}All test batches PASSED!${NC}"

        # Show individual scenario results from logs
        echo ""
        echo "Individual scenario results:"
        for i in {1..30}; do
            if [ -f "$SCRIPT_DIR/.test-scenario-${i}.log" ]; then
                if grep -q "PASS.*Scenario.*${i}" "$SCRIPT_DIR/.test-scenario-${i}.log" 2>/dev/null; then
                    echo -e "  Scenario $i: ${GREEN}PASS${NC}"
                else
                    echo -e "  Scenario $i: ${RED}FAIL${NC}"
                fi
            fi
        done
    else
        echo -e "${RED}Some test batches FAILED!${NC}"
        echo ""
        echo "Check individual scenario logs in: $SCRIPT_DIR/.test-scenario-*.log"
    fi

    # Cleanup temp files
    rm -f "$SCRIPT_DIR/.batch-results.tmp"

    exit $overall_failed
}

main "$@"
