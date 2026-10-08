# Release Host Parity Verification

## When to apply

Apply before operationally accepting a release when execution passes through native runtime, marketplace adapter, host plugin cache, hooks, or shipped skills.

## Procedure

1. Select one release candidate as the comparison source.
2. Verify the native binary version matches the candidate.
3. Verify the deployed marketplace adapter version matches the same candidate.
4. Verify the host plugin cache version matches the candidate.
5. Verify required hooks are present and correspond to the candidate.
6. Compute and compare SHA256 values for shipped skills against the candidate.
7. Accept the release only when every layer matches; otherwise correct the stale layer and repeat the checks.

## Pitfalls

- A correct native binary version does not prove that the host executes the new release.
- A stale plugin cache can execute old skills while the native runtime appears healthy.
- Checking versions without verifying hooks and skill hashes can miss mixed-release state.
