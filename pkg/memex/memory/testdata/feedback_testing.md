---
name: feedback-testing
description: Don't mock the database in integration tests
type: feedback
---

Integration tests must hit a real database, not mocks.
**Why:** Prior incident where mock/prod divergence masked a broken migration.
**How to apply:** When writing tests for data layer, always use testcontainers or t.TempDir() for SQLite.