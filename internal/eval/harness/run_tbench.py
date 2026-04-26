#!/usr/bin/env python3
"""Run Terminal-Bench 2.0 against ycode.

89 complex terminal tasks across 10 technical domains (compilation, training,
system configuration, debugging, games, etc.).

Prerequisites:
  pip install harbor-ai
  docker desktop running

Usage:
  # Quick test (1 task, 1 attempt)
  python run_tbench.py --model claude-sonnet-4-6-20250514 --tasks 1 --attempts 1

  # Full run for leaderboard
  python run_tbench.py --model claude-sonnet-4-6-20250514 --attempts 5

  # Run with local Ollama
  python run_tbench.py --model qwen2.5-coder:32b --provider ollama

Cost estimate: $5-50 for full 89-task run depending on model.
"""

import argparse
import json
import os
import subprocess
import sys
import time
from pathlib import Path

from scoring import pass_at_k, flakiness, composite_score


def check_prerequisites():
    """Verify harbor and docker are available."""
    try:
        subprocess.run(["docker", "info"], capture_output=True, check=True)
    except (FileNotFoundError, subprocess.CalledProcessError):
        print("ERROR: Docker not running. Terminal-Bench requires Docker.")
        sys.exit(1)

    # Check if harbor is installed.
    try:
        result = subprocess.run(["harbor", "--version"], capture_output=True, text=True)
        if result.returncode != 0:
            raise FileNotFoundError
    except FileNotFoundError:
        print("Harbor not found. Installing...")
        subprocess.run([sys.executable, "-m", "pip", "install", "harbor-ai"], check=True)


def run_benchmark(args):
    """Execute Terminal-Bench via Harbor."""
    check_prerequisites()

    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)

    # Build harbor command.
    cmd = [
        "harbor", "run",
        "-d", f"terminal-bench@{args.version}",
        "-m", args.model,
        "-k", str(args.attempts),
    ]

    if args.tasks:
        cmd.extend(["--limit", str(args.tasks)])

    # Configure provider via environment.
    env = os.environ.copy()
    if args.provider == "ollama":
        env["OPENAI_API_BASE"] = env.get("OLLAMA_HOST", "http://127.0.0.1:11434") + "/v1"
        env["OPENAI_API_KEY"] = "ollama"
    elif args.provider == "anthropic":
        if "ANTHROPIC_API_KEY" not in env:
            print("ERROR: ANTHROPIC_API_KEY required")
            sys.exit(1)
    elif args.provider == "openai":
        if "OPENAI_API_KEY" not in env:
            print("ERROR: OPENAI_API_KEY required")
            sys.exit(1)

    print(f"Running Terminal-Bench {args.version}: model={args.model} attempts={args.attempts}")
    print(f"Command: {' '.join(cmd)}")
    print()

    start = time.time()
    result = subprocess.run(cmd, env=env, capture_output=False)
    duration = time.time() - start

    return {
        "benchmark": f"terminal_bench_{args.version}",
        "model": args.model,
        "provider": args.provider,
        "attempts": args.attempts,
        "exit_code": result.returncode,
        "duration_seconds": round(duration, 1),
    }


def parse_results(results_dir):
    """Parse Terminal-Bench results into our scoring format."""
    results_path = Path(results_dir)

    # Harbor stores results in JSON format.
    result_files = list(results_path.glob("**/results.json"))
    if not result_files:
        print("No results found. Check Harbor output directory.")
        return None

    all_results = []
    for rf in result_files:
        with open(rf) as f:
            data = json.load(f)
        all_results.append(data)

    if not all_results:
        return None

    # Aggregate pass rates.
    total_tasks = 0
    total_passed = 0
    task_results = []

    for run_data in all_results:
        tasks = run_data.get("tasks", [])
        for task in tasks:
            total_tasks += 1
            passed = task.get("success", False)
            if passed:
                total_passed += 1
            task_results.append({
                "name": task.get("name", "unknown"),
                "passed": passed,
                "domain": task.get("domain", "unknown"),
                "duration_s": task.get("duration", 0),
            })

    pass_rate = total_passed / total_tasks if total_tasks > 0 else 0

    return {
        "total_tasks": total_tasks,
        "passed": total_passed,
        "pass_rate": round(pass_rate, 4),
        "pass_at_k": pass_at_k(total_tasks, total_passed, 1),
        "flakiness": flakiness(pass_rate),
        "tasks": task_results,
    }


def main():
    parser = argparse.ArgumentParser(description="Run Terminal-Bench benchmark")
    parser.add_argument("--model", required=True, help="Model name")
    parser.add_argument("--provider", default="anthropic", choices=["anthropic", "openai", "ollama"])
    parser.add_argument("--version", default="2.0", help="Terminal-Bench version")
    parser.add_argument("--attempts", type=int, default=5, help="Attempts per task (k for pass@k)")
    parser.add_argument("--tasks", type=int, default=None, help="Limit number of tasks (for quick test)")
    parser.add_argument("--output-dir", default="./tbench-results", help="Output directory")

    args = parser.parse_args()
    result = run_benchmark(args)

    print()
    print(f"Benchmark complete in {result['duration_seconds']}s")
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
