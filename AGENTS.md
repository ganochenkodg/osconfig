# GEMINI.md

## Go Development Guidelines

In this project we work on golang tests and follow next rules:

1. Write simple and idiomatic Go code. Prefer clarity over cleverness and avoid unnecessary abstractions.

2. Follow existing project style and structure. Keep consistency with naming, formatting, and file organization.

3. Always handle errors explicitly and add context when returning them.

4. Keep functions small and focused. Each function should do one thing well.

5. Avoid global state and use dependency injection via constructors. Do not change original code for testing without request.

6. Write comments only when necessary, and always in English.

7. Prefer table-driven tests for better readability and coverage. Use readable testcase names that shows what testcase is actually testing. Format of name is "`input`, want `expected result`", e.g. "valid data, want nil error".

8. wantErr in table-driven tests should have error type instead of boolean

9. For error comparison use function AssertErrorMatch from util/utiltest/utiltest.go. Example:
   ```go
   utiltest.AssertErrorMatch(t, gotErr, tt.wantErr)
   ```

10. For comparing variable values use function AssertEquals from util/utiltest/utiltest.go. Example:
    ```go
    utiltest.AssertEquals(t, gotCode, tt.wantCode)
    ```

11. Inside the test runner loop:
    ```go
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) { ... })
    }
    ```
    Try to write minimal code, ideally only retrieving results and errors, and comparing them with expectations. Move any lengthy setup or preparation steps to helper functions.

12. Add short docstrings before new functions

13. Never apply code changes immediately, carefully plan them and show to the user

14. Answer questions using user's language

15. Don't try to run go test locally, user will run them manually in docker inveronment
