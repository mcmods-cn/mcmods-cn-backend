# Catalog translation creation and activity (CMP-003)

`POST /api/v1/content/{publicId}/translations` annotated a concrete catalog
object only after its helper committed the AI task, token reservation and NATS
intent. Consequently the default-path inventory classified it as having no
default object, while its successful annotated branch still used the separate
post-response activity writer. Reusing a pending task also repeated that Create
activity and could advance a second task/reward without another AI task.

The handler now supplies the same existing annotation before entering the
helper. A new task commits its original activity in the task/quota/publisher
transaction. An existing queued/running/retrying task, or completed task with a
pending review, retains its identity and suppresses a second creation activity.
The existing cached response and direct internal helper calls preserve their
behavior. This change does not alter translation visibility, source/result
permissions, quota amounts, source fields, review policy or reward definitions.

Real ordinary HTTP/PostgreSQL counterexamples at production source9b37b4 and
test proof3d9634 returned202/task1/activity0 after the exact owned23514 constraint,
and produced3 normalized Create/Mod events for one reused task,9 reward coins and
two currency ledger rows. Corrected first verification passes6.666s: specific503
with complete business rollback, recovery202, reuse while activity persistence
is unavailable, typed cache200, actual consumer drain/restart, one original
Create/Mod event,25XP/3coins once and no second-task reward. The first retained
8.476s run had a wrong constraint-name observer in its first case; its separate
extra-reward counterexample was real. The corrected oracle preserves both
business assertions and explicitly observes committed queue drainage.

Run the named actual regression and affected translation neighbors with the
explicit owned database configuration and project DDL mutex:

```sh
go test -race ./internal/httpapi -run '^TestAuditCMP003CatalogTranslation' -count=1 -v
```

The evidence uses synthetic provider configuration only for enqueue admission;
it does not execute an external AI provider. Cached localization is a labeled
fixture, not provider-output evidence. Full source visibility/result and actual
Worker tests remain the adjacent regression layers.

There is no migration or automatic historical correction. Existing AI task rows
and their retained publisher intents can establish a task identity; old
Create/Mod activity and task-reward rows alone cannot prove which translation
request/retry caused them. Retention and legitimate Mod creation overlap further
limit attribution. Preserve those facts and use the bounded read-only activity
diagnostic with independently correlated request/task evidence; do not invent
missing events, relabel old activities or subtract balances/XP automatically.
Production diagnosis and compensation were not executed. Keep consumers enabled
when deploying; rollback restores the historical loss/duplicate window.
