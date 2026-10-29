#!/usr/bin/env python3
"""Runs the setup assistant command lines that the docs give, as a newcomer
would type them, and checks the files it writes.

The commands are read from the doc blocks marked <!-- test:setup-synology -->,
setup-unraid, setup-vps and setup-flags in doc/quick-start.md (see
scripts/doc-blocks.mjs). Only what cannot work in CI is rewritten: the published
image becomes the image under test, sudo and -it are dropped, and the flags that
make the run non-interactive and offline are added when the block lacks them.
Then it asserts the generated .env and docker-compose.yml.

Usage: scripts/onboarding_wizard_check.py [image]    (default mnema-talk:local)
"""
import os
import re
import shutil
import stat
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
PUBLISHED = "ghcr.io/msch128/mnema-talk"
PUBLIC_URL = "https://chat.example.com"
REQUIRED = {  # key -> minimum length of the generated value
    "PUBLIC_URL": 1, "MNEMA_IMAGE": 1, "JWT_SECRET": 32, "POSTGRES_PASSWORD": 16,
    "S3_ACCESS_KEY": 3, "S3_SECRET_KEY": 8, "ADMIN_INITIAL_PASSWORD": 14,
}
PLACEHOLDERS = ("...", "changeme", "change-me", "change_me", "replace", "example", "<")
SMTP_FLAGS = ("--smtp-host smtp.example.com --smtp-port 587 --smtp-tls starttls "
              "--smtp-username mailer --smtp-password mailpass --smtp-from talk@example.com")


def block(name):
    out = subprocess.run(["node", os.path.join(ROOT, "scripts/doc-blocks.mjs"),
                          os.path.join(ROOT, "doc/quick-start.md"), name],
                         capture_output=True, text=True)
    if out.returncode != 0:
        sys.exit(out.stderr.strip() or "cannot read doc block " + name)
    return out.stdout


def assistant_command(text, image, extra=""):
    """The docs' `docker run ... setup` line, made runnable in CI."""
    text = re.sub(r"\\\n\s*", " ", text)
    lines = [l.strip() for l in text.splitlines() if "docker run" in l and not l.strip().startswith("#")]
    if len(lines) != 1:
        sys.exit("expected exactly one 'docker run' line in the doc block, got: %r" % lines)
    cmd = lines[0]
    cmd = re.sub(r"^sudo\s+", "", cmd)
    cmd = re.sub(r"\s-it\b", "", cmd)
    if PUBLISHED not in cmd or not re.search(r"\bsetup\b", cmd):
        sys.exit("the doc command no longer names %s and 'setup': %s" % (PUBLISHED, cmd))
    cmd = cmd.replace(PUBLISHED, image)
    # --pull never: the image under test only exists locally.
    cmd = cmd.replace("docker run ", "docker run --pull never ", 1)
    add = []
    if "--non-interactive" not in cmd and " -y" not in cmd:
        add.append("--non-interactive")
    if "--public-url" not in cmd:
        add.append("--public-url " + PUBLIC_URL)
    add.append("--skip-preflight")  # no DNS or network probes in CI
    if extra:
        add.append(extra)
    return cmd + " " + " ".join(add)


def read_env(path):
    values = {}
    for line in open(path):
        line = line.rstrip("\n")
        if line and not line.startswith("#") and "=" in line:
            k, _, v = line.partition("=")
            values[k] = v.strip().strip('"').strip("'")
    return values


def run(cmd, cwd, check=True):
    p = subprocess.run(["bash", "-c", cmd], cwd=cwd, capture_output=True, text=True)
    if check and p.returncode != 0:
        sys.exit("FAILED: %s\n%s%s" % (cmd, p.stdout, p.stderr))
    return p


def check_files(name, d, extra_env):
    errors = []
    for f in (".env", "docker-compose.yml"):
        if not os.path.isfile(os.path.join(d, f)):
            errors.append("%s was not written" % f)
    if errors:
        return errors
    env = read_env(os.path.join(d, ".env"))
    for key, minlen in REQUIRED.items():
        v = env.get(key, "")
        if len(v) < minlen:
            errors.append(".env: %s missing or shorter than %d characters" % (key, minlen))
        elif key not in ("PUBLIC_URL", "MNEMA_IMAGE") and any(p in v.lower() for p in PLACEHOLDERS):
            errors.append(".env: %s still holds a placeholder: %r" % (key, v))
    if env.get("PUBLIC_URL") != PUBLIC_URL:
        errors.append(".env: PUBLIC_URL is %r, want %r" % (env.get("PUBLIC_URL"), PUBLIC_URL))
    if len({env.get(k) for k in ("JWT_SECRET", "POSTGRES_PASSWORD", "S3_SECRET_KEY")}) != 3:
        errors.append(".env: secrets are not distinct")
    for k, want in extra_env.items():
        got = env.get(k, "")
        if (not got) if want is None else (got != want):
            errors.append(".env: %s is %r, want %s" % (k, got, "a value" if want is None else repr(want)))
    mode = stat.S_IMODE(os.stat(os.path.join(d, ".env")).st_mode)
    if mode & 0o077:
        errors.append(".env mode is %o, secrets must not be group or world readable" % mode)
    # The compose file must load with the .env next to it, and run the image
    # the .env names with its secrets substituted.
    p = run("docker compose config --format json", d, check=False)
    if p.returncode != 0:
        errors.append("docker compose config failed: " + p.stderr.strip())
    else:
        import json
        cfg = json.loads(p.stdout)
        app = cfg["services"].get("app", {})
        if app.get("image") != env["MNEMA_IMAGE"]:
            errors.append("compose: app image is %r, .env says %r" % (app.get("image"), env["MNEMA_IMAGE"]))
        if app.get("environment", {}).get("JWT_SECRET") != env["JWT_SECRET"]:
            errors.append("compose: the app does not receive JWT_SECRET from .env")
        if "ports" not in app:
            errors.append("compose: the app publishes no ports")
    return errors


def main():
    image = sys.argv[1] if len(sys.argv) > 1 else "mnema-talk:local"
    cases = [
        ("setup-synology", "", {}),
        ("setup-unraid", "", {}),
        ("setup-vps", "", {}),
        ("setup-flags", "", {"ADMIN_USERNAME": "admin"}),
        ("setup-flags", "--auto-update " + SMTP_FLAGS,
         {"COMPOSE_PROFILES": "autoupdate", "UPDATER_TOKEN": None, "SMTP_HOST": "smtp.example.com", "SMTP_PORT": "587"}),
    ]
    failures = []
    for name, extra, want in cases:
        label = name + (" + " + extra.split()[0] if extra else "")
        d = tempfile.mkdtemp(prefix="mnema-wizard-")
        os.chmod(d, 0o777)  # the image runs as uid 10001 and the docs do not always pass --user
        try:
            cmd = assistant_command(block(name), image, extra)
            print("==> %s\n    %s" % (label, cmd), flush=True)
            run(cmd, d)
            # Without --user the files belong to the image's uid; read them as root.
            subprocess.run(["docker", "run", "--rm", "--pull", "never", "--user", "0", "--entrypoint", "chown",
                            "-v", d + ":/d", image, "-R", "%d:%d" % (os.getuid(), os.getgid()), "/d"], check=True)
            errs = check_files(name, d, want)
            if not errs:
                before = open(os.path.join(d, ".env")).read()
                again = run(cmd, d, check=False)
                if again.returncode == 0 or open(os.path.join(d, ".env")).read() != before:
                    errs.append("a second run overwrote or accepted an existing .env")
            failures += ["%s: %s" % (label, e) for e in errs]
        finally:
            if subprocess.run(["rm", "-rf", d]).returncode != 0:
                subprocess.run(["sudo", "rm", "-rf", d])
    if failures:
        sys.exit("\nsetup assistant check failed:\n  " + "\n  ".join(failures))
    print("setup assistant check passed (%d command lines)" % len(cases))


if __name__ == "__main__":
    main()
