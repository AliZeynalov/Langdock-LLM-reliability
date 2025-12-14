# Questions to Address

## Missing from the guide (but mentioned in case statement):

- How will you handle **rate limiting** specifically? (backoff is mentioned, but rate limit detection/handling could be more explicit)
- **Stalled stream detection** - how do you detect when a stream hangs without closing?
- **Token tracking** for partial failures (mentioned in case statement as important for cost/latency)

## Areas to expand:

- **Provider abstraction**: Consider documenting how you'll normalize different provider response formats and error codes

- **Failover criteria**: What triggers a failover vs a retry? (circuit breaker state, error types, etc.)

- **Mid-stream validation**: This is mentioned but could use more detail - how do you validate structured output chunks without buffering the entire response?
