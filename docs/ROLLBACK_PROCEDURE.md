# Rollback procedure

Principle: rollback never terminates or deletes pre-existing resources (bucket `griffin-artifacts-…`, `GriffinEC2WorkerRole`,
`GriffinEC2WorkerProfile`). Resources created by this deployment are deleted only after explicit user approval. Status: **not exercised** (nothing deployed).

| Order | Component | Trigger | Action | Effect |
|---|---|---|---|---|
| 1 | Scheduler | task storms / errant ticks | `aws events disable-rule --name griffin-tick $P` | no new wakeups/ticks (queued work stays durable in Postgres) |
| 2 | Runner | runaway cost / bad worker | `systemctl disable --now griffin-worker` via SSM, then `aws ec2 stop-instances --instance-ids $I $P` | compute billing stops; EBS (≈$2.7/mo est.) remains |
| 3 | API | 5xx / bad release | `aws lambda update-alias` to previous version, or `put-function-concurrency --reserved-concurrent-executions 0` | API stops/reverts; PWA shows errors |
| 4 | PWA | broken UI | `vercel rollback` | previous deployment |
| 5 | Secrets | leak | rotate `WORKER_API_KEY` + `SUPABASE_JWT_SECRET` in SSM, Lambda env and the EventBridge connection (`events update-connection`); restart worker | old keys rejected |
| 6 | Database | bad migration | restore the pre-migration `pg_dump -Fc` (take it before every `up`); never run `*.down.sql` on production | previous schema |
| 7 | Full teardown (needs approval, in this order) | abandoning the deploy | delete EventBridge rule/targets/destination/connection → API → Lambda → roles `griffin-api-lambda-role`, `griffin-events-invoke-role` → stop and (with approval) terminate **only** the `griffin-cloud-runner` instance and its retained EBS volume (`DeleteOnTermination=false`) → SG `griffin-runner-sg` → SSM params `/griffin/prod/*` | back to current state (empty account except existing bucket/role/profile) |

Detach IAM policies A/D after deployment is finished (admin) to return `griffin-bootstrap` to its current minimal state.
