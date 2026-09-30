"""Check tracked source boundaries without printing credential contents."""
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parents[2]
GIT = ["git", "-c", f"safe.directory={ROOT.as_posix()}", "-C", str(ROOT)]
names = subprocess.check_output(GIT + ["ls-files", "-z"]).decode("utf-8").split("\0")
errors = []
count = 0
size = 0
for name in filter(None, names):
    path = ROOT / name
    if (name.startswith(("local/", "assistant-agent/dist/"))
            or name.startswith("external/") and name not in (
                "external/README.md", "external/dependencies.lock.json")):
        errors.append(f"Local-only file tracked: {name}")
    if re.search(r"(^|/)(id_rsa[^/]*|id_ed25519[^/]*|admin-token\.txt|secrets\.env|root-password\.hash|\.env)$", name):
        errors.append(f"Credential file tracked: {name}")
    data = path.read_bytes()
    count += 1
    size += len(data)
    if len(data) > 2 * 1024 * 1024:
        errors.append(f"Large file tracked (>2 MiB): {name}")
    if data.startswith((b"\x7fELF", b"MZ")) or path.suffix in (".img", ".squashfs", ".ubi", ".zip", ".tar", ".gz"):
        errors.append(f"Binary artifact tracked: {name}")
    try:
        text = data.decode("utf-8")
    except UnicodeDecodeError:
        continue
    if re.search(r"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----|\bsk-[A-Za-z0-9_-]{20,}|\bCERU_KEY-[A-Fa-f0-9-]{20,}", text):
        errors.append(f"Possible credential literal: {name}")
    if name.startswith("scripts/") and not name.startswith("scripts/legacy/") and path.suffix in (".sh", ".ps1") and ("/mnt/d/xiaoai" in text or "D:\\xiaoai" in text):
        errors.append(f"Machine-specific project path: {name}")
    if path.suffix == ".md":
        for dest in re.findall(r"\]\(([^\s)]+)\)", text):
            if re.match(r"^[a-zA-Z][a-zA-Z0-9+.-]*:|^#", dest):
                continue
            target = (path.parent / unquote(dest.split("#")[0])).resolve()
            # Local evidence and external checkouts are absent in fresh clones.
            if target.is_relative_to(ROOT / "local") or target.is_relative_to(ROOT / "external/xiaoai-patch"):
                continue
            if not target.exists():
                errors.append(f"Broken document link: {name} -> {dest}")

if count == 0:
    errors.append("No tracked files. Stage the reviewed source files first.")
for error in errors:
    print(error, file=sys.stderr)
print(f"Repository boundary check: {count} files, {size / 1024:.1f} KiB, {len(errors)} errors")
sys.exit(bool(errors))
