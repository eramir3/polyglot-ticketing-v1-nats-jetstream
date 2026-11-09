# Error Contract

The API gateway is the public error boundary. REST endpoints respond with:

```json
{
  "errors": [
    {
      "message": "Email must be valid",
      "code": "INVALID_EMAIL",
      "field": "email"
    }
  ]
}
```

GraphQL endpoints use the native GraphQL `errors` array. Each item has its
message at the top level and its application code and optional field in
`extensions`.

Services communicate errors over gRPC using the relevant gRPC status code and
the JSON representation of `common.v1.ErrorResponse` in the status details.
`common.v1.ErrorCode` is the canonical source for the stable public code
values; its generated values are converted to the string values below at the
public error boundary.

## Public Codes

| Code                         | Meaning                                                 |
| ---------------------------- | ------------------------------------------------------- |
| `INVALID_ARGUMENT`           | A request is invalid but no more specific code applies. |
| `INVALID_NAME`               | The `name` field is missing or invalid.                 |
| `INVALID_EMAIL`              | The `email` field is missing or invalid.                |
| `INVALID_PASSWORD`           | The `password` field is missing or invalid.             |
| `INVALID_TITLE`              | The `title` field is missing or invalid.                |
| `INVALID_PRICE`              | The `price` field is missing or invalid.                |
| `INVALID_CREDENTIALS`        | The supplied email or password is incorrect.            |
| `EMAIL_NOT_VERIFIED`         | The account email must be verified before sign-in.      |
| `INVALID_VERIFICATION_TOKEN` | An email verification token is invalid or expired.      |
| `ALREADY_EXISTS`             | The requested resource already exists.                  |
| `UNAUTHENTICATED`            | Authentication is required or invalid.                  |
| `FORBIDDEN`                  | The caller lacks permission.                            |
| `NOT_FOUND`                  | The requested resource does not exist.                  |
| `RATE_LIMITED`               | The caller exceeded a rate limit.                       |
| `SERVICE_UNAVAILABLE`        | A required service is unavailable.                      |
| `INTERNAL_ERROR`             | An unexpected error occurred.                           |

`field`, when present, uses the public lower-camel-case request field name.
Service implementation details, stack traces, and database errors must not be
included in public error messages.

## Creation Idempotency Keys

`POST /api/tickets` and `POST /api/orders` require one `Idempotency-Key`
header containing 1–128 ASCII letters, digits, underscores, or hyphens.
Missing, repeated, or malformed keys return HTTP `400`, code
`INVALID_ARGUMENT`, and field `idempotencyKey`. The owning gRPC services also
validate the required `idempotency_key` creation field.

Reusing an unexpired key for different validated business input returns gRPC
`ALREADY_EXISTS`, translated to HTTP `409`, with public code `ALREADY_EXISTS`,
field `idempotencyKey`, and message
`Idempotency key was already used with different input.` Keys are case-sensitive
and scoped by authenticated user and operation. Matching successful retries
replay the original response and HTTP status for 24 hours; errors are not
cached. Retries do not extend retention. See `SPEC.md` for transaction and
expiry semantics.
