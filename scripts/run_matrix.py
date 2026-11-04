"""Run a gcserve experiment matrix with repetitions and record run metadata.

Usage:
    python scripts/run_matrix.py experiments/pilot.json [--dry-run] [--resume]

The matrix file (JSON) has:
    name        output directory under results/
    reps        repetitions per (workload, config)
    gomaxprocs  GOMAXPROCS for every run (default 4)
    cooldown    seconds to pause between runs (default 2)
    common      gcserve flags shared by all runs, e.g. {"duration": "15s"}
    workloads   {name: flags}, e.g. {"steady-med": {"rate": 30000}}
    configs     {name: flags}, e.g. {"gogc400": {"gogc": "400"}}

Flags are merged as common < workload < config and passed as -key value
(true -> bare -key). Each run writes
    results/<name>/<workload>/<config>/rep<k>.{csv,json,log}
and results/<name>/run_info.json records the machine, toolchain, git commit,
and the matrix itself.

Run order is shuffled within each repetition, so slow drift on the machine
(thermal, background load) does not line up with one configuration. All
configs in repetition k use the same seed, so they see the same arrivals.
"""
import argparse
import json
import os
import platform
import random
import shutil
import subprocess
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
EXE = ROOT / "bin" / ("gcserve.exe" if os.name == "nt" else "gcserve")
RESERVED = {"out", "summary", "seed"}  # set by the runner


def sh(*cmd):
    try:
        return subprocess.run(cmd, cwd=ROOT, capture_output=True, text=True, check=True).stdout.strip()
    except (OSError, subprocess.CalledProcessError):
        return ""


def cpu_model():
    if sys.platform.startswith("linux"):
        for line in Path("/proc/cpuinfo").read_text().splitlines():
            if line.startswith("model name"):
                return line.split(":", 1)[1].strip()
    if os.name == "nt":
        name = sh("powershell", "-NoProfile", "-Command", "(Get-CimInstance Win32_Processor).Name")
        if name:
            return name
    return platform.processor()


def ram_gib():
    if sys.platform.startswith("linux"):
        kib = int(Path("/proc/meminfo").read_text().split()[1])
        return round(kib / 2**20, 1)
    if os.name == "nt":
        b = sh("powershell", "-NoProfile", "-Command", "(Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory")
        if b.isdigit():
            return round(int(b) / 2**30, 1)
    return None


def to_args(flags):
    args = []
    for k, v in flags.items():
        if k in RESERVED:
            sys.exit(f"flag -{k} is set by the runner; remove it from the matrix")
        if v is True:
            args.append(f"-{k}")
        elif v is not False and v is not None:
            args += [f"-{k}", str(v)]
    return args


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("matrix")
    ap.add_argument("--dry-run", action="store_true", help="print the run plan and exit")
    ap.add_argument("--resume", action="store_true", help="skip runs whose summary JSON exists")
    a = ap.parse_args()

    m = json.loads(Path(a.matrix).read_text())
    reps, procs = int(m["reps"]), str(m.get("gomaxprocs", 4))
    cooldown = float(m.get("cooldown", 2))
    out = ROOT / "results" / m["name"]

    plan = []
    for rep in range(1, reps + 1):
        runs = [(w, c) for w in m["workloads"] for c in m["configs"]]
        random.Random(rep).shuffle(runs)
        plan += [(rep, w, c) for w, c in runs]

    if a.dry_run:
        for rep, w, c in plan:
            flags = {**m.get("common", {}), **m["workloads"][w], **m["configs"][c]}
            print(f"rep{rep} {w}/{c}: {' '.join(to_args(flags))}")
        print(f"{len(plan)} runs")
        return

    if out.exists() and not a.resume:
        sys.exit(f"{out} exists; use --resume to continue it or pick a new name")
    out.mkdir(parents=True, exist_ok=True)

    subprocess.run(["go", "build", "-o", str(EXE), "./cmd/gcserve"], cwd=ROOT, check=True)
    info = {
        "started": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "go_version": sh("go", "version"),
        "git_commit": sh("git", "rev-parse", "HEAD"),
        "git_dirty": bool(sh("git", "status", "--porcelain", "--", "cmd", "scripts", "go.mod")),
        "os": platform.platform(),
        "cpu": cpu_model(),
        "logical_cpus": os.cpu_count(),
        "ram_gib": ram_gib(),
        "gomaxprocs": procs,
        "matrix": m,
    }
    if info["git_dirty"]:
        print("warning: uncommitted changes in cmd/ or scripts/; results may not be reproducible")
    info_path = out / "run_info.json"
    if not info_path.exists():
        info_path.write_text(json.dumps(info, indent=2) + "\n")
    shutil.copy(a.matrix, out / "matrix.json")

    env = {**os.environ, "GOMAXPROCS": procs}
    for k in ("GOGC", "GOMEMLIMIT"):  # GC settings come only from the matrix
        env.pop(k, None)

    failed = 0
    for i, (rep, w, c) in enumerate(plan, 1):
        d = out / w / c
        base = d / f"rep{rep}"
        if a.resume and base.with_suffix(".json").exists():
            continue
        d.mkdir(parents=True, exist_ok=True)
        flags = {**m.get("common", {}), **m["workloads"][w], **m["configs"][c]}
        cmd = [str(EXE), *to_args(flags), "-seed", str(rep), "-out", str(base.with_suffix(".csv"))]
        print(f"[{i}/{len(plan)}] rep{rep} {w}/{c}", flush=True)
        with open(base.with_suffix(".log"), "w") as log:
            r = subprocess.run(cmd, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT)
        if r.returncode != 0:
            failed += 1
            print(f"  failed (exit {r.returncode}), see {base.with_suffix('.log')}")
        if i < len(plan):
            time.sleep(cooldown)

    print(f"done: {len(plan) - failed} ok, {failed} failed -> {out}")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
