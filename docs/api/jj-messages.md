# POST /api/v1/jj/messages

Single chat entry point for JJ. A rule-based classifier (`service.ClassifyMessage`) decides how
the message is handled:

- **conversation** (greetings, small talk, questions, explanations; also the default for
  ambiguous text): one worker-executed *chat task*, no orchestration.
- **objective** (imperative requests for a deliverable or multi-step work, e.g. "Research three
  Go queue libraries and compare them"): the existing orchestrator
  (`JJOrchestrator.SubmitRequest`), same `orchestration` / `plan` payload as
  `POST /api/v1/orchestrator/requests`.

Auth: same user JWT as the other `/api/v1` routes (401 without it).

## Request

```json
{
  "message": "Explain what a Go worker does in three bullet points",
  "conversation_id": "5b2f0c6e-3a1d-4c1e-9f0b-7d2a8e4c1a90",
  "history": [{"role": "user", "text": "hi"}, {"role": "assistant", "text": "Hello!"}],
  "priority": "NORMAL_P2",
  "idempotency_key": "client-generated-key"
}
```

| Field | Rules |
|---|---|
| `message` | required, non-blank, at most 8000 characters |
| `conversation_id` | optional UUID; generated when absent. Send the returned value on follow-ups |
| `history` | optional; roles `user`/`assistant` only (else 400); text truncated to 2000 chars; only the last 10 turns are kept |
| `priority` | optional; one of the task priorities (400 listing allowed values otherwise). Applies to objectives (default `NORMAL_P2`); chat tasks are always `HIGH_P1` |
| `idempotency_key` | optional, at most 255 chars; the `Idempotency-Key` header is also accepted |

## Chat task (conversation)

`manager_domain` `JJ`, no parent, title `Chat: <first 60 chars>`, goal = the message, priority
`HIGH_P1`, `max_retries` 3, `input_payload`
`{"kind":"chat","conversation_id":"...","history":[...]}`. The worker completes it with
`output_payload` `{"response":"...","provider":{"name","version","model"}}`. Read it with
`GET /api/v1/tasks/{task_id}` (poll until `status` is `COMPLETED` or `FAILED`; other users get 404).

## Response

`201 Created` (`200 OK` when an idempotency key matched an existing conversation task: the
original `task_id` is returned and no second task is created). The task or orchestration is
persisted before the response.

Conversation: see `web-contract-fixtures/jj_message_conversation.json`.

```json
{"success": true, "data": {"kind": "conversation", "conversation_id": "...", "task_id": "...",
  "runner": {"wake": "signaled"}}}
```

Objective: see `web-contract-fixtures/jj_message_objective.json`
(`kind`, `conversation_id`, `orchestration`, `plan`, `runner`).

### `runner`

| `wake` | Meaning |
|---|---|
| `signaled` | start signal sent to the worker instance |
| `skipped` | nothing to do: worker already running/booting, recent signal, duplicate request, or no runnable work |
| `failed` | wake did not succeed; the work is saved and the scheduler retries. `detail` says why |
| `disabled` | scale-to-zero wake is off |

A wake failure never fails the request. When AWS has no capacity for the instance
(`InsufficientInstanceCapacity`), `detail` is
`AWS has no capacity for the worker instance right now; it will retry` and the API logs it at
ERROR (`runner.ErrInsufficientCapacity`). The UI should show this as an in-progress/delayed state,
not an error.

## Errors

`400 VALIDATION_ERROR` with a message: empty or too long message, bad `conversation_id`, bad
history role, invalid priority, over-long idempotency key. `401` unauthenticated.

## Known limit

Idempotency keys are honored for conversations. Objectives go through the unchanged orchestrator
and are not de-duplicated by key.
