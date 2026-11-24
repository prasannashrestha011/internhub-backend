## Development Mode — Keep Verification Lightweight

Work only on the requested scope. Prioritize implementation speed and correctness without performing exhaustive repository-wide validation.

### Testing rules

* Run only **targeted tests directly related to the files/packages/features you changed**.
* Do **not** run the full test suite (`go test ./...`, full frontend tests, full E2E suite, etc.) unless I explicitly request it.
* Do not repeatedly rerun tests that already passed unless a subsequent change could reasonably affect them.
* If there are no existing targeted tests for the changed code, perform a minimal compile/type-check or the smallest relevant verification instead.

### Do NOT perform automatically

Unless explicitly requested, do not:

* run full repository test suites
* run race detection
* run coverage analysis
* replay database migrations multiple times
* test upgrades from legacy database schemas
* create temporary PostgreSQL/MySQL databases solely for migration verification
* perform exhaustive migration compatibility testing
* run extensive regression suites
* run `go vet ./...` or equivalent repository-wide linting
* run repository-wide builds when a package-level build/check is sufficient
* perform repeated formatting verification across the entire repository
* update documentation, README files, API guides, or changelogs
* refactor unrelated code
* clean up unrelated warnings or technical debt
* add extra regression tests for unrelated edge cases discovered during implementation
* investigate speculative edge cases outside the requested feature
* make unrelated architectural improvements

### Migration changes

When modifying the database:

* implement the required migration
* verify that the migration is structurally correct
* run only the minimum test necessary for the requested feature
* do not simulate historical/legacy upgrades unless specifically requested
* do not repeatedly migrate fresh databases just to prove idempotency unless the task requires it

### Validation budget

Use this order of verification:

1. Format only changed files if necessary.
2. Compile/type-check the affected package/module.
3. Run the smallest relevant targeted test(s).
4. Stop when those checks pass.

Do not escalate to broader validation unless the targeted checks fail in a way that requires further investigation.

### Failure handling

If a targeted test fails:

* investigate only the failure relevant to the requested change
* make the smallest necessary fix
* rerun that targeted test
* do not automatically broaden into repository-wide debugging

If you discover an unrelated problem, mention it briefly in the final response instead of fixing or deeply investigating it.

### Scope discipline

Do not expand the task beyond what I explicitly requested.

For example, if I ask for:

* a model → don't also implement handlers and frontend
* an API endpoint → don't redesign adjacent APIs
* a frontend page → don't modify unrelated pages
* a bug fix → don't refactor the subsystem
* a UI redesign → don't change backend behavior

Before modifying a file, it should have a clear reason related to the requested task.

### Completion

Once the requested implementation works and the smallest relevant verification passes, **stop**.

In the final response, report only:

* what was changed
* which targeted verification was run
* whether it passed
* any important issue you intentionally left untouched because it was outside scope

Do not perform additional “just to be safe” validation after the requested task is already working.
