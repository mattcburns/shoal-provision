# Test Expansion - Phase 6 Milestone 8

Status: In Progress
Date: 2025-01-XX (Updated)
Reference: design/035_Test_Strategy.md, plans/004_Phase_6_Provisioner_Plan.md

## Summary

This document tracks the test expansion work for Phase 6 Milestone 8, implementing the comprehensive test strategy defined in design/035_Test_Strategy.md.

## Coverage Targets

Per design 035:
- **Global target**: ≥ 80% line coverage
- **Critical packages**: ≥ 90% coverage
  - Controller API
  - Jobs/state machine
  - ISO builder
  - Redfish client adapters
  - Signed URL validator
  - Webhook handler

## Current Status (2025-01-XX - Latest Update)

### Coverage Progress

**Before Test Expansion (Baseline):**
- Global coverage: 57.3%
- internal/provisioner/api: 55.1% ❌
- internal/provisioner/jobs: 67.8% ❌
- internal/provisioner/iso: 80.2% ⚠️ (meets 80% but critical → needs 90%)
- internal/provisioner/redfish: 61.3% ❌
- internal/provisioner/store: 60.8% ❌
- pkg/crypto: 90.1% ✅

**After Current Expansion (Latest):**
- Global coverage: **60.2%** (+2.9pp from 57.3%) 🔄
- internal/provisioner/api: **81.7%** (+26.6pp) ✅ Exceeds 80%
- internal/provisioner/jobs: **74.1%** (+6.3pp) 🔄 Good progress
- internal/provisioner/iso: **84.2%** (+4.0pp) 🔄 Approaching 90%
- internal/provisioner/redfish: **61.3%** (0.0pp) ⚠️ Interface coverage limitation
- internal/provisioner/store: **78.7%** (+17.9pp) ✅ Near 80% target
- pkg/crypto: 90.1% ✅

### Completed Work

#### 1. API Authentication Tests (auth_test.go - PR #77 MERGED)

**Coverage Improvement**: 55.1% → 81.7% (+26.6 percentage points)

Comprehensive test suite added covering:

- **Context Management**
  - WithPrincipal and PrincipalFromContext
  - Principal attachment and retrieval

- **Authentication Modes**
  - Mode: "none" (no auth)
  - Mode: "basic" (HTTP Basic auth)
    - Valid credentials
    - Invalid password
    - Missing Authorization header
    - Wrong scheme
  - Mode: "jwt" (HS256 JWT tokens)
    - Valid token with all claims
    - Expired token (exp claim)
    - Not yet valid (nbf claim)
    - Issuer mismatch
    - Audience mismatch (string and array)
    - Invalid signature
    - Missing sub claim
    - Wrong scheme
    - Missing Authorization header
  - Mode: "unknown" (unsupported mode rejection)

- **Basic Auth Parsing**
  - Valid formats
  - Colon in password
  - Empty header
  - Wrong scheme
  - Invalid base64
  - Missing colon separator

- **JWT Validation**
  - Header/payload/signature splitting
  - Algorithm validation (only HS256 allowed)
  - Invalid JSON handling
  - Claim type checking
  - Signature verification

- **Security Functions**
  - Constant-time string comparison (secureEqual)
  - Token redaction for logging
  - WWW-Authenticate header generation

**Tests Added**: 14 test functions with 30+ test cases covering all authentication code paths.

**Security Coverage**: All authentication and authorization code paths now tested, ensuring:
- Credentials never appear in logs (redaction)
- Constant-time comparisons prevent timing attacks
- JWT signature validation prevents token forgery
- Claim validation prevents expired/invalid tokens

#### 2. Store Package Tests (store_test.go - Commit c0230db)

**Coverage Improvement**: 60.8% → 78.7% (+17.9 percentage points)

Expanded database operation tests covering:
- `TestSettingsSetAndGet`: Key-value settings storage and retrieval
- `TestUpdateJobTaskISOPath`: ISO path updates for job tasks
- `TestExtendLease`: Lease extension behavior and validation
- `TestStealExpiredLease`: Expired lease recovery logic
- `TestAppendAndListJobEvents`: Event logging and retrieval

**Tests Added**: 5 test functions (+299 lines)

**Achievement**: Package now near 80% target for non-critical package ✅

#### 3. ISO Builder Tests (builder_test.go - Commit d0a56df)

**Coverage Improvement**: 80.2% → 84.2% (+4.0 percentage points)

Comprehensive ISO builder tests covering:
- `TestFileBuilder_ErrorPaths`: Validation and error handling
- `TestFileBuilder_MinimalAssets`: Basic functionality with minimal inputs
- `TestFileBuilder_AllAssets`: Full feature coverage with all assets
- `TestFileBuilder_DeterminismWithSourceDateEpoch`: Reproducibility validation
- `TestFileBuilder_ConcurrentBuilds`: Thread safety verification

**Tests Added**: 5 test functions (+240 lines)

**Key Achievement**: SOURCE_DATE_EPOCH determinism verified ✅ - Critical for reproducible builds

#### 4. Jobs/Worker Tests (worker_test.go - Commit 7a46484)

**Coverage Improvement**: 67.8% → 74.1% (+6.3 percentage points)

Expanded worker loop and helper tests:
- `TestWorker_RunLoop`: Main worker loop job acquisition and processing
- `TestWorker_RunLoopCancellation`: Context cancellation handling
- `TestWorker_ProcessJobMissingServer`: Server resolution error path
- `TestWorker_HelperFunctions`: Utility functions (displayDuration, truncate, minDur, strPtr)
- `TestWorker_ComposeTaskMediaURL`: URL composition (valid/invalid bases)
- `TestWorker_LoggingCoverage`: logf heartbeat suppression logic

**Tests Added**: 6 test functions (+252 lines)

**Key Achievement**: Run function coverage 0% → 93.3% ✅ - Main worker loop now tested

### Remaining Work

#### Priority 1: Reach 80% Global Coverage (~20pp gap from current 60.2%)
Target high-value packages with low coverage:
- **internal/web**: 44.2% (large package, significant impact)
- **internal/database**: 57.6% (database operations for Shoal aggregator)
- **internal/api**: 54.7% (Shoal aggregator API)
- **internal/provisioner/dispatcher**: 59.5% (dispatcher coordination)

#### Priority 2: Complete Critical Package Coverage (90% target)

##### Jobs/Worker (74.1% → 90%, 15.9pp gap)
- **Target**: 67.8% → 90%
- **Focus Areas**:
  - processJob error paths and branch coverage
  - ESXi-specific functions (pollESXiPowerState 60.9%, runESXiCompletion 76.7%)
  - Recipe parsing helpers (taskTargetFromRecipe 66.7%, kickstartFromRecipe 77.8%)
  - awaitBMCReady edge cases (58.3%)

##### ISO Builder (84.2% → 90%, 5.8pp gap)
- **Target**: 80.2% → 90%
- **Focus Areas**:
  - Edge cases in writeIfNonEmpty, writeAtomic
  - Additional determinism scenarios
  - Layout verification edge cases

##### Redfish Client (61.3% → 90%, 28.7pp gap)
- **Target**: 61.3% → 90%
- **Note**: Interface coverage measurement limitation detected
- **Focus Areas**:
  - Helper function tests (toBootTarget 40.0%, parseRetryAfter 41.7%)
  - Error path coverage for retry/backoff logic
  - Vendor profile differences (iDRAC, iLO, Supermicro)
  - Idempotency checks

#### Priority 3: Concurrency & Recovery Tests
Per design/035_Test_Strategy.md section 7:
- Lease stealing under concurrent access
- Webhook deduplication with parallel requests
- Controller restart scenarios
- Worker shutdown (graceful vs abrupt)

#### Priority 4: Integration Tests
Per design/035_Test_Strategy.md section 5:
- Enhance mock Redfish server with vendor profiles
- Add fault injection scenarios (network errors, timeouts)
- End-to-end workflow validation
- Media server signed URL tests
- OCI registry edge cases

#### Priority 5: Security Tests
- Comprehensive auth enforcement across all endpoints
- Log redaction verification (no secrets in logs)
- Fuzzing for recipe validator
- Webhook secret rotation tests

#### Priority 6: Performance Tests
- Sparse file large blob simulation
- Timeout and backoff validation
- Memory boundary checks
- Concurrent job processing load tests

## Test Categories

### Unit Tests
- ✅ API auth (completed)
- 🔄 Jobs/worker logic (in progress)
- 🔄 ISO builder (needs improvement)
- 🔄 Redfish client (needs improvement)
- 🔄 Store operations (needs improvement)
- ✅ Crypto/password hashing (complete)

### Integration Tests
- ✅ Basic workflow tests (existing)
- 🔄 Mock Redfish enhancements (planned)
- 🔄 Vendor profile coverage (planned)
- 🔄 Fault injection (planned)

### End-to-End Tests
- ✅ Linux workflow (existing)
- ✅ Windows workflow (existing)
- 🔄 ESXi handoff (planned)
- 🔄 VM smoke tests (planned)

### Security Tests
- ✅ Auth middleware (completed)
- ✅ Rate limiting (existing)
- ✅ Security headers (existing)
- 🔄 Fuzzing (planned)

### Performance Tests
- 🔄 Load simulation (planned)
- 🔄 Timeout validation (planned)
- 🔄 Memory checks (planned)

## Validation

All tests must pass:
```bash
go run build.go validate
```

Current status: ✅ PASSING

## Next Steps

1. Add missing tests for jobs/worker to reach 90%
2. Enhance ISO builder tests for determinism and 90% coverage
3. Expand Redfish client tests with vendor profiles and fault injection
4. Improve store tests to 80%+
5. Add concurrency and recovery tests
6. Implement performance test suite
7. Verify determinism across all ISO builds
8. Update documentation with test coverage requirements

## References

- [design/035_Test_Strategy.md](../../design/035_Test_Strategy.md) - Full test strategy
- [plans/004_Phase_6_Provisioner_Plan.md](../../plans/004_Phase_6_Provisioner_Plan.md) - Phase 6 plan
- [AGENTS.md](../../AGENTS.md) - Repository testing requirements
