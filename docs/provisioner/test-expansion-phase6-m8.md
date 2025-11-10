# Test Expansion - Phase 6 Milestone 8

Status: In Progress  
Date: 2025-11-10  
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

## Current Status (2025-11-10)

### Coverage Progress

**Before Test Expansion:**
- Global coverage: 57.3%
- internal/provisioner/api: 55.1% ❌
- internal/provisioner/jobs: 67.8% ❌
- internal/provisioner/iso: 80.2% ⚠️ (meets 80% but critical → needs 90%)
- internal/provisioner/redfish: 61.3% ❌
- internal/provisioner/store: 60.8% ❌
- pkg/crypto: 90.1% ✅

**After Initial Expansion:**
- Global coverage: TBD (in progress)
- internal/provisioner/api: 81.7% ⚠️ (improved, but still needs 90% as critical package)
- internal/provisioner/jobs: 67.8% ❌ (no change yet)
- internal/provisioner/iso: 80.2% ⚠️ (needs 90%)
- internal/provisioner/redfish: 61.3% ❌ (no change yet)
- internal/provisioner/store: 60.8% ❌ (no change yet)
- pkg/crypto: 90.1% ✅

### Completed Work

#### 1. API Authentication Tests (auth_test.go)

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

### Remaining Work

#### 2. Jobs/State Machine Tests
- **Target**: 67.8% → 90%
- **Focus Areas**:
  - Lease acquisition and heartbeat
  - Lease expiry and steal scenarios
  - State transitions (queued → provisioning → succeeded/failed → complete)
  - Worker concurrency and serialization
  - Recovery after controller restart

#### 3. ISO Builder Tests
- **Target**: 80.2% → 90%
- **Focus Areas**:
  - Determinism validation (SOURCE_DATE_EPOCH)
  - Layout verification
  - Error handling paths
  - Permission and size checks

#### 4. Redfish Client Tests
- **Target**: 61.3% → 90%
- **Focus Areas**:
  - Vendor profile coverage
  - Retry and backoff logic
  - Idempotency checks
  - Error path coverage
  - Fault injection scenarios

#### 5. Store Tests
- **Target**: 60.8% → 80%
- **Focus Areas**:
  - CRUD operations
  - Migrations
  - Concurrency
  - Error handling

#### 6. Integration Tests
- Enhance mock Redfish server with vendor profiles
- Add fault injection scenarios
- Media server signed URL tests
- OCI registry edge cases

#### 7. Concurrency & Recovery Tests
- Lease behavior under concurrent access
- Redfish idempotency verification
- Webhook deduplication
- Controller restart scenarios

#### 8. Security Tests
- Comprehensive auth enforcement
- Log redaction verification
- Fuzzing for recipe validator
- Webhook secret rotation

#### 9. Performance Tests
- Sparse file large blob simulation
- Timeout and backoff validation
- Memory boundary checks
- Concurrent job processing

#### 10. Determinism Tests
- ISO builder with SOURCE_DATE_EPOCH
- Verify identical SHA-256 across runs
- Environment variable stability

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
