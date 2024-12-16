Below is a brief design document focusing on a simplified version of LogchefQL query translation. This version does not support map fields or arrays and includes some examples of more complex queries and their corresponding optimized ClickHouse SQL.

---

# Atomic Task: LogchefQL Simplified Query Support

**Author:** Your Name
**Date:** YYYY-MM-DD
**Task ID/Reference:** LOGCHEF-SIMPLE

## 1. Overview

**Goal:**
Extend the LogchefQL parsing and SQL generation to handle basic field filtering (including nested JSON fields), equality, inequality, and pattern matching operations—without supporting map field access or arrays at this stage.

**Context:**
Currently, LogchefQL can parse simple conditions. This task will allow users to form more complex queries involving multiple conditions and nested JSON fields but will not include features like map or array field access.

**Key Outcomes / Success Criteria:**
- Support queries with multiple conditions joined by `;`.
- Handle basic operators: `=, !=, ~, !~, >, <, >=, <=`.
- Translate nested JSON field references (e.g., `p.error.code`) into proper `JSONExtract...` functions in ClickHouse.
- Provide example queries and their corresponding ClickHouse SQL.

## 2. Background & Motivation

**Current State:**
The current system handles simple queries on top-level fields only. It lacks robust support for nested fields and complex conditions.

**Why This Task:**
To allow users more flexibility in searching logs, we need to handle nested fields and multiple chained conditions. This will improve the usability of LogchefQL and reduce the need for manual SQL building.

## 3. Requirements

**Functional Requirements:**
- Parse and translate queries with top-level and nested JSON fields.
- Support `=, !=, ~, !~, >, <, >=, <=` on both string and numeric fields.
- Multiple conditions separated by `;` should translate to `AND` logic in SQL.
- No support for map fields or arrays in this iteration.

**Non-Functional Requirements:**
- Maintain parsing efficiency (under a millisecond for average queries).
- Ensure SQL injection safety by using parameterized queries.
- Keep memory usage minimal.

## 4. Proposed Solution

**High-Level Approach:**
- Extend the parser to recognize nested fields like `p.error.code`.
- Map these nested fields to `JSONExtract*` functions in ClickHouse when generating SQL.
- For string pattern matches (~ and !~), use ILIKE or NOT ILIKE.
- Combine conditions with `AND` in the final SQL.

**Example Field Mappings:**
- `service_name` (top-level): use directly as `service_name`.
- `p.error.code` (nested): `JSONExtractString(body, 'error.code')`.

**Error Handling & Edge Cases:**
- If a nested field does not exist, queries will simply return no rows (no special handling needed).
- Invalid operators on non-string fields will be documented (no runtime type checks at this stage).

## 5. Detailed Steps / Implementation Plan

1. Enhance the AST to support a `SubFields []string` for nested fields.
2. Extend the SQL builder to:
   - Convert top-level fields directly (e.g., `service_name`).
   - Convert nested fields into `JSONExtract*` calls.
3. Implement operator mappings:
   - `=` and `!=` -> `=`, `<>`
   - `~` and `!~` -> `ILIKE`, `NOT ILIKE`
   - `>`, `<`, `>=`, `<=` as is for numeric fields (assume JSONExtractFloat or JSONExtractInt as needed).
4. Chain multiple conditions separated by `;` with `AND`.
5. Add unit tests covering normal and complex scenarios.

## 6. Testing Strategy

**Test Cases:**
- Simple equality: `service_name='api'`
- Nested field numeric filter: `p.error.code=500`
- Pattern match on body: `body~'timeout'`
- Multiple conditions: `service_name='payment-service';p.error.retry_count>3;timestamp>-1h`
- Edge: Non-existent nested field or empty string value

**Automation & Tools:**
- Use Go unit tests and a mock ClickHouse connection for dry-run SQL generation.

## 7. Dependencies & Integration

- Depends on existing LogchefQL parser code.
- Integrates with the ClickHouse SQL builder (using `go-sqlbuilder`).

## 8. Deployment & Rollout Plan

- Merge changes in a feature branch.
- Deploy to a staging environment.
- Test with a sample set of queries.
- Roll out to production if no issues.

## 9. Maintenance & Support

- The owning team will monitor logs for query failures or unexpected performance degradation.
- Future enhancements might reintroduce map field support and arrays.

## 10. Open Questions & Assumptions

- Assuming all nested fields are accessible via `JSONExtractString` or `JSONExtractInt`, without schema-driven type validation.
- Future tasks may add explicit type checks.

---

### Complex Query Examples

**1. Multiple Conditions with Nested Fields and Patterns**
**LogchefQL:**
```
service_name='payment-service';p.error.retry_count>3;body~'timeout';timestamp>-1h
```
**Translated SQL (ClickHouse):**
```sql
SELECT *
FROM logs
WHERE service_name = ?
  AND JSONExtractInt(body, 'error.retry_count') > ?
  AND body ILIKE ?
  AND timestamp > now() - INTERVAL 1 HOUR;
```
**Parameters:** `['payment-service', 3, '%timeout%']`

---

**2. Numeric Comparison with Nested Fields**
**LogchefQL:**
```
p.metrics.latency>=1000;service_name='order-service';timestamp>-30m
```
**ClickHouse SQL:**
```sql
SELECT *
FROM logs
WHERE JSONExtractFloat(body, 'metrics.latency') >= ?
  AND service_name = ?
  AND timestamp > now() - INTERVAL 30 MINUTE;
```
**Parameters:** `[1000, 'order-service']`

---

**3. Pattern Matching on Nested Fields**
**LogchefQL:**
```
service_name='auth-service';p.user.username~'jdoe';severity_text='error'
```
**ClickHouse SQL:**
```sql
SELECT *
FROM logs
WHERE service_name = ?
  AND JSONExtractString(body, 'user.username') ILIKE ?
  AND severity_text = 'error';
```
**Parameters:** `['auth-service', '%jdoe%']`

---

**4. Multiple Nested Conditions**
**LogchefQL:**
```
p.request.path='/checkout';p.response.status_code!=200;timestamp>-5m;service_name='api-gateway'
```
**ClickHouse SQL:**
```sql
SELECT *
FROM logs
WHERE JSONExtractString(body, 'request.path') = ?
  AND JSONExtractInt(body, 'response.status_code') <> ?
  AND timestamp > now() - INTERVAL 5 MINUTE
  AND service_name = ?;
```
**Parameters:** `['/checkout', 200, 'api-gateway']`
