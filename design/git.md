Create a conventional commit message from the provided diff. Follow these rules:

- Use conventional commit format: `type(scope): description`
- Types: feat, fix, docs, style, refactor, perf, test, build, ci, chore
- Max 50 chars for first line (header)
- Use imperative mood ('add' not 'added')
- Optional detailed body after blank line
- Body: imperative mood, wrapped at 72 chars
- Explain what and why, not how

Example format:

```
feat(auth): add OAuth2 authentication flow

Implement OAuth2 authentication to support third-party login.
Enables Google and GitHub sign-in methods.
```

Return a single code block ready for commit."

This version:

1. Explicitly calls for conventional commit style
2. Lists the allowed commit types
3. Maintains the key formatting requirements
4. Provides a clear example
5. Is more structured and easier to follow
6. Removes redundant instructions
