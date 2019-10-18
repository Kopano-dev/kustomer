## Bootstrap a Kopano system with a license

Generally supported Kopano requires a license to access the Kopano repositories
and to install. This document describes a way how to bootstrap a system using
a license file.

Generally the idea is that a customer retrieves a license file and uses a single
command which automatically takes care of setting up the repository access on
the system where the command is invoked and optionally also installs packages
and pre-seeds configuration.

## Install license

Customers can put the license(s) into `/etc/kopano/licenses` manually. This is
always supported and can be the first step when bootstrapping a new Kopano
system.

## Bootstrap

Nothing is installed yet, so to trigger installation a binary from Kopano is
loaded which automatically installs repository access (unless disabled) based on
the found licenses.

If no license is found, the bootstrap tool shall also prompt the user to
copy/paste license data to it.

Optionally the tool could offer to trial mode and automatically prompt for an
email address and offer selection for a product where we offer trials which
once confirmed automatically triggers an email containing the trial license.

### Running bootstrap

The bootstrap tool could be a shell script (sh) so it can be run directly with
a single `curl` pipe into shell command but since this is not very nice for the
admin (no way to review, we should not do that.)

```
curl -o kopanosetup.sh https://setup.kopano.com
```

Now review script, then run it:

```
sudo sh ./kopanosetup.sh
```

The script is just a wrapper for the real bootstrap command which is just
appended base64 encoded. The reason for the script in between is to give any
potential admin the possibility to see what it is actually doing by having a
text block at the beginning, stating details and links to the source code for
review and validation.

### Kopano bootstrap implementation

The bootstrap script and implementation are implemented as Go project for the
following reasons:

1. No runtime dependencies
2. Easily unit testable and statically typed (maintainability)
3. Still maintains local customizability by running it with Go
   (GO111MODULE="on" go get setup.kopano.com && ./kopanosetup)

Without that the thing would be a `sh` script, but since that thing is
potentially large and should be nice the shell script approach would soon
become complex.

