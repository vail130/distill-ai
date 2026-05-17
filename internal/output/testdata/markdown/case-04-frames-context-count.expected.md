# events from pytest

### [1] ERROR KeyError: 'session_id'

- **Location:** `auth/views.py:112`
- **Count:** ×4
- **Vendor frames collapsed:** 8

```
KeyError: 'session_id'
  raise KeyError('session_id')
```

**Context:**

```
    response = client.post("/login", data=creds)
    assert response.status_code == 302
>   assert response.headers["location"] == "/dashboard"
```

---

- **Lines distilled:** 12,000 → 21
- **Events emitted:** 1
- **Events dropped:** 0
- **Events truncated:** 0
- **Events deduped:** 3
- **Vendor frames removed:** 8
- **Estimated tokens:** 0 (heuristic)
