#!/usr/bin/env python3
"""Archive native bundles and hash distributables after `mygo build`."""
import hashlib
import pathlib
import shutil
import sys
import tarfile

root = pathlib.Path(sys.argv[1])
target = sys.argv[2].replace("/", "-")
output = root / target
upload = root / "artifacts"
upload.mkdir(parents=True, exist_ok=True)
artifacts = []
for bundle in output.glob("*.app"):
    archive = upload / f"mygo-git-{target}.tar.gz"
    with tarfile.open(archive, "w:gz") as packed:
        packed.add(bundle, arcname=bundle.name)
    artifacts.append(archive)
for source in output.iterdir():
    if source.is_file() and source.suffix in {".gz", ".deb", ".exe", ".dmg"}:
        name = f"mygo-git-{target}.exe" if source.suffix == ".exe" else source.name
        destination = upload / name
        shutil.copy2(source, destination)
        artifacts.append(destination)
if not artifacts:
    raise SystemExit("No distributable artifacts were produced")
checksums = upload / f"SHA256SUMS-{target}.txt"
with checksums.open("w", encoding="utf-8") as manifest:
    for artifact in sorted(artifacts):
        digest = hashlib.sha256()
        with artifact.open("rb") as source:
            for chunk in iter(lambda: source.read(1024 * 1024), b""):
                digest.update(chunk)
        manifest.write(f"{digest.hexdigest()}  {artifact.name}\n")
print("\n".join(str(p) for p in artifacts + [checksums]))
