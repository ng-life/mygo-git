#!/usr/bin/env python3
"""Restart only this checkout's executable; never interrupt a Git subprocess."""
import os
import pathlib
import signal
import subprocess
import sys
import time

project = pathlib.Path(sys.argv[1]).resolve()
build = pathlib.Path(sys.argv[2])
binary = project / "bin" / "mygo-git"


def processes():
    rows = subprocess.check_output(["ps", "-axo", "pid=,ppid=,comm="], text=True)
    result = {}
    for row in rows.splitlines():
        fields = row.strip().split(None, 2)
        if len(fields) == 3:
            result[int(fields[0])] = (int(fields[1]), fields[2])
    return result


def app_pids(snapshot):
    return [pid for pid, (_, command) in snapshot.items() if command == str(binary)]


def git_children(snapshot, parents):
    descendants = set(parents)
    while True:
        found = {pid for pid, (parent, _) in snapshot.items() if parent in descendants}
        if found <= descendants:
            break
        descendants |= found
    return [pid for pid in descendants - set(parents)
            if pathlib.Path(snapshot[pid][1]).name in {
                "git", "ssh", "git-remote-http", "git-remote-https",
                "git-upload-pack", "git-receive-pack"}]


deadline = time.monotonic() + 60
while True:
    snapshot = processes()
    old = app_pids(snapshot)
    if not git_children(snapshot, old):
        break
    if time.monotonic() >= deadline:
        raise SystemExit("Git is still running; kept the existing app. Retry after the operation finishes.")
    time.sleep(0.5)
for pid in old:
    try:
        os.kill(pid, signal.SIGTERM)
    except ProcessLookupError:
        pass
for _ in range(100):
    if not app_pids(processes()):
        break
    time.sleep(0.1)
else:
    raise SystemExit("Existing app did not exit cleanly; it was not force-killed.")
os.replace(build, binary)
log_path = project / "bin" / "dev.log"
with log_path.open("ab", buffering=0) as log:
    process = subprocess.Popen([str(binary)], cwd=project, stdin=subprocess.DEVNULL,
                               stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
(project / "bin" / "dev.pid").write_text(str(process.pid) + "\n")
time.sleep(2)
if process.poll() is not None:
    raise SystemExit(f"App exited during startup; inspect {log_path}")
print(f"Started MyGo Git: PID {process.pid}; log: {log_path}")
