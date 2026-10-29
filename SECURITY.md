# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub's security advisories:
[open a private report](https://github.com/RhythmShahriar/tacet/security/advisories/new) (the
"Report a vulnerability" button on the Security tab). Don't open a public issue.

Include what you found, how to reproduce it and the tacet version (`tacet version`). You'll get
a reply within a few days. A fix is released before the details are made public and you are
credited in the advisory unless you'd rather not be.

## Supported versions

Only the latest release gets security fixes.

## Scope

tacet runs on your machine, reads your project and its Claude Code files and changes Claude Code's
settings through its plugin. Reports that matter most:

- a way for a file in a project to make tacet run code, write outside the project or the Claude
  Code config folder or send data anywhere
- a secret that `guard` should block but lets through, or a secret that appears in tacet's output
  or report
- a release archive or install script that can be swapped without failing the checksum or
  signature check

## Verifying a release

Every release's `checksums.txt` is signed with keyless Sigstore by the release workflow.
[`scripts/verify-release.sh`](scripts/verify-release.sh) checks the signature and the archive
before you run anything; the install scripts do the same when cosign is installed.
