# CI is parked, not active

`ci.yml.disabled` is the intended GitHub Actions workflow (build + vet + test +
cross-compile matrix). It lives here instead of `.github/workflows/ci.yml` because
the personal access token used to push lacks the `workflow` scope, and GitHub
rejects any push that creates or edits files under `.github/workflows/` without it.

To activate CI, do **one** of:

1. **Add the `workflow` scope to the token**, then move the file back:
   `git mv .github/ci.yml.disabled .github/workflows/ci.yml` and push.
2. **Add it via the GitHub web UI** — create `.github/workflows/ci.yml` through
   github.com (the browser session has the scope) and paste in the contents of
   `ci.yml.disabled`.
