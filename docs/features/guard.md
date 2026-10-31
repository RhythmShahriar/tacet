# guard

**Default: on.** Keeps secrets out of the session.

```sh
tacet off guard
```

## What it does

Blocks Claude Code's reads of files that hold secrets. It also blocks shell commands that would print them (`cat .env`, `head id_rsa`). Each block comes with a reason Claude Code can act on, such as reading `.env.example` instead.

Blocked:

- `.env`, `.env.*` and `.envrc`
- keys and keystores: `.pem`, `.key`, `.p12`, `.pfx`, `.jks`, `.keystore`, `.ppk`, `.asc`, `.gpg`
- SSH keys (`id_rsa`, `id_ed25519` and the rest)
- `.netrc`, `.pgpass`, `.pypirc`, `.npmrc`, `.git-credentials`, `.htpasswd`, `auth.json`
- `credentials.*`, `secret.*`, `secrets.*`, service account JSON files
- Terraform state
- anything under `~/.ssh`, `~/.aws`, `~/.gnupg`, plus `~/.docker/config.json` and `~/.kube/config`

Templates pass: `.env.example`, `.sample`, `.template`, `.dist` and `.defaults`.

This is a guard against accidents, not a sandbox. Claude Code's own permission settings are the place for hard rules.
