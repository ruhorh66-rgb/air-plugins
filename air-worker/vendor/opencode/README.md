# OpenCode runtime payload

AirWorker 0.10.6 vendors the official OpenCode Windows x64 baseline archive as a ready coding-agent shell. AirWorker does not fork or modify OpenCode and does not use it for provider/model selection.

- upstream: `anomalyco/opencode`
- release: `v1.18.31`
- asset: `opencode-windows-x64-baseline.zip`
- packaged name: `opencode-windows-x64-baseline-v1.18.31.zip`
- archive SHA-256: `7c4fc9be7124df5e7c42184b99e8d8540fb0863bb0378b0c4219d9567b2d8434`
- extracted `opencode.exe` SHA-256: `a6167edb2f47fee14e834b8566fefadaa85ef031b8993ab3fd860899281a2856`
- license: MIT; see `LICENSE.opencode`

The installer verifies both hashes and `opencode --version` before placing the executable under the user-local AirWorker home. The archive is not downloaded at runtime.
