# AirWorker Native Action

## When to apply

After an authenticated native validate failure because canonical run-config.json is missing.

## Procedure

~~~air-worker-action
{"schema":"air-worker.native-learned-action/v1","id":"validate.canonical-config-preflight","version":1,"native_command":"validate","precondition":"validate-run-config-missing","operation":"check-missing-config-and-defer","outcome":"deferred_missing_configuration","effect":"avoided_repeat_validation_with_missing_config"}
~~~

## Pitfalls

- This read-only action neither repairs files nor claims that validation passed.
