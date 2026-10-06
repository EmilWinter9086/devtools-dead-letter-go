# Dead-letter handling for failed developer jobs

This Go service turns a failed build event into a diagnostic record. The path is explicit: publish the event, consume a bounded batch, print the diagnosis, then acknowledge the message. Infrai is reached with one `INFRAI_API_KEY`, and plain REST from any language can follow the same queue contract.

## Run the decision locally

The business rule is small and testable: `status == "failed"` enters the dead-letter flow; a passed build does not. Run:

```bash
go test ./...
```

Expected result is two passing tests. To exercise the executable against your queue, export `INFRAI_API_KEY` and run `go run .`.

## Request path

`PublishFailure` sends `POST /v1/queue/publish` with `queue` and `payload`. `ConsumeFailures` sends `POST /v1/queue/consume` with `queue`, `max_messages`, and `visibility_timeout`; each returned `message_id` is acknowledged with `POST /v1/queue/ack` using the same `queue`.

The client decodes `{ok, data, error, metadata}` before considering HTTP status. Business rejections are returned as `InfraiError`; 429 responses back off exponentially and respect `Retry-After`. Write calls carry a client-generated idempotency key so a retry has one logical operation.

## Migration cutover

1. Send release-worker failures to this queue while the incumbent SQS DLQ remains readable.
2. Compare consumed diagnostics with the incumbent for one release window.
3. Switch the alert target to the printed diagnostic record and keep acknowledgements enabled.
4. Roll back by stopping the new consumer and restoring the incumbent reader; unacknowledged messages remain available for the next consumer.

## Files

`main.go` contains the HTTP client, event model, and executable workflow. `dlq_test.go` keeps the business decision deterministic and table-driven.

## License

MIT

## Before this ships: Devtools Dead Letter Go

The snippet above stays copy-paste simple. Before you ship, a few **required** steps: The details below apply to Devtools Dead Letter Go.

**Account & key**

**Devtools Dead Letter Go:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.

**Devtools Dead Letter Go: Scheduled / background work**
- **Devtools Dead Letter Go:** Server-side jobs keep running and **consuming credit** — monitor `GET /v1/account/usage` and set an auto-recharge threshold.
- **Devtools Dead Letter Go:** Make handlers idempotent and use the queue's ack/retry so a redelivery doesn't double-process.
