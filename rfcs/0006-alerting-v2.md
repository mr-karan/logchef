# RFC 0006: Alerting v2

Status: Draft
Date: 2026-11-15
Authors: Karan Sharma

## Summary

An alert today is one query, one number and one threshold, sent to email recipients and webhook URLs that live on the rule. This RFC turns it into a rule that keeps one state per group, and it adds the pieces around that rule:

- A rule fires and resolves separately for each value of a group key.
- A rule can report "no data" instead of reading an empty result as zero.
- Silences and weekly maintenance windows mute notifications without stopping evaluation.
- A backtest shows how often a rule would have fired before the user saves it.
- Notification destinations become reusable objects, with Slack and PagerDuty senders beside email and webhook, and a delivery queue with retries.
- A background job runner guarantees that one replica runs a given tick. Alert evaluation moves onto it. RFC 0008 and RFC 0010 use the same runner.
- After the Patterns and Compare RFC lands, a rule can fire on a new log template, and a notification can carry top patterns, a baseline comparison and sample rows.

## Problem

**One number per rule.** The evaluator reads the first column of the first row (`internal/util/query.go:16`) and compares it with the threshold (`internal/alerts/manager.go:228`). A query that returns one row per service alerts on whichever row comes first. Users who want "error rate per service" write one rule per service.

**An empty result is zero.** `ExtractFirstNumeric` returns 0 for no rows (`internal/util/query.go:17`). The test endpoint warns about it (`internal/core/alerts.go:527`), but the scheduler cannot tell "the pipeline stopped" from "nothing matched".

**No way to mute.** The only control is `is_active`, which stops evaluation and loses the state. Nothing mutes a rule for a deploy or a weekly batch job.

**No preview of behavior.** `POST /alerts/test` runs the query once, now (`internal/core/alerts.go:464`). The user learns how noisy a threshold is after it pages someone.

**Targets live on the rule.** Each rule stores its own `recipient_user_ids_json` and `webhook_urls_json` (`pkg/models/alerts.go:133`). Changing a Slack channel means editing every rule. The docs tell users to point the generic webhook at Slack or PagerDuty (`docs/src/content/docs/features/alerting.mdx:80`), but the payload (`internal/alerts/webhook_sender.go:38`) is neither a Slack message nor a PagerDuty event, so each team runs a translation service.

**Delivery blocks evaluation and retries are coarse.** The manager sends inline (`internal/alerts/manager.go:351`) inside a sequential loop (`internal/alerts/manager.go:168`). A retry happens only at the rule's next tick, up to three attempts (`internal/alerts/manager.go:28`), and the state of that retry lives in keys of the history payload (`internal/alerts/manager.go:325-340`). The docs describe an exponential backoff that the code does not have (`docs/src/content/docs/features/alerting.mdx:95`).

**Every replica evaluates every rule.** Each process runs its own ticker (`internal/alerts/manager.go:114`) over `ListActiveAlertsDue` (`internal/store/postgres/queries.sql:645`), which takes no lock. The docs tell operators to run the alert manager on exactly one replica (`docs/src/content/docs/operations/database-backends.md:65`). At startup each process also marks every in-flight delivery as interrupted (`internal/alerts/manager.go:104`), which is wrong as soon as a second replica is delivering.

**A fired alert carries no context.** The notification has the value and the query. The on-call engineer opens Explorer and starts from nothing. Ask Chef can already load an alert as context (`internal/askchef/alert.go:31`), but only after a person opens it.

## Goals and non-goals

Goals:

1. One rule keeps one state per group and notifies per group.
2. A rule has a stated policy for an empty result.
3. A user can silence a rule, a group or a whole source, once or weekly.
4. A user can backtest a rule before saving it.
5. Destinations are reusable, typed objects. Slack and PagerDuty are native.
6. One replica runs a given job tick, on SQLite and on Postgres with several replicas, through a runner that other features share.
7. Existing rules keep their behavior, and existing webhook receivers keep their payload, after the upgrade.
8. Items 1 to 6 ship without the Patterns work. New-pattern rules and triage are gated on it.

Non-goals:

- **A pending period ("for 5 minutes") and flap damping.** Useful, and the instance table makes it cheap later. Nobody asked for it, and it changes when existing rules fire.
- **Escalation chains, on-call schedules and acknowledgement.** PagerDuty owns them. The PagerDuty sender is the integration.
- **Regex matchers in silences.** Equality on labels covers a rule, a group and a source. Regex adds a second matching language.
- **A Slack app with a bot token.** It needs OAuth install, channel lookup and token storage. An incoming webhook covers posting. See open question 4.
- **Custom headers or request signing on webhooks.** No sender has them today.
- **An LLM-written summary at fire time.** Evaluation has no user, and a model call adds seconds and cost to every page. The notification links to Ask Chef, which runs as the person who clicks.
- **Rules and destinations in provisioning TOML.** Provisioning covers teams and sources only (`internal/config/provisioning.go:12`). Declarative rules need their own design for ownership and drift.
- **Encryption of destination secrets at rest.** Source passwords are stored as written and redacted on read (`pkg/models/source.go:626`). Destinations follow the same rule. Encrypting both belongs in one change.
- **Multi-query and composite conditions.**

## Design

### The shape in one page

```
alerts ──< alert_instances          one row per (rule, group), holds the state
   │  └──< alert_history            one row per episode of one group
   ├──< alert_destinations >── notification_destinations
   └──  jobs (kind alert.evaluate, key = alert id)

alert_silences                      scoped to a source, optionally to a rule
notification_deliveries             the delivery queue, drained by job notify.deliver
```

Three packages own the three concerns:

| Package | Owns |
|---|---|
| `internal/jobs` (new) | Schedules, the claim, the lease and the missed-tick policy |
| `internal/notify` (new) | Destinations, the delivery queue, the four senders |
| `internal/alerts` | The rule state machine, silences, backtest and triage |

The email and webhook senders move from `internal/alerts` to `internal/notify`. `internal/alerts` stops knowing how a message reaches anyone.

### Stored objects

Types below are Postgres types. SQLite uses `INTEGER`, `REAL`, `TEXT` and `DATETIME` in the style of the existing tables. JSON is stored as `TEXT`, as `labels_json` is today.

#### `jobs`

| Column | Type | Meaning |
|---|---|---|
| `id` | bigint, primary key | |
| `kind` | text, not null | The handler, for example `alert.evaluate` |
| `key` | text, not null | The object the job is for, for example an alert id. Unique with `kind`. |
| `schedule_kind` | text, `interval` or `cron` | |
| `schedule_expr` | text, not null | Seconds for `interval`. Five cron fields for `cron`. |
| `timezone` | text, default `UTC` | IANA name, used by `cron` |
| `misfire` | text, `run_once` or `skip` | The missed-tick policy |
| `grace_seconds` | bigint, default 0 | For `skip`, how old the latest due occurrence may be and still run |
| `enabled` | boolean | |
| `next_run_at` | timestamptz, not null | |
| `lease_owner` | text, null | The process that holds the claim |
| `lease_token` | bigint, default 0 | Incremented by every claim. A handler's writes commit only while its token is current. |
| `lease_expires_at` | timestamptz, null | |
| `last_started_at`, `last_finished_at` | timestamptz, null | |
| `last_status` | text, null | `ok`, `error`, `skipped` or `lost` |
| `last_error` | text, null | At most 512 bytes |
| `created_at`, `updated_at` | timestamptz | |

Indexes: unique `(kind, key)`, and `(enabled, next_run_at)`.

A job has no payload. The handler loads its object by `key`. A handler whose object is gone returns `jobs.ErrGone` and the runner deletes the row, so a job can never outlive its object for long.

#### `notification_destinations`

| Column | Type | Meaning |
|---|---|---|
| `id` | bigint, primary key | |
| `name` | text, not null | |
| `type` | text | `email`, `webhook`, `slack` or `pagerduty` |
| `scope` | text | `private`, `team` or `global` |
| `team_id` | bigint, null, references `teams` on delete cascade | Set only when `scope = 'team'` |
| `config_json` | text, not null | Settings that are safe to show |
| `secret` | text, not null, default `''` | Never returned by the API |
| `is_active` | boolean, default true | |
| `created_by` | bigint, null, references `users` on delete set null | |
| `last_delivery_at`, `last_delivery_status`, `last_delivery_error` | | Health shown in the list |
| `created_at`, `updated_at` | timestamptz | |

Per type:

| Type | `config_json` | `secret` |
|---|---|---|
| `email` | `{"user_ids":[3,9],"addresses":["oncall@example.com"]}` | empty |
| `webhook` | `{"payload_version":2}` | The URL |
| `slack` | `{}` | The incoming webhook URL |
| `pagerduty` | `{}` | The 32-character routing key |

A webhook URL is a secret because it often carries a token. The API returns `secret_hint` instead: the host for a URL, the last four characters for a routing key.

#### `alert_destinations`

`(alert_id, destination_id)`, primary key on both. `alert_id` cascades on delete. `destination_id` uses `ON DELETE RESTRICT`, so a destination in use cannot be deleted. RFC 0008 leaves destination recipients out of its first version. When it adds them, its link uses the same rule.

#### `notification_deliveries`

| Column | Type | Meaning |
|---|---|---|
| `id` | bigint, primary key | Also the send order |
| `destination_id` | bigint, references `notification_destinations` on delete cascade | |
| `producer` | text | `alert` |
| `producer_ref` | text | For alerts, `<alert_id>:<history_id>`: the episode of the one group that the message is about |
| `recipient` | text, default `''` | For an `email` destination, one recipient: `user:<id>` or an address. Empty for other types. |
| `sequence_key` | text | Rows with the same destination, recipient and sequence key go out in `id` order |
| `event` | text | `firing`, `resolved`, `no_data` or `test` |
| `dedup_key` | text, unique | Makes enqueue idempotent. Includes the recipient. |
| `message_json` | text | The rendered `notify.Message`, at most 256 KiB |
| `status` | text | `pending`, `attempting`, `sent`, `failed` or `unknown`. `attempting` is used by email rows only. |
| `attempts` | integer, default 0 | Incremented by every reservation |
| `next_attempt_at` | timestamptz | |
| `reserved_by` | text, null | The process that is sending the row |
| `reserved_token` | bigint, default 0 | Incremented by every reservation |
| `reserved_until` | timestamptz, null | |
| `last_error` | text, null | At most 512 bytes |
| `created_at`, `sent_at` | timestamptz | |

Index `(status, next_attempt_at)`.

An email destination gets one row per recipient, so that a retry reaches only the recipients that failed. The sender does the same today, with one result per recipient (`internal/alerts/email_sender.go:110`).

#### `alert_instances`

| Column | Type | Meaning |
|---|---|---|
| `alert_id` | bigint, references `alerts` on delete cascade | |
| `group_key` | text | `''` for an ungrouped rule. Otherwise the JSON array of group values in `group_by` order, or its SHA-256 in hex when longer than 512 bytes. |
| `group_json` | text | `{"service":"checkout"}`, each value cut to 256 characters |
| `state` | text | `ok`, `firing` or `no_data` |
| `value` | double precision, null | The last value read |
| `state_since` | timestamptz | |
| `last_seen_at` | timestamptz | The last evaluation that returned a row for the group |
| `history_id` | bigint, null | The open episode |
| `notified` | boolean | A notification went into the queue for the open episode |

Primary key `(alert_id, group_key)`.

#### Changes to `alerts`

| Column | Change |
|---|---|
| `rule_type` | New. `threshold` (default) or `new_pattern`. |
| `group_by_json` | New. Array of at most four result column names. Null means ungrouped. |
| `no_data_policy` | New. `zero` (default), `no_data` or `keep`. |
| `notify_on_resolve` | New. Boolean, default true. |
| `triage_enabled` | New. Boolean, default false. |
| `pattern_json` | New. Set only for `new_pattern`. `{"filter":"level=\"error\"","baseline":"previous"}` |
| `eval_seq` | New. Bigint, default 0. Guards concurrent writers. |
| `last_state` | The check gains `no_data`. |
| `recipient_user_ids_json`, `webhook_urls_json` | Dropped after the data moves to destinations. |

`last_state` stays as a summary for the list. It is `firing` when any instance fires, else `no_data` when any instance has no data, else `resolved`.

#### Changes to `alert_history`

| Column | Change |
|---|---|
| `group_key` | New. Text, not null, default `''`. |
| `group_json` | New. Text, null. |
| `status` | The check gains `no_data`. |

A history row is one episode of one group, from the transition into `firing` or `no_data` until the group returns to `ok`. The payload keys that start with `delivery_` are no longer written. Delivery state lives in `notification_deliveries`.

#### `alert_silences`

| Column | Type | Meaning |
|---|---|---|
| `id` | bigint, primary key | |
| `source_id` | bigint, not null, references `sources` on delete cascade | The scope and the access check |
| `alert_id` | bigint, null, references `alerts` on delete cascade | Null means every rule on the source |
| `matchers_json` | text, default `{}` | Label equality, at most five pairs. All must match. |
| `comment` | text, not null | Why |
| `schedule_kind` | text | `once` or `weekly` |
| `starts_at`, `ends_at` | timestamptz | Required for `once`. For `weekly`, `ends_at` is an optional last day. |
| `weekly_json` | text, null | `{"weekdays":[0,6],"start":"02:00","duration_minutes":120,"timezone":"Asia/Kolkata"}` |
| `created_by`, `created_at`, `updated_at` | | |

A maintenance window is a silence with `schedule_kind = 'weekly'`. One object and one list cover both.

### Migrations

Each migration takes the next free number at build time, in both `internal/store/sqlite/migrations` and `internal/store/postgres/migrations`.

| Purpose | Notes |
|---|---|
| `add_jobs` | Creates `jobs`. Inserts one `alert.evaluate` job per existing alert, with `next_run_at` set to `last_evaluated_at + frequency_seconds`, or now when null. The interval is `max(frequency_seconds, 10)`, the same floor that scheduling applies. Idempotent through the unique `(kind, key)`. |
| `add_alert_instances` | Creates `alert_instances`. Adds `group_key`, `group_json` to `alert_history`. Adds `group_by_json`, `no_data_policy`, `eval_seq` to `alerts`. Backfills instances as described under "Backfill of firing rules". Widens both checks to allow `no_data`. |
| `add_alert_silences` | Creates `alert_silences`. |
| `add_notification_destinations` | Creates `notification_destinations` and `alert_destinations`, then moves the data as described under "Migration of existing alerts". Drops the two JSON columns. |
| `add_notification_deliveries` | Creates `notification_deliveries`. Removes the `delivery_*` keys from open history payloads. |
| `add_alert_rule_options` | Adds `notify_on_resolve`. |
| `add_alert_patterns` | Phase 2. Adds `rule_type`, `pattern_json`, `triage_enabled`. |

**SQLite rebuilds.** SQLite cannot change a `CHECK`, so `add_alert_instances` rebuilds `alerts` and `alert_history`, in the shape of `internal/store/sqlite/migrations/000027_remove_legacy_query_type.up.sql:38`. A `DROP TABLE alerts` with foreign keys on would delete every history row through the cascade. The rebuild is safe for one reason, and this RFC relies on it: Logchef runs migrations on a separate connection opened with the bare file path (`internal/store/sqlite/sqlite.go:129`), not with the DSN that turns `foreign_keys` on (`internal/store/sqlite/sqlite.go:166`). Foreign keys are therefore off for the whole migration. The `PRAGMA foreign_keys=off` line inside the migration file does nothing, because the migration driver wraps the file in a transaction and SQLite ignores that pragma inside one. Keep the line out of new migrations so that nobody trusts it.

Each rebuild ships an upgrade test next to `TestDatasourceUpgradeFromV24` (`internal/store/sqlite/upgrade_test.go:23`). The test runs the real migration runner over a database that holds alerts, history rows and, for later rebuilds, instances and destination links. It asserts the row counts and ids afterward. `PRAGMA foreign_key_check` alone does not prove that child rows survived.

**Backfill of firing rules.** `add_alert_instances` gives each alert that has an open history row (`status = 'triggered'`) one instance:

| Field | Value |
|---|---|
| The episode | The open row that `GetLatestUnresolvedAlertHistory` picks today: newest `triggered_at`, then highest `id` |
| Other open rows of the alert | Closed with `status = 'resolved'`, `resolved_at` now and the message "superseded at upgrade" |
| `group_key`, `group_json` | `''` and `{}` |
| `state` | `firing` |
| `value` | The row's `value` |
| `state_since` | The row's `triggered_at` |
| `last_seen_at` | The alert's `last_evaluated_at` |
| `history_id` | The row's id |
| `notified` | True, for every backfilled instance |

`notified = true` is a choice. Every open row has had at least one delivery attempt, because the manager writes the row and the attempt count together (`internal/alerts/manager.go:337`). True means that the next tick sends no second `firing` message to targets that already got one, and that the later `resolved` message goes out. The cost is that a retry that was pending or interrupted at upgrade is dropped. The upgrade note says so. The upgrade test has four fixtures: delivered, retry pending, interrupted, and two open rows.

**No rolling upgrade.** The migrations drop two columns that the old binary names in its create and update statements (`internal/store/postgres/queries.sql:552`), and the old binary evaluates without instances. An old replica that runs after the migration fails on writes and corrupts the state for new replicas. Stop every old replica before the first new one starts. The upgrade note states the downtime and tells the operator to back up the metadata database first.

The down migrations drop the new tables and columns. They do not restore the two JSON columns from destinations. Migrations are forward-only in practice (`CLAUDE.md`), and the upgrade note says to back up the metadata database.

### Models

In `pkg/models/alerts.go`:

```go
type AlertRuleType string // "threshold", "new_pattern"

type AlertNoDataPolicy string // "zero", "no_data", "keep"

const (
	AlertStateFiring   AlertState = "firing"
	AlertStateResolved AlertState = "resolved"
	AlertStateNoData   AlertState = "no_data" // new
)

const AlertStatusNoData AlertStatus = "no_data" // new, beside triggered, resolved, error

type AlertInstanceState string // "ok", "firing", "no_data"

type AlertInstance struct {
	AlertID    AlertID            `json:"alert_id"`
	GroupKey   string             `json:"group_key"`
	Group      map[string]string  `json:"group"`
	State      AlertInstanceState `json:"state"`
	Value      *float64           `json:"value,omitempty"`
	StateSince time.Time          `json:"state_since"`
	LastSeenAt *time.Time         `json:"last_seen_at,omitempty"`
	HistoryID  *int64             `json:"history_id,omitempty"`
	Silenced   bool               `json:"silenced"` // computed at read time
}

type AlertPatternConfig struct {
	Filter   string `json:"filter"`   // LogchefQL
	Baseline string `json:"baseline"` // "previous", "day", "week"
}
```

`Alert` gains `RuleType`, `GroupBy []string`, `NoDataPolicy`, `NotifyOnResolve`, `TriageEnabled`, `Pattern *AlertPatternConfig`, `DestinationIDs []DestinationID` and `InstanceCounts` (firing, no data, ok). It loses `RecipientUserIDs` and `WebhookURLs`. `AlertHistoryEntry` gains `GroupKey`, `Group` and `Deliveries []DeliverySummary`.

New files `pkg/models/notifications.go` and `pkg/models/silences.go` hold `NotificationDestination`, `DeliverySummary` and `AlertSilence`, with the fields of the tables above. `NotificationDestination` has no `Secret` field in its JSON form. It has `HasSecret bool` and `SecretHint string`.

### The job runner

```go
package jobs

type Kind string

// Job is one claimed run.
type Job struct {
	ID         int64     // stable for the life of the job
	Kind       Kind
	Key        string
	At         time.Time // the latest due occurrence, the time this run stands for
	ClaimedAt  time.Time // the database clock at the claim
	LeaseToken int64     // the claim's identity, checked by Hold
}

// Hold proves, inside the handler's write transaction, that this run still
// owns the job. It returns ErrLeaseLost when a later claim exists.
func Hold(ctx context.Context, tx store.StoreOps, job Job) error

type HandlerFunc func(ctx context.Context, job Job) error

// KindSpec is registered once per process, at startup.
type KindSpec struct {
	Kind        Kind
	Handler     HandlerFunc
	Timeout     time.Duration // the handler's deadline
	Concurrency int           // runs of this kind at once, per process
}

type Misfire string // "run_once", "skip"

// Spec is the stored definition of one job.
type Spec struct {
	Kind     Kind
	Key      string
	Schedule Schedule // Every(d) or Cron(expr, location)
	Misfire  Misfire
	Grace    time.Duration
	Enabled  bool
}

var ErrGone = errors.New("jobs: the object of this job no longer exists")

func (r *Runner) Register(spec KindSpec)
func (r *Runner) Start(ctx context.Context)
func (r *Runner) Stop(ctx context.Context) error

// Upsert and Delete take the caller's transaction, so a rule and its job
// change together.
func Upsert(ctx context.Context, tx store.StoreOps, spec Spec) error
func Delete(ctx context.Context, tx store.StoreOps, kind Kind, key string) error

// Kick makes a job due now and wakes the local loop.
func (r *Runner) Kick(ctx context.Context, kind Kind, key string) error
```

**The claim.** Each process polls every `jobs.poll_interval` (default 2 seconds) and when `Kick` wakes it. For each registered kind with free slots it runs one statement:

```sql
UPDATE jobs
SET lease_owner = $1, lease_token = lease_token + 1,
    lease_expires_at = now() + $2 * interval '1 second', last_started_at = now()
WHERE id IN (
    SELECT id FROM jobs
    WHERE enabled AND kind = $3 AND next_run_at <= now()
      AND (lease_expires_at IS NULL OR lease_expires_at < now())
    ORDER BY next_run_at
    LIMIT $4
    FOR UPDATE SKIP LOCKED
)
RETURNING *, now() AS claimed_at;
```

On Postgres, two replicas that run this statement at once receive disjoint rows. SQLite has one writer and one process, so its version of the query omits `FOR UPDATE SKIP LOCKED`. Every time comparison uses the database clock, so skew between replicas does not matter.

**The guarantee.** It has two parts, and they are different in strength.

- *One replica starts a given tick.* The claim gives this, except when a process stalls for longer than its lease while it holds a job, for example a frozen virtual machine. The lease is `Timeout + 30 seconds`, and the handler's context ends at `Timeout`. Cancellation is cooperative, so the lease alone does not prove that the first run stopped. After a stall, a second replica claims the job and both handlers may be running.
- *Only the current claim can write.* This part has no exception. A handler calls `jobs.Hold(ctx, tx, job)` as the first statement of every transaction that changes state. `Hold` runs `UPDATE jobs SET updated_at = now() WHERE id = $1 AND lease_token = $2`. No row updated means a later claim exists, and `Hold` returns `ErrLeaseLost`, which rolls the transaction back. A row updated means the token is current, and the row lock that the update takes lasts until commit. A competing claim uses `FOR UPDATE SKIP LOCKED`, so it skips the locked job and cannot raise the token before the handler commits. A stale run therefore cannot commit state, history or delivery rows after a newer claim, whichever of the two reaches the database first.

Effects outside the database cannot be fenced that way. The design keeps them out of handlers that hold state: the alert handler sends nothing itself and only writes delivery rows, and the delivery drain uses its own row reservations, described under "The drain".

**Completion.** The runner computes the next run from the schedule and writes `next_run_at`, `last_status`, `last_error` and clears the lease, with `WHERE id = $1 AND lease_token = $2`. A run that updates no row logs the loss and counts `result="lost"`. A handler error does not change the schedule.

**Shutdown.** `Stop` cancels the handler contexts and waits for the handlers to return, within the shutdown budget. It clears the lease of a job only after that job's handler has returned. A handler that is still running when the budget ends keeps its lease, which then ends by time.

**The missed-tick policy.** The stored `next_run_at` is when the job became due. The time a run stands for is a different value, `Job.At`. At the claim, the runner finds the latest occurrence of the schedule that is not after the database clock, starting from `next_run_at`. That occurrence is `At`. `At` is never the claim time or the wall clock, so a late claim does not shift an evaluation window or a report edition. Older missed occurrences are dropped, so a job never runs once per missed tick. The search never changes what `At` means. It starts from the later of `next_run_at` and seven days before the database clock. The finest cron schedule fires once a minute, so seven days is at most 10,080 occurrences, and an occurrence older than seven days could not be the latest due one for any schedule that fires at least weekly. The search still has a hard cap of 50,000 occurrences. If it reaches the cap, the runner does not run the job. It releases the claim without changing `next_run_at`, logs an error with the job id and counts `result="deferred"`. It never runs an intermediate occurrence.

The policy then applies to `At`:

| Policy | Behavior | Used by |
|---|---|---|
| `run_once` | Run with `At`. | Alerts, the delivery drain |
| `skip` | Run with `At` when `ClaimedAt - At <= grace_seconds`. Otherwise do not run, and record `last_status = 'skipped'`. | Jobs where a late run is worse than none. RFC 0008 uses a grace of one hour, RFC 0010 uses none. |

In both cases the next run is the first occurrence after `At`. Because `At` is the latest due occurrence, it is always less than one schedule interval old. An alert that was down for an hour evaluates its normal lookback ending at its latest tick, and a daily report claimed at 09:20 is the 09:00 edition.

**Schedules.** `interval` runs every N seconds. `cron` takes five fields and a timezone. Alerts need only `interval`. `cron` is built when RFC 0008 starts (unit J3) because it needs a parser. See open question 5.

**Identity.** `lease_owner` is a random id made at process start. It does not reuse `export.instance_id` (`internal/config/config.go:122`), which must be stable across restarts. A lease that belongs to a dead process ends by time, and nothing needs the owner to come back.

**Configuration.** `[jobs] enabled = true` and `poll_interval = "2s"`. A replica with `jobs.enabled = false` serves HTTP and claims nothing. A replica with `alerts.enabled = false` does not register the alert kinds. `alerts.evaluation_interval` is deleted, because each rule's job carries its own frequency. `[alerts] max_concurrent_evaluations = 4` sets the `Concurrency` of `alert.evaluate`.

### Evaluation

`core.CreateAlert`, `UpdateAlert` and `DeleteAlert` call `jobs.Upsert` or `jobs.Delete` inside the same `WithTx` (`internal/store/store.go:36`). The job is `interval` with the rule's `frequency_seconds`, `run_once`, and `enabled = is_active`. New and edited rules need `frequency_seconds >= 10`.

The handler for `alert.evaluate` does five steps.

**1. Run the query.** Same call as today, `datasource.Service.EvaluateAlert` (`internal/datasource/service.go:443`), with the limits it already has. `AlertQueryRequest` (`internal/datasource/query.go:55`) gains `At time.Time`. The handler passes `job.At`, and the query window is the lookback that ends at `At`. See "Time macros".

**2. Turn rows into samples.**

```go
type Sample struct {
	GroupKey string
	Group    map[string]string
	Value    float64
}
```

For an ungrouped rule, the sample is the first column of the first row, as today. No rows means no sample. For a grouped rule, each row is one sample. The group columns are the names in `group_by`. The value column is the column named `value` when one exists, otherwise the first column that is not a group column. The evaluation fails, and records an `error` history row, when a group column is missing, when two rows have the same group, when a value is not a finite number, or when the result was truncated. A truncated result means more than 100 groups (`internal/datasource/clickhouse_provider.go:649`). The error text tells the user to add a `HAVING` clause or a `LIMIT` with an `ORDER BY`.

VictoriaLogs already returns one row per label set with `value` first (`internal/victorialogs/querying.go:703`), so `... | stats by (service) count() as value` needs no provider change.

**3. Compute the transitions.** One pure function holds every rule:

```go
func Transition(rule Rule, instances []Instance, samples []Sample, now time.Time) []Change
```

| Input for a group | `zero` | `no_data` | `keep` |
|---|---|---|---|
| A sample that meets the threshold | `firing` | `firing` | `firing` |
| A sample that does not | `ok` | `ok` | `ok` |
| No sample, group is tracked | Compare 0 with the threshold | `no_data` | No change |
| No sample at all, ungrouped rule | Compare 0 with the threshold | `no_data` | No change |

`zero` is today's behavior and the default, so an existing rule does not change. Under `zero` only firing groups are stored. Under `no_data` and `keep` the table also tracks `ok` groups, so that a group that stops reporting is noticed. A rule tracks at most 200 groups. Beyond that, the evaluation records an error.

**Forgetting a group.** A group that returned no row for 24 hours is forgotten, in whatever state it is. Forgetting is a resolve with a reason, so it has the same end as any other resolve:

| State when forgotten | What happens |
|---|---|
| `ok` | The instance is deleted. |
| `no_data` | The episode closes with the message "group forgotten after 24 hours without data". The `resolved` notification goes out when `notified` and `notify_on_resolve` are both true. The instance is deleted. |
| `firing`, which only `keep` can leave without a sample | The same as `no_data`. |

No open episode and no PagerDuty incident outlives its group. A rule with `notify_on_resolve = false` leaves the incident for a person to close, as it does for every other resolve.

A grouped rule with no rows and no tracked groups uses the group key `''` for its `no_data` state.

**4. Enrich.** Phase 2 only. When a change into `firing` will notify and the rule has `triage_enabled`, the handler builds the triage block. See "Triage".

**5. Commit.** One transaction:

1. `jobs.Hold(ctx, tx, job)`. A lost lease ends the evaluation with nothing written.
2. `UPDATE alerts SET eval_seq = eval_seq + 1 ... WHERE id = ? AND eval_seq = ?` with the value read in step 1. No row updated means a user edited or resolved the rule during the evaluation. The handler stops and returns nil.
3. Upsert and delete instances. Insert a history row for each new episode and close the row of each ended one.
4. Update `last_state`, `last_evaluated_at` and `last_triggered_at`.
5. Insert the delivery rows, unless a silence matches.

The two guards cover two different writers. `Hold` excludes another run of the same job. `eval_seq` excludes a person, whose edit or manual resolve does not go through the job.

Then the handler calls `Kick(notify.deliver)`. Because the delivery rows commit with the state, a crash after the commit loses nothing, and a crash before it changes nothing. Error history rows from a failed evaluation are written the same way, behind `Hold`.

An edit that changes `query`, `query_language`, `group_by`, the threshold or `rule_type` closes every open episode with the message "rule changed", deletes the instances and increments `eval_seq`. An edit to anything else keeps the state. Manual resolve does the same for one group or for all, and the group fires again at the next tick if the condition still holds, as it does today.

History pruning keeps every open episode and the newest `alerts.history_limit` closed ones (default 50).

### Time macros

Alert queries gain the built-in macros that dashboards use (`internal/template/builtins.go:78`). The evaluator substitutes `{{__from}}`, `{{__to}}` and `{{__time_filter}}` with `At - lookback_seconds` and `At`, the way `internal/core/query.go:168` does for raw SQL. A query without macros runs unchanged. This closes the gap that RFC 0003 item 28 names (`rfcs/0003-dashboards-v2.md:3722`).

Macros matter here because a query with `now() - toIntervalSecond(300)` cannot be run for a past instant, and backtest needs exactly that. The condition editor generates `now() - toIntervalSecond(...)` today (`frontend/src/composables/useAlertForm.ts:179`). It changes to `{{__time_filter}}`. On VictoriaLogs the provider already owns the window (`internal/victorialogs/querying.go:775`), so `At` sets the `time` parameter and nothing else changes.

**One render path.** Save-time validation parses the query text as SQL today (`internal/core/alerts.go:181` calls `ValidateAlertQuery` at `internal/datasource/service.go:222`), and a `{{__time_filter}}` placeholder is not SQL. Validation and execution therefore share one function, `renderAlertQuery(source, query, from, to)`, which expands the built-ins and then applies `formatAlertQuery`. Validation calls it with a fixed sample window. Create, update, the test route, scheduled evaluation and backtest all go through it, so a query that saves also runs. A user variable is still rejected, because an alert has nobody to supply it.

### Silences

A silence is active at time `t` when `schedule_kind = 'once'` and `starts_at <= t < ends_at`, or when `schedule_kind = 'weekly'` and `t` falls in a window of `weekly_json` in its timezone, on or before `ends_at`. The check is a pure function, `Silence.ActiveAt(t)`.

A silence matches an instance when it is active, its `source_id` is the rule's source, its `alert_id` is null or the rule's id, and every matcher equals the instance's label. The labels are the rule's labels, `alertname`, `severity`, and the group values.

A silenced instance still changes state and still writes history, with `silenced_by` in the payload. It sends nothing. Two rules cover the edges:

- When the silence ends and the instance still fires, the next evaluation sends the `firing` notification. `notified = false` on the instance is what tells the evaluator that it owes one.
- A `resolved` notification goes out only when `notified` is true. A group that fired and resolved inside a silence sends nothing at all.

The evaluator loads the silences of the source once per evaluation.

### Notification destinations and delivery

```go
package notify

type Message struct {
	Event    Event    // firing, resolved, no_data, test
	Title    string   // at most 200 characters
	Text     string   // plain text, at most 8 KiB
	HTML     string   // optional, used by email only
	URL      string   // a link back to Logchef
	Severity Severity // info, warning, critical
	DedupKey string   // stable for one incident
	Fields   []Field  // ordered name and value pairs, at most 20
	Alert    *AlertPayload // the structured form, for webhook JSON
}

type Sender interface {
	// Send delivers one row. recipient is set for email only. It returns
	// Permanent(err) for an error that a retry cannot fix, and
	// ErrOutcomeUnknown when the receiver may have accepted the message.
	Send(ctx context.Context, dest models.NotificationDestination, secret, recipient string, deliveryID int64, msg Message) error
}

// Enqueue writes the delivery rows inside the caller's transaction: one per
// destination, or one per recipient for an email destination. A row whose
// dedup_key exists is skipped.
func Enqueue(ctx context.Context, tx store.StoreOps, deliveries []Delivery) error

// Mailer is the mail layer under the email sender. RFC 0008 calls it
// directly, once per recipient.
type MailMessage struct {
	Subject, Text, HTML string
	Attachments         []Attachment // name, content type, bytes
}

func (m *Mailer) Send(ctx context.Context, to string, msg MailMessage) error

// ExpandEmailDestination returns the recipients of an email destination:
// Logchef users by id, and external addresses.
func ExpandEmailDestination(ctx context.Context, db store.StoreOps, id models.DestinationID) ([]Recipient, error)
```

Each sender renders `Message` into its wire format. `internal/alerts` builds the messages and never sees a destination type. Every destination type gets one message per group that changed: a tick in which three groups change sends three messages to each destination. See "Changes after build".

| Type | Messages per tick | Wire format |
|---|---|---|
| `email` | One per group and recipient | Plain text (`text/plain`), as before. The subject names the group and the event. |
| `slack` | One per group | `POST` to the webhook URL with `text` and Block Kit `blocks` |
| `webhook`, version 2 | One per group | The version 2 JSON below |
| `webhook`, version 1 | One per group | The existing payload, unchanged |
| `pagerduty` | One per group | Events API v2, one `dedup_key` per group |

**The drain.** `notify.deliver` is one job with the key `default`, an interval of 10 seconds and a timeout of 2 minutes. The job decides when a drain runs. It does not decide who may send a row, because the runner allows two handlers to overlap after a stall. Each row has its own reservation.

A run reserves up to 50 rows with one statement:

```sql
UPDATE notification_deliveries d
SET reserved_by = $1, reserved_token = reserved_token + 1,
    reserved_until = now() + interval '2 minutes', attempts = attempts + 1
WHERE d.id IN (
    SELECT c.id FROM notification_deliveries c
    WHERE c.status = 'pending' AND c.next_attempt_at <= now()
      AND (c.reserved_until IS NULL OR c.reserved_until < now())
      AND NOT EXISTS (
          SELECT 1 FROM notification_deliveries e
          WHERE e.destination_id = c.destination_id AND e.recipient = c.recipient
            AND e.sequence_key = c.sequence_key AND e.status IN ('pending', 'attempting') AND e.id < c.id)
    ORDER BY c.id
    LIMIT 50
    FOR UPDATE SKIP LOCKED
)
RETURNING d.*;
```

Four workers send the reserved rows, each send with a 60-second deadline, which is shorter than the reservation. The result is written with `WHERE id = $1 AND reserved_token = $2`. A write that updates no row means another drain took the row over. The stale result is dropped, logged and counted, so a stale drain cannot overwrite the retry state of the newer one.

The `NOT EXISTS` clause is the ordering rule. A row is not eligible while an earlier row for the same destination, recipient and `sequence_key` is still `pending`, reserved or not. A `resolved` therefore never overtakes its `firing`, with one drain or with two.

**Delivery is at least once, except for email.** For webhook, Slack and PagerDuty rows, two cases send a message twice, and neither can be removed. A process dies after the receiver accepted the message and before the status commit. A send takes longer than its reservation. In both, the row is reserved again and sent again. Three things limit the effect:

- PagerDuty deduplicates by `dedup_key`.
- Every webhook request, in both payload versions, carries the header `Logchef-Delivery-Id` with the row id. Version 2 also has `delivery_id` in the body. A receiver can drop a repeat.
- Slack has no such key. A repeat there is one extra message.
- Email is the exception. It is at most once from the moment SMTP may have accepted the message. See "Email" below.

**Retries.** A sender classifies its error.

| Result | Action |
|---|---|
| Success | `sent` |
| Network error, timeout, HTTP 408, 429 or 5xx | Retry after 30 seconds, 2 minutes, 10 minutes and 30 minutes. A `Retry-After` header replaces the delay, up to 10 minutes. After five attempts the row is `failed`. |
| Any other HTTP 4xx, a denied address, a destination that is inactive, a recipient user who no longer exists or has no address | `failed` at once |
| An SMTP error after the end of `DATA` | `unknown`, never retried. The server may have accepted the message. |
| An email row found in `attempting` after its reservation ended | `unknown`, never retried. The process that held it crashed or stalled during the SMTP transaction. |

Each run also deletes `sent`, `failed` and `unknown` rows older than 30 days and writes the destination's `last_delivery_*` columns.

**Email.** At enqueue, an email destination expands into one row per recipient through `ExpandEmailDestination`. A user recipient is stored as `user:<id>` and resolved to the user's current address at send time, as today. `Mailer.Send` sends one message to one address. It returns `ErrOutcomeUnknown` for a failure after the end of `DATA`. RFC 0008 calls `Mailer.Send` directly and keeps its own retries and records.

An email must never be sent again once the SMTP server may have accepted it, and a process crash must not weaken that. A reservation alone cannot give it, because a reserved row that outlives its process looks the same whether the crash came before the connection or after `DATA`. Email rows therefore get a durable marker:

1. Before the worker opens the SMTP transaction, it sets `status = 'attempting'` with `WHERE id = $1 AND reserved_token = $2 AND status = 'pending'` and commits. No row updated means the reservation is lost, and the worker sends nothing.
2. The worker sends. It writes the result with the same token check: `sent`, `failed`, `unknown`, or back to `pending` with a later `next_attempt_at` for an error that came before the end of `DATA`.
3. Each drain run starts with one statement that sets `status = 'unknown'`, increments `reserved_token` and stores the error "interrupted during SMTP" on every row with `status = 'attempting'` and `reserved_until < now()`. The reservation query takes `pending` rows only, so an `attempting` row is never reserved or sent again.

The marker is conservative. A crash after the marker and before the connection also ends as `unknown`, and that message is lost. One missed alert email with a visible `unknown` in history is the chosen cost of never mailing twice. The token increment in step 3 means that a stalled worker which wakes later cannot overwrite `unknown`. The ordering clause treats `attempting` like `pending`, so a `resolved` mail still waits for its `firing`. Webhook, Slack and PagerDuty rows never use `attempting` and stay at least once.

**Outbound requests.** Webhook, Slack and PagerDuty share one HTTP client, built the way `NewWebhookSender` builds it today: the dial control that rejects private and loopback addresses unless the host is in `alerts.webhook_allowed_hosts` (`internal/alerts/webhook_sender.go:74`), no redirects (`internal/alerts/webhook_sender.go:104`), a 256-byte cap on the response body read, and the `alerts.request_timeout` setting. A Slack destination accepts only `https://hooks.slack.com/` and `https://hooks.slack-gov.com/` URLs. A PagerDuty destination has no URL. Email keeps the SMTP settings in system settings (`internal/alerts/dynamic_email_sender.go:27`).

**Slack.** The body is `{"text": "<title>", "blocks": [...]}`. `text` is required and is the fallback for notifications. Slack answers HTTP 200 with the body `ok`. Error strings such as `channel_is_archived`, `no_service` and `invalid_payload` are permanent. HTTP 429 carries `Retry-After`. Incoming webhooks cannot choose a channel and do not return the message id, so a `resolved` is a new message, not a thread reply. Source: [Sending messages using incoming webhooks](https://docs.slack.dev/messaging/sending-messages-using-incoming-webhooks) and [Rate limits](https://docs.slack.dev/apis/web-api/rate-limits), read 2026-11-15.

**PagerDuty.** `POST https://events.pagerduty.com/v2/enqueue` with:

```json
{
	"routing_key": "<secret>",
	"event_action": "trigger",
	"dedup_key": "logchef:alert:42:9f2c1a...",
	"payload": {
		"summary": "High error rate: service=checkout value 412 > 100",
		"source": "<source name>",
		"severity": "critical",
		"timestamp": "2026-11-15T10:00:00Z",
		"group": "service=checkout",
		"class": "threshold",
		"custom_details": { "value": 412, "query": "...", "labels": {} }
	},
	"links": [{ "href": "<generator url>", "text": "Open in Logchef" }]
}
```

`event_action` is `trigger` for `firing` and `no_data`, and `resolve` for `resolved`. `dedup_key` is `logchef:alert:<id>:<first 16 hex of the SHA-256 of group_key>`, well under the 255-character limit. `summary` is cut to 1024 characters. Logchef severity `info`, `warning` and `critical` map to the same PagerDuty values. HTTP 202 is success, 400 is permanent, 429 and 5xx retry. Source: [Send an alert event](https://docs.pagerduty.com/developer/send-alert-event) and [Events API v2 overview](https://docs.pagerduty.com/developer/events-api-v2-overview), read 2026-11-15.

**Webhook version 2.**

```json
{
	"version": 2,
	"kind": "alert",
	"event": "firing",
	"title": "High error rate: service=checkout firing",
	"url": "https://logchef.example.com/logs/alerts/42?source=7",
	"severity": "critical",
	"sent_at": "2026-11-15T10:00:03Z",
	"delivery_id": 5512,
	"alert": {
		"id": 42, "name": "High error rate", "description": "",
		"source_id": 7, "source_name": "app-logs", "rule_type": "threshold",
		"query_language": "clickhouse-sql", "query": "...",
		"threshold_operator": "gt", "threshold_value": 100,
		"frequency_seconds": 60, "lookback_seconds": 300,
		"labels": {}, "annotations": {}
	},
	"instances": [
		{ "group": { "service": "checkout" }, "status": "firing", "value": 412,
		  "triggered_at": "2026-11-15T10:00:00Z", "resolved_at": null, "history_id": 981 }
	],
	"instances_omitted": 0,
	"triage": null
}
```
`status` in an instance is `firing`, `resolved` or `no_data`. `instances` holds the one group of the message, so `instances_omitted` is always 0.

### Migration of existing alerts and of the webhook payload

**Rules.** Every existing rule becomes an ungrouped `threshold` rule with `no_data_policy = 'zero'` and `notify_on_resolve = true`. Its evaluation result is the same as before for every input.

**Targets.** `add_notification_destinations` moves each rule's targets in SQL. Postgres reads the arrays with `jsonb_array_elements_text`. SQLite uses `json_each`.

- Each distinct webhook URL of one creator becomes one `webhook` destination with `scope = 'private'`, `created_by` set to the rule's creator, the name `Webhook <host>` and `payload_version = 1`. Rules of the same creator that used the same URL share it.
- Each rule with recipients gets one `email` destination with `scope = 'private'`, the name `Email for <rule name>` and the same `user_ids`.
- A rule with a null `created_by` gets destinations with a null `created_by`. Only global admins can edit them, which matches who can edit the rule (`internal/core/alerts.go:420`).

The migration runs once inside the migration transaction, and it drops the two columns in the same file. The link table is new, so no SQLite rebuild is needed here. Its upgrade test still seeds rules, history and instances and asserts that they survive.

**The webhook payload.** A destination with `payload_version = 1` receives the 20 fields of `webhookPayload` (`internal/alerts/webhook_sender.go:38`) with the same names and types, and no new top-level field. Every migrated destination is version 1, so no receiver sees a change at upgrade.

Version 1 on a rule that uses the new features follows three rules, so that a strict receiver keeps working:

- A grouped rule sends one request per group. The group values are added to `labels`, and `annotations.group` holds the group as `service=checkout`.
- `status` is only ever `triggered` or `resolved`. A `no_data` episode is sent as `triggered` with `annotations.reason = "no_data"` and a `message` that says so.
- Triage is never included.

A new webhook destination defaults to version 2. The destination form has a version selector, so a team moves a receiver when it is ready.

**Delivery state.** Retries that were pending in history payloads at the moment of upgrade are dropped, and "Backfill of firing rules" says what each open episode keeps. `add_notification_deliveries` removes the `delivery_*` keys only after that backfill has read them. History rows from before the upgrade show no delivery detail.

**The API.** `recipient_user_ids` and `webhook_urls` leave the alert JSON. A create or update request that contains either key gets HTTP 400 with a message that names `destination_ids`. The check is on the presence of the key, so an empty array is rejected too. A script that sends `[]` to clear targets must not appear to succeed while doing nothing. See open question 1.

**Upgrade.** This release cannot be rolled out replica by replica. See "No rolling upgrade" under "Migrations".

### Backtest

A backtest answers one question before the user saves a rule: over the last N hours, when would this rule have fired? It runs the rule's query for past instants and feeds the samples through the same `Transition` function that the scheduler uses, with an in-memory set of instances. The no-data policy, the grouping and the threshold therefore behave exactly as they will live. Silences are not applied.

Backtest works by replay. It runs up to 120 queries, one after another, and each is exact for the instant it evaluates. A single query with time buckets is not in this RFC. See "Alternatives rejected".

**Replay.** The step is `max(frequency_seconds, ceil(range / 120))`. The handler evaluates at each step with `At` set to that instant. When the step is longer than the lookback, some rows fall between two evaluations. The response reports that as `coverage = min(1, lookback / step)`, and the form shows "Evaluated 96 of 1,440 ticks, covering 33% of the range".

The server controls the window when the rule is a VictoriaLogs rule, or a ClickHouse rule whose query uses the time macros. A ClickHouse query that uses `now()` gets HTTP 422 with "This query reads the current time. Replace the time filter with {{__time_filter}} to backtest it."

A LogchefQL rule compiled by RFC 0005 (unit Q1) is always replayable, because the server passes the window to the compile call.

The response:

```json
{
	"start": "2026-11-14T10:00:00Z", "end": "2026-11-15T10:00:00Z",
	"step_seconds": 900, "evaluations": 96, "coverage": 0.33, "partial": false,
	"summary": {
		"episodes": 2, "firing_seconds": 2700, "breaching_evaluations": 7,
		"notifications": 4, "no_data_evaluations": 0, "errors": 0
	},
	"series": [{ "t": "2026-11-14T10:15:00Z", "status": "ok", "value": 12, "firing_groups": 0 }],
	"groups": [{ "group": { "service": "checkout" }, "episodes": 2, "breaching_evaluations": 5, "max_value": 912 }],
	"groups_omitted": 0
}
```

`notifications` counts what the rule would have sent: one per episode, plus one per resolve when `notify_on_resolve` is set. `groups` holds the 20 groups with the most breaching evaluations. `partial` is true when the deadline cut the run short. `series` then ends early.

### New-pattern rules (phase 2)

A rule with `rule_type = 'new_pattern'` fires when a log template appears in the current window that the baseline window did not have. It builds on the Patterns and Compare RFC, called RFC 0004 here. That RFC is in review as pull request 194 under the number 0002.

The rule stores a LogchefQL filter and a baseline kind in `pattern_json`. The current window is the rule's lookback. `threshold_value` is the minimum count, and `threshold_operator` is fixed to `gte`. `no_data_policy` is fixed to `zero` and `group_by` is empty.

Each new template is one sample, so the per-group machinery does the rest:

- `GroupKey` is the SHA-256 of the template text.
- `Group` is `{"pattern": "connection to <IP> timed out after <*>"}`.
- `Value` is the template's count in the current window.

Every sample fires. A template resolves when it leaves the new set, which happens when it shows up in the baseline or stops occurring. For that reason `notify_on_resolve` defaults to false for this rule type. A rule holds at most 20 pattern instances, ranked by count, and the notification states the total number of new templates.

**An empty baseline.** RFC 0004 calls a template `new` only when the baseline window has matching rows. With an empty baseline its status is `baseline_empty`. This RFC keeps that rule. When the baseline has no matching rows, the evaluation produces no samples, so nothing fires and open pattern instances resolve. The tick writes no history row, because a row per tick would fill the history. The test route and the rule detail show "The baseline window has no matching logs". Without the rule, a source that started an hour ago, or a baseline past retention, would page on every template it has.

**The query.** RFC 0004 plans this alert as one count. This RFC needs the ranked templates and a total, on both backends. Unit P0 adds that call to `datasource.Service`, on top of the `GroupCounter` method of RFC 0004:

```go
type NewPatternsRequest struct {
	Filter   AnalysisFilter
	Current  TimeRange
	Baseline TimeRange
	MinCount int64
	Limit    int // at most 20
}

type NewPatternsResult struct {
	Templates     []NewPattern // template, count, sample, ordered by count
	Total         int64        // new templates with count >= MinCount, counted before Limit
	BaselineTotal int64        // matching rows in the baseline window
}

func (s *Service) NewPatterns(ctx context.Context, id models.SourceID, req NewPatternsRequest) (*NewPatternsResult, error)
```

Validation requires that the source has a message field and the `analysis` capability, both defined by RFC 0004.

### Triage on fire (phase 2)

When a rule has `triage_enabled` and a group changes to `firing`, the handler attaches a triage block before it commits:

```go
type Triage struct {
	Window      TimeRange
	Patterns    []TriagePattern // top 5 templates in the window, with count and share
	Changes     []TriageChange  // top 5 share shifts against the previous window
	Samples     []TriageSample  // 5 newest rows, message cut to 500 characters
	Error       string          // set when triage was cut short
}
```

Triage needs a LogchefQL filter. A condition-mode rule has one in `condition_json`, which holds a LogchefQL string despite its name (`frontend/src/composables/useAlertForm.ts:240`). A new-pattern rule has one in `pattern_json`. For a grouped rule, the handler adds one equality clause per group value. A native SQL or LogsQL rule has no filter, so it gets no triage, and the form says why next to the disabled switch.

The handler calls the `Patterns` and `ComparePeriods` methods of `datasource.Service` from RFC 0004 and one `QueryLogs` call for the samples. It triages at most three groups per tick, those with the highest value. The whole step has 15 seconds. When the time runs out, the notification goes without the missing parts and `Error` says which. A page is never held back for context.

The block is stored in the history payload under `triage`, at most 32 KiB. Email and Slack show three patterns, three changes and three samples. Webhook version 2 carries the whole block in `triage`. PagerDuty carries it in `custom_details.triage`. Webhook version 1 carries nothing.

Sample rows are log data that leaves Logchef. See "Access and security".

### API

All routes are under `/api/v1` and use the existing response envelope.

| Route | Scope | Change |
|---|---|---|
| `GET /alerts`, `GET /alerts/:alertID` | `alerts:read` | The alert gains the new fields and `instance_counts` |
| `POST /alerts`, `PUT /alerts/:alertID` | `alerts:write` | Accept `group_by`, `no_data_policy`, `notify_on_resolve`, `destination_ids`, and in phase 2 `rule_type`, `pattern`, `triage_enabled` |
| `POST /alerts/test` | `alerts:write` | Accepts `group_by` and `no_data_policy`. The response gains `samples` (at most 20), `groups_total` and `no_data`. |
| `POST /alerts/backtest` | `alerts:write` | New. The test body plus `source_id`, `frequency_seconds` and `range_seconds` (3,600 to 604,800). |
| `GET /alerts/:alertID/instances` | `alerts:read` | New. At most 200 rows, firing first. |
| `GET /alerts/:alertID/history` | `alerts:read` | Each entry gains `group_key`, `group` and `deliveries` |
| `POST /alerts/:alertID/resolve` | `alerts:write` | The body gains an optional `group_key` |
| `GET /alert-silences?source_id=` | `alerts:read` | New. Lists silences of one source, with `active` computed. |
| `POST /alert-silences`, `PUT /alert-silences/:id`, `DELETE /alert-silences/:id` | `alerts:write` | New |
| `GET /notification-destinations` | `destinations:read` | New. Lists the destinations the caller can use. |
| `POST /notification-destinations`, `PUT /:id`, `DELETE /:id` | `destinations:write` | New. A blank `secret` on update keeps the stored one. |
| `POST /notification-destinations/test` | `destinations:write` | New. Body is `{"id": 5}` or an unsaved definition. Sends a `test` message now and returns the result. |
| `POST /admin/settings/test-webhook` | | Deleted (`internal/server/server.go:405`). The destination test replaces it. |

The alert routes stay behind `requireAlertsEnabled` (`internal/server/server.go:511`). The destination routes do not, because RFC 0008 expands email destinations into report recipients when alerting is off.

Two token scopes are new: `destinations:read` and `destinations:write`, beside the alert scopes in `pkg/models/auth.go:66`.

| Status | When |
|---|---|
| 400 | A validation failure. The message names the field. Also a legacy target key in an alert request, even with an empty value. |
| 403 | The caller cannot see the source, cannot edit the object, or cannot use a destination it names |
| 404 | The object does not exist, or the caller cannot see its source |
| 409 | Delete of a destination that a rule uses. The message gives the count. |
| 422 | A backtest that cannot run, with the reason |
| 429 | The backtest admission limit |
| 503 | Alerting is disabled |

### Client surfaces

**Web UI.**

- `AlertQuerySection.vue` gets a **Group by** field that takes result column names, and a **Backtest** panel under the test result. The panel has a range select (1 hour, 6 hours, 24 hours, 7 days), the summary line, and a state strip drawn with the chart components in `frontend/src/components/visualizations`.
- `AlertScheduleSection.vue` gets **When the query returns no data** and **Notify when resolved**.
- `AlertNotificationSection.vue` is replaced by a destination picker. It lists the destinations the user can use, and **New destination** opens the destination form in a dialog, so adding a webhook stays one step.
- `AlertsOverview.vue` shows "3 firing" or "no data" in the state column.
- `AlertDetail.vue` gets an **Instances** table with group, state, value, since and a silenced badge, and row actions **Silence** and **Resolve**. History rows show the group and one delivery line per destination, or per recipient for email. `AlertDeliveryStatus.vue` reads `deliveries`.
- Two new views, `views/alerts/AlertSilences.vue` and `views/alerts/NotificationDestinations.vue`, sit beside the rule list as tabs. Both use `PageHeader`, `SearchInput`, `useTableSearchSort`, `SortHeader`, `RowActionsMenu`, `ConfirmDialog`, `EmptyState` and `LoadingState`. Their routes go before `alerts/:alertID` in `frontend/src/router/index.ts:94`.
- The webhook test leaves `AdminSettings.vue`.
- New clients `frontend/src/api/silences.ts` and `frontend/src/api/notificationDestinations.ts`, with Pinia stores. New strings go into every locale catalog.

**CLI.** Not affected. The CLI requests `alerts:read` (`cli/crates/logchef-core/src/auth/mod.rs:29`) but has no alert command.

**MCP.** `list_alerts` and `get_alert_history` return the API JSON as text (`internal/mcp/investigate.go:191`), so they carry the new fields with no code change. Their descriptions and the `investigate_alert` prompt (`internal/mcp/prompts.go:102`) are updated to mention groups, `no_data` and silences. No write tool is added.

**Ask Chef.** `writeContext` (`internal/askchef/prompt.go:101`) adds the entry's group and, in phase 2, a short form of its triage block. Each notification links to the alert detail with `history=<id>`, where the existing **Ask Chef** action on a history entry (`frontend/src/views/alerts/AlertDetail.vue:142`) starts the investigation.

**Provisioning TOML.** Not affected.

**Docs.** Rewrite `docs/src/content/docs/features/alerting.mdx`: grouping, no data, silences, backtest, destinations, the two webhook versions and the upgrade path. Remove "Run exactly one alert-manager replica" from `docs/src/content/docs/operations/database-backends.md:65` and describe the runner. Add the metrics to `docs/src/content/docs/operations/metrics.mdx`. Add `[jobs]` to `config.toml` and to `docs/src/content/docs/getting-started/configuration.md`, and remove `evaluation_interval` from both.

**Metrics.** Registered the way `internal/metrics/metrics.go:92` does.

| Metric | Labels |
|---|---|
| `logchef_jobs_runs_total` | `kind`, `result` (`ok`, `error`, `skipped`, `lost`, `deferred`) |
| `logchef_jobs_run_duration_seconds` | `kind` |
| `logchef_jobs_schedule_lag_seconds` | `kind` |
| `logchef_alert_evaluations_total` | `result` (`ok`, `error`, `superseded`, `lease_lost`) |
| `logchef_alert_transitions_total` | `to` (`firing`, `ok`, `no_data`) |
| `logchef_alert_notifications_silenced_total` | |
| `logchef_notification_deliveries_total` | `type`, `result` (`sent`, `retry`, `failed`, `unknown`, `reservation_lost`) |
| `logchef_notification_deliveries_pending` | |
| `logchef_alert_backtests_total` | `result` |
| `logchef_alert_triage_total` | `result` (`ok`, `partial`, `error`) |

No metric has a per-rule or per-group label.

## Access and security

**Rules.** Unchanged. A user sees a rule when one of their teams has the source (`internal/core/visibility.go:46`). The creator and global admins edit it (`internal/core/alerts.go:420`). Backtest follows the test endpoint: `alerts:write`, source visibility through a team, and no bypass for global admins (`internal/core/visibility.go:82`).

**Instances and history.** Readable by anyone who can read the rule.

**Silences.**

| Action | Who |
|---|---|
| List | Anyone who can see the source |
| Create | Anyone who can see the source, with `alerts:write` |
| Edit, delete | The creator of the silence, and global admins |

An on-call engineer must be able to mute a rule they did not write. Every silence is an audit event and shows its author in the list. See open question 2.

**Destinations.**

| Scope | Use in a rule | Create | Edit, delete |
|---|---|---|---|
| `private` | The creator | Any user | The creator, global admins |
| `team` | Members of the team | Team owner, admin or editor (`pkg/models/auth.go:81`) | The creator, team owners and admins, global admins |
| `global` | Everyone | Global admins | Global admins |

A rule update that adds a destination checks that the editor can use it. Destinations already on the rule stay when someone else edits it. A raw address in an `email` destination needs a global admin. Other users pick Logchef users, as today. See open question 3.

**Secrets.** `secret` never leaves the server. The API returns `has_secret` and `secret_hint`. Delivery errors pass through `boundedDeliveryError` (`internal/alerts/delivery_error.go:11`), and transport errors keep the sanitized form that hides the URL (`internal/alerts/webhook_sender.go:255`). Senders never log a secret or a request body. Audit events for destinations carry the name and type only.

**Outbound requests.** Covered under "Outbound requests" above. The only hosts a user can choose are webhook hosts, and the dial control checks the resolved address, so a DNS answer that changes cannot reach a private network.

**Restricted sources.** RFC 0002 makes a restricted view its own source and lets the log database enforce the row filter (`rfcs/0002-restricted-source-access.md:50` lists the alert paths). Every query in this RFC, evaluation, backtest, triage and new-pattern counting, runs through `datasource.Service` as that source's connection user. None of them names another source, so none can read past the filter.

**Log data in notifications.** Triage puts templates and sample rows into Slack, email and webhook bodies. People who read that channel may have no access to the source. Three controls apply. `triage_enabled` is off by default and is set per rule by someone who can see the source. The system setting `alerts.triage_enabled` lets a global admin turn the feature off for the deployment. Each sample message is cut to 500 characters, and a rule sends at most five.

## Limits and failure modes

| Bound | Value | What the user sees when it is hit |
|---|---|---|
| Group columns per rule | 4 | 400 on save |
| Groups per evaluation | 100 | An `error` history row: "The query returned more than 100 groups" |
| Tracked groups per rule | 200 | An `error` history row |
| Evaluation time | 90 seconds (`internal/alerts/manager.go:26`) | An `error` history row |
| Concurrent evaluations | 4 per replica | Later rules start late. `logchef_jobs_schedule_lag_seconds` rises. |
| Rule frequency | At least 10 seconds | 400 on save |
| Closed history rows | 50 per rule, plus every open one | Older rows are deleted |
| Silences | 100 per source, 5 matchers each, `once` at most 30 days, a weekly window at most 24 hours | 400 on save |
| Destinations per rule | 10 | 400 on save |
| Destinations per team | 100 | 400 on create |
| Recipients per email destination | 20, users and addresses together | 400 on save |
| Delivery reservation | 2 minutes, with a 60-second deadline per send | A slower send is taken over and may arrive twice |
| Silent group | Forgotten after 24 hours without a row | Its episode resolves with the reason |
| Delivery message | 256 KiB | Not sent. `Enqueue` writes the row as `failed` with the size in its error, so the evaluation still commits. |
| Delivery attempts | 5 over about 43 minutes | The history row shows `failed` with the last error |
| Send timeout | `alerts.request_timeout`, default 5 seconds | Counts as a transient failure |
| Drain batch | 50 rows, 4 at once | Deliveries wait for the next run, at most 10 seconds |
| Delivery rows kept | 30 days | Older history shows no delivery detail |
| Backtest | 120 evaluations, 60 seconds, 1 per user and 2 in total, range at most 7 days | `partial: true`, or 429 "A backtest is already running" |
| Triage | 15 seconds, 3 groups, 5 patterns, 5 changes, 5 samples, 32 KiB | The notification goes without the missing parts |
| New-pattern instances | 20 per rule | The notification states the total |
| Job error text | 512 bytes | Cut, marked `[truncated]` |

Backtest registers in the query tracker under a new class next to the existing ones (`internal/server/logs_handlers.go:34`). Scheduled evaluation has no user and stays outside the tracker, as it is today.

**Failure modes.**

| Failure | Behavior |
|---|---|
| The log backend is down | Each evaluation records an `error` row. Instance states do not change, so nothing resolves by mistake. |
| The metadata database is down | The runner logs and polls again. No job is lost, because the schedule is stored. |
| A process dies during an evaluation | Nothing was committed. The lease ends after `Timeout + 30 seconds` and another run evaluates. |
| A process dies after the commit | The delivery rows exist. The drain sends them. |
| A destination is down | Retries, then `failed`. Evaluation is not slowed, because sending is a separate job. |
| A destination is deleted or made inactive | Its pending rows become `failed`. Rules that used it show a warning in the list. |
| Logchef was down for an hour | Each rule evaluates once on start, over its normal lookback ending at its latest due tick. Data older than that window is never evaluated. |
| A process stalls past its lease while it evaluates | Another replica claims the tick. The stalled run's `Hold` fails and it writes nothing. |
| A process stalls or dies between a send and its status write | A webhook, Slack or PagerDuty row is reserved again and sent again. An email row is marked `unknown` and is not sent again. |
| A rule has no destination | It evaluates and writes history. The list shows "No destination". |

**Several replicas on Postgres.** Every replica runs the runner and claims jobs. A rule runs on whichever replica claims its tick, so evaluation load spreads. The drain job normally runs on one replica at a time, and row reservations keep two overlapping drains from sending the same row. Silences, instances and destinations are rows, so every replica sees a change at its next read.

What the design promises with several replicas, in order of strength:

- Rule state, history and delivery rows are written by the current claim only. A stalled run cannot change them.
- A delivery row is sent by one drain at a time while its reservation lasts.
- A webhook, Slack or PagerDuty message can still arrive twice, in the two cases under "Delivery is at least once, except for email". An email cannot. A stall during an evaluation does not cause that. A stall or a crash during a send does, and it can repeat every row that the process had reserved, at most 50.

## Backend differences

| Feature | ClickHouse | VictoriaLogs | Loki, if RFC 0011 lands |
|---|---|---|---|
| Ungrouped rule | SQL, first column of first row | LogsQL through `stats_query` | Needs a metric query. RFC 0011 decides. |
| Grouped rule, native | `GROUP BY` in the query, columns named in `group_by` | `stats by (...)`, labels named in `group_by` | A LogQL `sum by (...)`, if RFC 0011 exposes labels as columns |
| Grouped rule, LogchefQL | RFC 0005 | RFC 0005 | Not available. RFC 0011 rejects LogchefQL aggregations in its first version. |
| No-data policy | Portable | Portable | Portable |
| Silences, destinations, runner | Portable | Portable | Portable |
| Backtest by replay | Needs the time macros in the query, or a LogchefQL rule | Always | RFC 0011 passes `At` as the `time` parameter of the instant query |
| New-pattern rules, triage | RFC 0004, needs a message field | RFC 0004 | Not in RFC 0004 |

Nothing here adds a capability flag. A grouped rule is valid wherever the query returns the named columns, and the save-time test proves that. New-pattern rules and triage check the `analysis` capability of RFC 0004. The LogchefQL group editor checks the aggregation capability of RFC 0005.

## Seams

**Requires from RFC 0005 (LogchefQL aggregations).** RFC 0005 offers `core.CompileLogchefQL` with `AllowAggregation`, which returns the native query and an `AggregationInfo`. An alert condition needs this from it:

- Two shapes. `scalar` with one value for an ungrouped rule, and `grouped` with one value per group for a grouped rule. Any other shape is a 400 on save.
- The functions the condition editor has today: `count`, `sum`, `avg`, `min` and `max`. At most four group keys, JSON paths included.
- `AggregationInfo` names the value column and the group columns in order. The evaluator copies the group columns into `group_by`, so the sample step is the same for native and compiled rules.
- The compile call takes the window as a start and an end. The evaluator passes `At - lookback` and `At` at each evaluation, which is what makes a LogchefQL rule replayable.
- A grouped result that the backend cut must be detectable. This RFC decides the rule: a cut result is an evaluation error, never a partial state update.
- The `time` key is rejected in a stored alert condition. An alert has one window per evaluation.
- One LogchefQL aggregation compiles to one backend query, and a provider that cannot run a shape rejects it with a typed error. The save-time test surfaces that error as a 400.
- The capability `logchefql_aggregation` gates the LogchefQL group editor.

This RFC asks RFC 0005 for no partial-aggregate mode. Backtest is by replay only.

**Before RFC 0005 lands.** A grouped rule works in native mode on both backends from unit A2: the user writes `GROUP BY` or `stats by (...)` and names the columns in `group_by`. Condition mode stays ungrouped, with a hint under the disabled **Group by** field. The frontend keeps generating the condition query until unit Q1 moves that to the server and stores the rule as `query_language = 'logchefql'`.

**Requires from RFC 0004 (Patterns and Compare).** That RFC plans the alert as one count read through `ExtractFirstNumeric`. This RFC needs more, because each new template is its own instance. It does not ask RFC 0004 to build the difference. Unit P0 of this RFC builds `Service.NewPatterns` on both providers. From RFC 0004 it needs:

- The `GroupCounter` provider method with a baseline window, and the rule that a template is `new` only when the baseline has matching rows. P0 extends the request with a minimum current count and a "new only" filter that the database applies before the limit, and returns the total.
- `Service.Patterns` and `Service.ComparePeriods` callable without a user, as that RFC already states for alerts.
- The helper that turns a field and a value into a LogchefQL equality clause, which its click filter needs as well.
- The source's `message_field` and the `analysis` capability.

**Provides to RFC 0008 (scheduled reports) and RFC 0010 (schema advisor).** The runner as described: `Register`, `Upsert` and `Delete` inside the caller's transaction, `KindSpec.Timeout` and `Concurrency`, `Schedule.Next(after)` for "next run" in a UI, and a stable `Job.ID` with a context, which RFC 0009 asks for. `Job.At` is the selected due occurrence, the latest one due, and never the claim time. A handler that writes state calls `jobs.Hold` in its transaction. RFC 0008's missed-tick policy is `skip` with `grace_seconds = 3600`, applied to that occurrence. RFC 0010's weekly refresh is `cron` with `skip` and no grace.

**Provides to RFC 0008, delivery.** `Mailer.Send(ctx, to, MailMessage)` sends one message with subject, text, HTML and attachments to one address through the instance SMTP settings. It never retries. It returns `ErrOutcomeUnknown` for a failure after the end of `DATA`. `ExpandEmailDestination` turns an email destination into its recipients, with Logchef users kept as user ids, so that a report can authorize panels per user. Report delivery owns its retries, its backoff and its records. Reports do not use the delivery queue in their first version, and Slack, PagerDuty and webhook destinations are not report targets yet.

**Provides to RFC 0011 (Loki).** `AlertQueryRequest.At` is the instant to evaluate. `lookback_seconds` stays a required field of every rule, because backtest derives its step and coverage from it. A Loki native LogQL rule stores it, and the provider may ignore it at evaluation.

**Requires from RFC 0009 (audit).** The events below go through its contract. Units D1, S1 and A2 depend on its units P2 (the audit package) and P3 (route coverage), and add to its unit P6, which covers alerts.

**Seam notes.**

- *RFC 0008 states daylight-saving rules for a cron schedule.* A removed local time fires once at the next valid instant, and a repeated local time fires once. Unit J3 must prove both for the parser it picks, or wrap the parser in an adapter that gives them. See "Unverified claims".
- *The email destination has two consumers with different retry rules.* Alerts retry through the queue. Reports retry themselves. Both sit on the one `Mailer`, so the SMTP rule about `DATA` is written once.

## Audit events

Every event goes through the contract of RFC 0009: a route name on each mutating route, an action constant in `internal/audit/actions.go`, a view function in `internal/audit/views.go`, and one staging line in the handler. No handler calls the recorder directly. The jobs in this RFC change no configuration, so they record nothing.

| Event | Actor | Target |
|---|---|---|
| `alert.create`, `alert.update`, `alert.delete` | User | Alert id, source id |
| `alert.resolve` | User | Alert id, group key |
| `alert_silence.create`, `alert_silence.update`, `alert_silence.delete` | User | Silence id, source id, alert id when set |
| `notification_destination.create`, `.update`, `.delete` | User | Destination id, with name, type and scope |
| `notification_destination.test` | User | Destination id, or the type for an unsaved one |

The views follow two rules of RFC 0009. The destination view marks `secret` with `Secret`, and it marks the addresses of an email destination the same way. The alert view records the query language, `eval_seq` as the revision and a SHA-256 digest of the query text. It never records the query, the LogchefQL filter or `pattern_json`.

`POST /alerts/test` and `POST /alerts/backtest` change nothing. Each gets an entry in `auditExemptRoutes` with the reason "read-only query".

State changes of a rule (`firing`, `resolved`, `no_data`) are not audit events. Alert history records them, and RFC 0009 excludes scheduler run records.

## Alternatives rejected

| Alternative | Reason |
|---|---|
| Leader election, with one leader that runs every job | A failover needs a second mechanism, and one replica carries all the load. A claim per job gives the same guarantee, spreads the work and uses one SQL statement that also works on SQLite. |
| Postgres advisory locks for the claim | SQLite has none, so the two stores would need two designs. A lock also ends with the connection, which a pooled client does not control well. |
| A claim on the `alerts` row, with no `jobs` table | It solves alerts only. RFC 0008 and RFC 0010 would each build a second scheduler. |
| A job queue library | The repo has no such dependency, and the need is one table and three statements. |
| Derive instance state from open history rows | It works for firing groups, as today. The `no_data` and `keep` policies must remember `ok` groups, which have no history row. |
| One rule per group, generated from a template | The user manages N rules, and a new service gets no rule until someone adds it. |
| Keep targets inline on the rule and add destinations beside them | Two ways to say where a notification goes, forever. The SQL migration is a few statements. |
| Send inline and keep retry state in the history payload | A slow receiver delays every later rule, and a crash between the state change and the send loses the notification. The queue commits with the state. |
| One webhook payload, with new fields added to the old one | A grouped rule cannot fit a single `value`, and a strict receiver breaks on a new `status`. Two versions let each receiver move when it chooses. |
| A separate "maintenance window" object | It has the same fields as a silence plus a weekly schedule. |
| Stop evaluating a silenced rule | The state at the end of the silence would be unknown, and history would have a hole exactly where a deploy happened. |
| Fence only the job completion with the lease token | The first draft did this. A stalled run could still commit its older state before the newer run, which then lost the `eval_seq` race. `Hold` inside the write transaction closes that. |
| Let the drain job's lease decide who sends a row | The runner allows two handlers after a stall, so two drains would send the same 50 rows. Rows need their own reservation. |
| One delivery row per email destination | A retry after a partial SMTP failure would mail the recipients that already succeeded. |
| An expand and contract migration, so that replicas can upgrade one by one | The old binary evaluates without instances, so the two versions would disagree on state even if the old columns stayed. A short stop is cheaper than a second release. |
| Backtest with one bucketed query | Buckets of the rule's frequency cannot rebuild a lookback that cuts a bucket, such as 90 seconds over 60-second buckets, and an exact average needs partial aggregates that RFC 0005 does not compile. It can return later for `count` and `sum` when the lookback is a multiple of the frequency. |
| Backtest only for structured rules | It would not exist until RFC 0005. Replay works today on VictoriaLogs and on any ClickHouse query that uses the macros. |
| Backtest by replaying every tick | A day of one-minute ticks is 1,440 queries. The cap of 120 with a stated coverage is honest about what it checked. |
| New-pattern rule as one count | The notification could not say which template is new, and that is the whole message. |
| Send the alert first and the triage as a follow-up | Only a Slack bot token or an email thread could attach it to the first message. A 15-second budget keeps one message. |

## Implementation plan

Phase 1 (units A, J, S, D and X) ships items 1 to 6 with no dependency on RFC 0004 or RFC 0005. Phase 2 (units P) waits for RFC 0004. Unit Q1 waits for RFC 0005.

Audit events need units P2 and P3 of RFC 0009. A unit that adds a mutating route adds its action, view and staging line when those exist. If RFC 0009 lands later, its unit P6 adds them for the routes that this RFC already shipped.

Every unit that touches a store adds the migration and the queries to both stores, runs `just sqlc-generate`, and adds a conformance test in `internal/store/storetest`. Every unit ends with `just check`. Frontend units also run `bun run typecheck` and `bun run test`. Each unit names only its extra check below.

Four tracks can run in parallel after A1: A (A2 to A5), J (J1, then J2), S (S1, S2) and D (D1 to D6). A1 goes first because it splits `internal/alerts/manager.go` into files that the tracks then edit separately.

**A1. Instances and the pure transition.** Depends on nothing.

- Creates `internal/alerts/transition.go` (`Sample`, `Instance`, `Change`, `Transition`), `internal/alerts/evaluate.go` (the five steps), `internal/alerts/messages.go` (builds notifications from changes), migration `add_alert_instances`, `internal/store/storetest/alert_instances.go`.
- Edits `internal/alerts/manager.go` (keeps only `Start`, `Stop` and manual resolve), `pkg/models/alerts.go`, both `queries.sql`, `internal/store/store.go`.
- Change: ungrouped rules run through `Transition` with `eval_seq`. No behavior changes. History pruning keeps every open episode.
- Check: a table test of `Transition` covering each row of the policy table for `zero`. The existing `internal/alerts/manager_test.go` and `delivery_regression_test.go` pass unchanged. An upgrade test in `internal/store/sqlite/upgrade_test.go`, and its Postgres twin, that runs the real migration runner over four fixtures (delivered, retry pending, interrupted, two open rows) plus closed history. It asserts that every history row survives the rebuild with its id, that each firing rule has one instance with the backfill values, and that the first tick after upgrade sends no `firing` message.

**A2. Group by in native mode.** Depends on A1. First unit a user can use.

- Edits `internal/core/alerts.go` (validate `group_by`), `internal/server/alert_handlers.go` (`GET /alerts/:alertID/instances`, `group_key` on resolve, `samples` in the test response), `internal/server/server.go`, `frontend/src/api/alerts.ts`, `frontend/src/composables/useAlertForm.ts`, `AlertQuerySection.vue`, `AlertDetail.vue`, `AlertsOverview.vue`, the email template, locale catalogs.
- Change: samples from grouped rows, the instance table, per-group history, one webhook request per group in the existing payload.
- Check: `Transition` tests for two groups that fire and resolve at different ticks, a duplicate group, a missing column and a truncated result. UI scenario on both dev backends: a rule grouped by `service` shows two firing rows, one resolves, the other stays.

**A3. No-data policy.** Depends on A2.

- Migration `add_alert_rule_options` also carries `notify_on_resolve`. Edits `transition.go`, `AlertScheduleSection.vue`, `AlertForm.vue`, the senders for the `no_data` event.
- Check: `Transition` tests for every cell of the policy table, the 200-group cap, and the forget rule in each of its three states, including a `keep` group that is forgotten while firing and sends its `resolved`. UI scenario: a rule with policy `no_data` on a filter that matches nothing shows "No data" and sends one notification.

**A4. Time macros and `At`.** Depends on A1. Parallel with A2 and A3.

- Edits `internal/datasource/query.go` (`At`), `internal/datasource/clickhouse_provider.go` (`renderAlertQuery`, used by `EvaluateAlert`), `internal/datasource/service.go` (`ValidateAlertQuery` takes the source and calls `renderAlertQuery`), `internal/core/alerts.go` (pass the source to validation), `internal/victorialogs/querying.go` (`time` from `At`), `frontend/src/composables/useAlertForm.ts` (generate `{{__time_filter}}`).
- Check: a provider test that a query with `{{__time_filter}}` and `At` one hour ago reads only rows of that window, on dev ClickHouse. The same for VictoriaLogs with a native LogsQL query. A query without macros produces the same SQL as before. A core test that a `{{__time_filter}}` query passes create, update and the test route, that a query with a user variable is refused, and that a macro query whose expansion `formatAlertQuery` rejects is refused on save with the same error as at run time.

**A5. Backtest by replay.** Depends on A3 and A4.

- Creates `internal/alerts/backtest.go`, `frontend/src/components/alerts/AlertBacktest.vue`. Edits `internal/core/visibility.go` (`BacktestAlertForPrincipal`), `internal/server/alert_handlers.go`, `internal/server/logs_handlers.go` (the query class), `AlertQuerySection.vue`.
- Check: a test with a fake evaluator that returns a fixed series and asserts `episodes`, `firing_seconds`, `notifications` and `coverage`. A test that a ClickHouse query with `now()` returns 422. UI scenario: seed a burst of errors 30 minutes ago, backtest one hour, see one episode.

**J1. The job runner.** Depends on nothing.

- Creates `internal/jobs/runner.go`, `internal/jobs/schedule.go` (`Every`), `internal/jobs/jobs_test.go`, the `jobs` table without the alert backfill, `internal/store/storetest/jobs.go`. Edits `internal/config/config.go` (`[jobs]`), `config.toml`, `internal/metrics/metrics.go`.
- Change: the package and its store methods. Nothing calls it until J2, which is the one unit in this plan that lands without a production caller.
- Check: a conformance test in which two runners with different owners claim from one database, 200 times, and no job is handed to both. It runs against SQLite and, with `LOGCHEF_TEST_POSTGRES_DSN`, against Postgres. That test proves the first claim only, so four more tests cover the rest. Takeover: run A holds a job past its lease, run B claims it, then A calls `Hold` and gets `ErrLeaseLost`, in both orders of A's and B's commit. On Postgres, a claim that starts while A is inside a transaction after `Hold` does not get the job until A commits. Time: after a gap of 100 intervals, `At` is the latest due occurrence, the handler runs once, and the next run is the occurrence after `At`. Shutdown: `Stop` clears the lease of a handler that returned and keeps the lease of one that did not. Also `ErrGone`. Run with `-race`.

**J2. Alert evaluation on the runner.** Depends on A1 and J1.

- Edits `internal/alerts/manager.go` (delete the ticker loop), `internal/app/app.go` (`internal/app/app.go:232` starts the runner), `internal/core/alerts.go` (`jobs.Upsert` and `jobs.Delete` in the write transactions), `internal/config/config.go` and `internal/config/runtime.go` (delete `evaluation_interval`, add `max_concurrent_evaluations`), `docs/src/content/docs/operations/database-backends.md`. Migration `add_jobs` backfill.
- Check: an integration test with two app instances on one Postgres database and one rule at a 10-second frequency, run for one minute, that counts exactly one history transition and one notification. A takeover test at the evaluator: a stalled evaluation that reaches its commit after a second run has claimed the tick writes no instance, no history row and no delivery row. `internal/app/shutdown_test.go` passes with the runner.

**J3. Cron schedules and the `skip` policy.** Depends on J1. Built when RFC 0008 starts. Needs the answer to open question 5.

- Edits `internal/jobs/schedule.go`, `go.mod`.
- Check: table tests in `Europe/Berlin` that a local time removed by the spring change fires once at the next valid instant and that a local time repeated by the autumn change fires once. If the parser fails either, the unit adds an adapter that passes both. A downtime test: a per-minute cron job whose `next_run_at` is 60 days old, more than 50,000 occurrences behind, is claimed once, runs once with `At` equal to the latest due minute, and walks fewer than 10,081 occurrences. A cap test: with the cap lowered below the distance, the runner defers the claim, logs, leaves `next_run_at` unchanged and never calls the handler. Tests for `skip`: the latest due occurrence inside the grace runs with `At` set to it, and one outside the grace is skipped, when several older occurrences were missed.

**S1. Silences, backend.** Depends on A2.

- Creates `pkg/models/silences.go`, `internal/alerts/silence.go` (`ActiveAt`, `Matches`), `internal/core/silences.go`, `internal/server/silence_handlers.go`, migration `add_alert_silences`. Edits `evaluate.go`, `internal/server/server.go`.
- Check: table tests for `ActiveAt` (a `once` window, a weekly window that crosses midnight, a timezone other than UTC) and for the two edge rules: a notification is sent when a silence ends, and none is sent for a group that fired and resolved inside one. A handler test for the access table.

**S2. Silences, UI.** Depends on S1.

- Creates `frontend/src/api/silences.ts`, `frontend/src/stores/silences.ts`, `views/alerts/AlertSilences.vue`, `components/alerts/SilenceForm.vue`. Edits the router, `AlertDetail.vue` (the **Silence** row action).
- Check: UI scenario. Silence one group for 10 minutes from the instance table, see the badge, see no notification, delete the silence, see the notification at the next tick.

**D1. Destinations and the move of existing targets.** Depends on A1.

- Creates `pkg/models/notifications.go`, `internal/notify/` (`destination.go`, the email and webhook senders moved from `internal/alerts`, `Mailer`), `internal/core/destinations.go`, `internal/server/destination_handlers.go`, migration `add_notification_destinations`, `frontend/src/api/notificationDestinations.ts`, `components/alerts/DestinationPicker.vue`, `components/alerts/DestinationForm.vue`. Edits `messages.go`, `pkg/models/auth.go` and `internal/mcp/server.go` (scopes), `AlertNotificationSection.vue`, `useAlertForm.ts`. Deletes `handleTestWebhook` and its route and its use in `AdminSettings.vue`.
- Change: rules reference destinations. Sending still happens inline with the existing retry bookkeeping, now keyed by destination id.
- Check: a migration test in both stores that loads a fixture with three rules (shared URL, recipients, null creator) and asserts the destinations and links. A golden test that the version 1 payload of a migrated rule is byte-equal to the payload before the change. Handler tests for the access table and for `secret` never appearing in a response.

**D2. The delivery queue.** Depends on D1 and J1.

- Creates `internal/notify/queue.go` (`Enqueue` with the per-recipient expansion, the reservation and result statements, the drain handler, retry classification), migration `add_notification_deliveries`. Edits `evaluate.go`, the email sender (one recipient per call, `ErrOutcomeUnknown`), the webhook sender (the `Logchef-Delivery-Id` header), the history handler (`deliveries`), `AlertDeliveryStatus.vue`. Deletes `internal/alerts/delivery.go`, `RecoverAlertDeliveries` and `InterruptAlertDeliveries` in both stores.
- Check: tests with an `httptest` receiver. A 500 then a 200 gives `sent` after one retry. A 400 gives `failed` at once. A second `Enqueue` with the same `dedup_key` adds nothing. A test that kills the handler after commit and asserts the next drain sends. Takeover: two drains run against one batch, and each row reaches the receiver once. A drain whose reservation expired writes its result and changes nothing, and the newer drain's retry state stands. Ordering: with two drains, a `resolved` row is never reserved while its `firing` row is pending. Email: with a fake SMTP server that accepts recipient A and refuses B, the retry mails B only. An error after `DATA` gives `unknown` and no retry. Crash recovery: the fake server accepts `DATA`, the worker is killed before its status write, the reservation ends, and the next drain marks the row `unknown` while the server sees exactly one message. The same test with a webhook row shows a second request, which is the documented difference.

**D3. Destinations page.** Depends on D1. Parallel with D2.

- Creates `views/alerts/NotificationDestinations.vue`, `frontend/src/stores/notificationDestinations.ts`. Edits the router.
- Check: UI scenario. Create a team webhook destination, test it against a local receiver, attach it to two rules, see that delete is refused with the count.

**D4. Slack sender.** Depends on D2. Parallel with D5 and D6.

- Creates `internal/notify/slack.go` and its test. Edits `DestinationForm.vue`.
- Check: a golden test of the body for a firing message of one group. Tests for `ok`, for `no_service` as permanent and for 429 with `Retry-After`. A test that a URL on another host is refused on save.

**D5. PagerDuty sender.** Depends on D2.

- Creates `internal/notify/pagerduty.go` and its test.
- Check: a golden test of `trigger` and `resolve` with the same `dedup_key` for one group and different keys for two groups. 202 is success, 400 is permanent, 429 retries.

**D6. Webhook version 2.** Depends on D2.

- Edits `internal/notify/webhook.go`, `DestinationForm.vue` (the version selector).
- Check: a golden test of the version 2 body. A test that a version 1 destination on a `no_data` episode sends `status = "triggered"`.

**X1. Docs and upgrade notes.** Depends on J2, A5, S2, D6.

- Edits `docs/src/content/docs/features/alerting.mdx`, `docs/src/content/docs/operations/metrics.mdx`, `CHANGELOG.md`, the `investigate_alert` prompt and tool descriptions in `internal/mcp`.
- Check: the docs build passes, and every route and setting named in the page exists in `internal/server/server.go` and `config.toml`.

**P0. The new-template query.** Depends on RFC 0004, its unit that adds `GroupCounter` to both providers.

- Edits `internal/datasource/` (`Service.NewPatterns`, the request and result types, the "new only" and minimum-count options of the group count request) and both providers.
- Check: an integration test that seeds the same corpus into dev ClickHouse and VictoriaLogs and asserts the same templates, counts and `Total` on both. Cases: 25 new templates with a limit of 20 gives `Total = 25`. A template under the minimum count is left out. An empty baseline gives no templates and `BaselineTotal = 0`.

**P1. New-pattern rules.** Depends on A3 and P0.

- Migration `add_alert_patterns`. Edits `evaluate.go`, `internal/core/alerts.go`, `AlertForm.vue`.
- Check: an integration test on both dev backends that seeds a baseline hour, adds one new template in the current window and asserts one firing instance whose `group.pattern` is that template. A test that an empty baseline fires nothing and that the test route returns the warning.

**P2. Triage.** Depends on P1 and D6.

- Creates `internal/alerts/triage.go`. Edits `evaluate.go`, the four senders.
- Check: a test with a slow fake that asserts the notification leaves after 15 seconds with `Error` set. Golden tests of the Slack and email bodies with triage.

**P3. Ask Chef context.** Depends on P2.

- Edits `internal/askchef/prompt.go`, `AlertDetail.vue` (open the entry named by `history`).
- Check: a prompt test that includes the group and the triage summary.

**Q1. LogchefQL conditions compiled on the server.** Depends on A2 and RFC 0005.

- Edits `evaluate.go` (compile at each evaluation), `internal/core/alerts.go`, `useAlertForm.ts` (delete the query generator), `AlertQuerySection.vue` (enable **Group by** in condition mode).
- Check: a grouped LogchefQL rule fires per group on both dev backends, an existing condition-mode rule returns the same value before and after, and a backtest of a LogchefQL rule runs by replay.

## Changes after build

- **One message per group, for every destination type.** The design sent email, Slack and webhook version 2 one message per rule and tick that listed up to 20 groups. The build sends one message per group to every destination type, as webhook version 1 and PagerDuty already did, and Karan kept that. With one group per message, a silence holds or cancels a queued delivery for exactly the group it mutes, and history finds each delivery by its episode ref, `<alert_id>:<history_id>` (`DeliveryRef` in `internal/alerts/messages.go`). A message that listed several groups would belong to no single episode, and a silence on one of its groups would hold back the others. The cost is volume: a tick in which many groups change sends that many messages to a channel or an inbox. The 20-group list and the cut group list are gone. Webhook version 2 keeps the `instances` array, with one entry.
- **A message over 256 KiB is not sent.** With one group per message there is no group list to cut, and a cut would change the version 1 payload. `Enqueue` writes the row as `failed` with the size in its error, so the transaction that produced it still commits and history shows why (`internal/notify/queue.go`, #345).
- **Email is plain text.** Alert email has always been `text/plain` (`internal/notify/mailer.go`). The design's "existing HTML template" did not exist, and none was added.

## Open questions for Karan

1. **Legacy fields in the alert API.** Default: a request that contains the key `webhook_urls` or `recipient_user_ids`, with any value including an empty array, gets 400 naming `destination_ids`, with an upgrade note, as 2.7.0 did for dashboard saves. The alternative accepts them for one release and creates private destinations.
2. **Who may silence.** Default: anyone who can see the source and holds `alerts:write`. The alternative limits it to the rule's creator and global admins.
3. **Raw email addresses.** Default: only global admins may add an address that is not a Logchef user.
4. **Slack integration.** Default: incoming webhooks only. A bot token would add a channel picker and threaded resolves, and needs an app install.
5. **Cron parser.** Default: add `github.com/robfig/cron/v3` in unit J3, and use only its parser and `Schedule.Next`, on the condition that the J3 tests prove the daylight-saving behavior that RFC 0008 needs, or that a small adapter supplies it. The alternative is a hand-written parser for the daily, weekly and monthly forms that reports need.
6. **Where migrated destinations land.** Default: private to the rule's creator. The alternative makes them global, which lets every user attach another team's webhook.
7. **Triage default.** Default: off per rule, and the admin switch on.
8. **Forgetting a silent group.** The end state is now defined: a forgotten group resolves, closes its episode and sends `resolved` under the usual rules, in every state, so no episode or incident is left open. Default, kept: a fixed 24 hours. Note the limit of that choice: a group that reports less often than daily, such as a weekly batch job, is forgotten before its next run, so under `no_data` or `keep` it resolves after 24 hours and starts fresh each week. The alternative is a field per rule.
9. **Minimum frequency.** Default: 10 seconds for new and edited rules. An existing rule below it keeps its stored value and runs at 10 seconds. The same floor applies in the migration backfill and in every later job upsert.
10. **Resolve notifications for new-pattern rules.** Default: off.

## Unverified claims

| Claim | Experiment |
|---|---|
| Two Postgres replicas that run the claim statement at once get disjoint rows | The J1 conformance test against Postgres, 200 rounds. It proves the first claim only. |
| The row lock taken by `Hold` makes a concurrent claim skip the job until the handler commits, so a stale run cannot commit after a newer claim | The J1 takeover tests on Postgres, in both commit orders |
| On SQLite, `Hold` and the claim cannot interleave inside one write transaction | The same takeover tests on SQLite |
| Two drains never reserve the same delivery row, and the `NOT EXISTS` ordering clause holds under concurrency | The D2 takeover and ordering tests on Postgres |
| The mail library reports whether a failure came before or after the end of `DATA` | Read the SMTP client used by `internal/alerts/email_sender.go`, then the D2 fake-server test |
| The SQLite build in `modernc.org/sqlite` accepts `UPDATE ... WHERE id IN (SELECT ... LIMIT) RETURNING` | Run the claim query in a store test on SQLite |
| The `alerts` rebuild keeps every `alert_history` row, because the migration connection has foreign keys off | Read from `internal/store/sqlite/sqlite.go:129`. The A1 upgrade test through the real migration runner settles it. |
| `json_each` and `jsonb_array_elements_text` handle a null or empty targets column | The D1 migration test with null, `[]` and `null` values |
| The config loader ignores the removed `alerts.evaluation_interval` key | Start the server with the old key in `config.toml` and with `LOGCHEF_ALERTS__EVALUATION_INTERVAL` set |
| A VictoriaLogs `stats_query` with a past `time` returns what a live evaluation returned then | Seed rows, evaluate live, evaluate again an hour later with `At`, compare. Late-arriving rows will differ. |
| 120 sequential evaluations finish within 60 seconds on a production-size table | Time a 24-hour backtest of a count rule on the largest dev source |
| The cron parser fires a skipped local time once and a repeated local time once, as RFC 0008 requires | The J3 daylight-saving table test |
| PagerDuty has an EU endpoint and ignores a `resolve` for an unknown `dedup_key` | Read the Events API v2 reference, then send both from a trial account |
| A Slack section block accepts the text length that the sender produces | Post a 3,000-character block to a test webhook |
| Slack sends `Retry-After` on a 429 from an incoming webhook | Stated by the Slack rate-limit page. Not observed. |
| An old binary fails on alert writes after the migration, which is why a rolling upgrade is refused | Run the 2.7.0 binary against a migrated Postgres database and create an alert |
| The existing history pruning query keeps one open row only, so A1 must change it | Read from `internal/store/postgres/queries.sql:712`. Confirm with a store test of two open episodes. |
