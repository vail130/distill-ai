# events from pytest

### [1] ERROR AssertionError: expected 302, got 200

- **Location:** `tests/api/test_auth.py:47`

```
AssertionError: expected 302, got 200
```

### [2] WARN DeprecationWarning: use foo() instead of bar()

```
DeprecationWarning: use foo() instead of bar()
```

### [3] ERROR fixture 'db' not found

- **Location:** `tests/conftest.py:12`

```
E       fixture 'db' not found
ERROR tests/test_db.py::test_insert
```

---

- **Lines distilled:** 8,432 → 25
- **Events emitted:** 3
- **Events dropped:** 0
- **Events truncated:** 0
- **Events deduped:** 0
- **Vendor frames removed:** 0
- **Estimated tokens:** 0 (heuristic)
