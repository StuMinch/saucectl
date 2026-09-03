# Product Requirements Document: Prune Passed Retries

 ## Overview

 **Feature name:** `prunePassedRetries`

 **Parent feature:** `smartRetry`

 **Status:** Proposed

 Add an option that removes a test failure from the final suite results when that test passes on a subsequent retry. This allows customers to focus on tests that remain genuinely unstable or broken instead of failures that were recovered by retrying.

 ## Problem

 `smartRetry.failedOnly: true` currently limits a retry to tests that failed in the previous attempt. This reduces unnecessary execution, but a test that fails initially and passes on retry can still contribute to the suite's overall failure rate.

 As a result, the reported failure rate may be artificially elevated even though the test ultimately recovered. Customers must manually distinguish recovered failures from failures that remain after all retry attempts, making triage slower and less reliable.

 ## Goals

 - Remove recovered test failures from the final failure set when a retry passes.
 - Make the final results reflect tests that still fail after the configured retry attempts.
 - Preserve the current `smartRetry.failedOnly` behavior and configuration compatibility.
 - Give customers a clear way to identify failures that require attention.

 ## Non-goals

 - Changing how many retry attempts are run.
 - Changing the definition of a test pass or failure.
 - Suppressing tests that fail on their final attempt.
 - Changing retry behavior when `smartRetry.failedOnly` is disabled.

 ## Proposed Configuration

 Add `prunePassedRetries` under the existing `smartRetry` block:

 ```yaml
 smartRetry:
	 failedOnly: true
	 prunePassedRetries: true
 ```

 `prunePassedRetries` is a boolean and defaults to `false` to preserve existing behavior.

 The option is intended to be used with `failedOnly: true`. When `prunePassedRetries: true` is configured without `failedOnly: true`, saucectl should reject the configuration with a clear validation error rather than silently producing unexpected results.

 ## User Experience

 With the feature enabled:

 1. saucectl runs the initial suite.
 2. saucectl retries only the tests that failed in the previous attempt.
 3. For each retried test that passes, saucectl removes the earlier failure from the final failure set.
 4. Tests that continue to fail remain in the final results and continue to affect the failure rate.

 Example:

 | Test | Initial attempt | Retry | Final result |
 | --- | --- | --- | --- |
 | `login succeeds` | Failed | Passed | Excluded from final failures |
 | `checkout accepts card` | Failed | Failed | Included in final failures |
 | `search returns results` | Passed | Not retried | Included as passed |

 ## Functional Requirements

 ### Configuration

 - Add `prunePassedRetries` to the `smartRetry` configuration object.
 - Accept only boolean values.
 - Default the option to `false` when omitted.
 - Reject `prunePassedRetries: true` when `smartRetry.failedOnly` is not enabled.
 - Document the option in the applicable configuration schemas and user documentation.

 ### Retry and result handling

 - When enabled, use the existing smart retry flow to identify failed tests for retry.
 - Match retry results to the corresponding tests from the prior attempt using the framework's existing test identity.
 - Remove a prior failure only when the corresponding retry result is a pass.
 - Keep a test in the final failure set when it fails on its latest attempt.
 - Leave tests that were not retried unchanged.
 - Apply the behavior consistently across every framework that supports `smartRetry.failedOnly`.

 ### Failure and fallback behavior

 - If the previous report cannot be retrieved, parsed, or matched reliably, preserve the existing retry and reporting behavior rather than pruning results speculatively.
 - If a retry result is missing or ambiguous, retain the failure.
 - A job-level error or unavailable report must not be treated as a passing retry.

 ## Acceptance Criteria

 - A configuration with `smartRetry.failedOnly: true` and `prunePassedRetries: true` is accepted.
 - A failed test that passes on its retry is absent from the final failure set and does not contribute to the final failure rate.
 - A failed test that fails on its retry remains in the final failure set.
 - A test that passes initially is unaffected and is not retried solely because pruning is enabled.
 - Existing configurations without `prunePassedRetries` produce the same results as they do today.
 - `prunePassedRetries: true` without `failedOnly: true` produces a clear configuration error.
 - Reported test counts, pass rates, and failure rates remain internally consistent after pruning.
 - The behavior is covered by automated tests for recovered failures, persistent failures, multiple retries, missing results, and each supported framework.

 ## Example Scenarios

 ### Recovered failure

 A suite has 100 tests. One test fails initially and passes on retry. With pruning enabled, the final failure count is zero for that test, and the recovered failure does not lower the final pass rate.

 ### Persistent failure

 A suite has 100 tests. One test fails initially and fails on retry. The test remains visible as a failure and continues to affect the final failure rate.

 ### Multiple retries

 A test fails on its first attempt, fails on the second attempt, and passes on the third attempt. The test is pruned from the final failure set because its latest retry passed. A test that fails on its final configured attempt remains reported as failed.

 ### Feature disabled

 When `prunePassedRetries` is omitted or set to `false`, saucectl retains the current result aggregation behavior, including failures from earlier attempts.

 ## Success Metrics

 - Customers can identify the tests that remain failing without manually filtering recovered retries.
 - The final failure rate no longer counts failures that were recovered by a passing retry when the feature is enabled.
 - No increase in retry selection errors, report parsing errors, or inconsistent test counts.

 ## Open Questions

 - Should the final report retain a separate indication that a test was recovered by retry, even though it is removed from the failure set?
 - Should pruning apply only to the final aggregate failure rate, or also to individual attempt-level artifacts and dashboards?
 - Which report formats and frameworks are included in the first release?
