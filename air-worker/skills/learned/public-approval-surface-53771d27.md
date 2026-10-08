# Expose Public Approval Paths

## When to apply

Apply when a normal product operation requires authority or approval, especially when fresh sessions must complete the operation with healthy provider credentials.

## Procedure

1. Expose a typed, documented public approval command for the operation.
2. Expose read-only authority-status diagnostics.
3. Validate the complete user path in a fresh session, including compose, approve, and send.

## Pitfalls

- Do not require operators to know internal grant commands or hidden authority-file paths.
- Do not require users to hand-build approval envelopes.
- Healthy providers and credentials do not compensate for an inaccessible approval surface.
