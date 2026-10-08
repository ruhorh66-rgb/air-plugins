# Keep CLI command discovery and routing self-consistent

## When to apply

Apply when a CLI exposes shared or legacy subcommands, especially when some actions require a product or context flag.

## Procedure

1. Define one command contract listing every supported action, required flag, help text, and diagnostic behavior.
2. Verify root help, `learn --help`, no-argument usage, and each action’s help against that contract.
3. Invoke every recognized shared action without its required product/context flag; confirm it returns a typed, actionable missing-context diagnostic rather than “unknown action.”
4. Repeat representative actions with the product/context flag and confirm routing reaches the shared authority without changing state semantics.
5. Keep regression checks for help, missing-context, and product-qualified paths together so discovery and routing cannot drift.

## Pitfalls

- Advertising actions in root help that a subcommand parser does not recognize.
- Treating a missing required context as an unknown action.
- Testing only successful, product-qualified invocations and missing the discovery failure.
