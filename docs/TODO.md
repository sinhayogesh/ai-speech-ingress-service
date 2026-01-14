# Speech Ingress Service — Deferred TODOs

These are **intentionally deferred** items — not gaps or oversights. Each has a clear rationale for why it's postponed.

---

## TODO-1: Automated Test Coverage

**Scope:**
- Unit tests for segment state machine
- Guardrail breach tests (max bytes, duration, partials)
- STT error injection tests
- Mock adapter edge cases

**Rationale:**
Better ROI after downstream services exist for E2E tests. Current manual testing with real audio validates core functionality.

**Priority:** Medium  
**Trigger:** When building downstream consumers (intent detection, agent orchestration)

---

## TODO-2: Advanced Observability

**Scope:**
- Latency histograms (audio-to-transcript, end-to-end)
- STT response time metrics (p50, p95, p99)
- Audio ingest rate monitoring
- Alerting thresholds

**Rationale:**
Requires production traffic patterns to set meaningful thresholds. Current basic metrics sufficient for development.

**Priority:** Medium  
**Trigger:** Production deployment with real traffic

---

## TODO-3: Multi-Language / Multi-Tenant STT

**Scope:**
- Language routing per tenant configuration
- Dynamic STT config selection (language, model, hints)
- Tenant-specific STT credentials
- Language detection fallback

**Rationale:**
Out of v1 scope. Product decision needed on tenant configuration model.

**Priority:** Low  
**Trigger:** Product requirement for multi-language support

---

## TODO-4: Advanced VAD / Endpointing

**Scope:**
- Noise-aware endpointing (traffic, keyboard, background voices)
- Barge-in optimization (detect when user interrupts agent)
- Configurable silence thresholds per tenant
- Server-side VAD as backup to STT VAD

**Rationale:**
Only relevant after agent/LLM interaction loop exists. Current Google STT VAD sufficient for basic utterance detection.

**Priority:** Low  
**Trigger:** Agent orchestration service exists with barge-in requirements

---

## TODO-5: Cost Optimization

**Scope:**
- STT streaming window tuning (batch vs. stream)
- Silence suppression (don't stream silence to STT)
- Partial throttling (rate-limit partial publishes)
- STT provider fallback (cheaper provider for low-priority calls)

**Rationale:**
Premature optimization without usage data. Need production metrics to identify cost drivers.

**Priority:** Low  
**Trigger:** Production cost reports show STT as significant expense

---

## Completed Items ✅

These items are **done, tested, and frozen**:

- [x] gRPC audio ingestion
- [x] Google Streaming STT integration
- [x] Segment lifecycle & utterance boundaries
- [x] Partial vs Final semantics
- [x] Separate Kafka topics (infrastructure-level ACLs)
- [x] Safety guardrails (limits + fail-closed behavior)
- [x] Basic observability (structured logs + Prometheus metrics)
- [x] Environment-driven configurability
- [x] End-to-end validation with real WAV audio
- [x] Real-time transcript viewer UI

---

## Decision Log

| Date | Decision | Rationale |
|------|----------|-----------|
| 2026-01-13 | Defer automated tests | Manual testing with real audio validated core. E2E tests more valuable when downstream services exist. |
| 2026-01-13 | Use continuous mode by default | Single utterance mode loses buffered audio on session restart. Continuous mode captures all speech. |
| 2026-01-13 | Separate Kafka topics | Infrastructure-level access control. Partials can be restricted without affecting finals. |
| 2026-01-13 | Freeze speech ingress core | All v1 requirements met. Further changes should be product-driven. |

