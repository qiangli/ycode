#!/usr/bin/env python3
"""Run Aider polyglot benchmark against ycode.

225 Exercism exercises across C++, Go, Java, JavaScript, Python, Rust.
Tests code editing with a retry loop (attempt → test → feedback → retry).

Prerequisites:
  pip install aider-chat
  docker (required for sandboxed test execution)

Usage:
  # Run with default model via ycode proxy
  python run_aider_bench.py --model claude-sonnet-4-6-20250514

  # Run subset for quick test
  python run_aider_bench.py --model claude-sonnet-4-6-20250514 --languages python --max-problems 10

  # Full run for leaderboard submission
  python run_aider_bench.py --model claude-sonnet-4-6-20250514 --threads 10

Cost estimate: ~$20-40 for full 225-exercise run with Claude Sonnet.
"""

import argparse
import json
import os
import subprocess
import sys
import time
from pathlib import Path

from scoring import pass_at_k, composite_score, wilson_lower


def check_prerequisites():
    """Verify aider and docker are available."""
    try:
        subprocess.run(["aider", "--version"], capture_output=True, check=True)
    except (FileNotFoundError, subprocess.CalledProcessError):
        print("ERROR: aider not found. Install with: pip install aider-chat")
        sys.exit(1)

    try:
        subprocess.run(["docker", "info"], capture_output=True, check=True)
    except (FileNotFoundError, subprocess.CalledProcessError):
        print("WARNING: docker not available. Some tests may fail without sandboxing.")


def run_benchmark(args):
    """Execute the aider benchmark."""
    check_prerequisites()

    benchmark_dir = Path(args.output_dir)
    benchmark_dir.mkdir(parents=True, exist_ok=True)

    cmd = [
        "python", "-m", "aider.benchmark",
        "--model", args.model,
        "--edit-format", args.edit_format,
        "--threads", str(args.threads),
    ]

    if args.languages:
        cmd.extend(["--languages", args.languages])
    if args.max_problems:
        cmd.extend(["--max-problems", str(args.max_problems)])

    # Set API key from environment.
    env = os.environ.copy()
    if args.provider == "anthropic":
        if "ANTHROPIC_API_KEY" not in env:
            print("ERROR: ANTHROPIC_API_KEY required for anthropic provider")
            sys.exit(1)
    elif args.provider == "openai":
        if "OPENAI_API_KEY" not in env:
            print("ERROR: OPENAI_API_KEY required for openai provider")
            sys.exit(1)
    elif args.provider == "ollama":
        env.setdefault("OPENAI_API_BASE", "http://127.0.0.1:11434/v1")
        env.setdefault("OPENAI_API_KEY", "ollama")

    print(f"Running aider benchmark: model={args.model} provider={args.provider}")
    print(f"Command: {' '.join(cmd)}")
    print()

    start = time.time()
    result = subprocess.run(cmd, env=env, capture_output=False)
    duration = time.time() - start

    return {
        "benchmark": "aider_polyglot",
        "model": args.model,
        "provider": args.provider,
        "exit_code": result.returncode,
        "duration_seconds": round(duration, 1),
    }


def parse_results(results_dir):
    """Parse aider benchmark YAML results into our scoring format."""
    results_path = Path(results_dir)
    if not results_path.exists():
        return None

    # Aider outputs YAML results. Parse the summary.
    summary = {}
    for yaml_file in results_path.glob("*.yaml"):
        try:
            import yaml
            with open(yaml_file) as f:
                data = yaml.safe_load(f)
            if data and "pass_rate_1" in str(data):
                summary = data
                break
        except ImportError:
            print("WARNING: PyYAML not installed, cannot parse results")
            return None

    if not summary:
        return None

    # Extract metrics.
    total = summary.get("completed_tests", 225)
    passed_1 = summary.get("pass_rate_1", 0)
    passed_2 = summary.get("pass_rate_2", 0)

    return {
        "total_exercises": total,
        "pass_rate_1": passed_1 / 100 if passed_1 > 1 else passed_1,
        "pass_rate_2": passed_2 / 100 if passed_2 > 1 else passed_2,
        "pass_at_1": pass_at_k(total, int(passed_1 * total / 100), 1),
        "wilson_lower_95": wilson_lower(int(passed_1 * total / 100), total),
        "cost_usd": summary.get("total_cost", 0),
    }


def main():
    parser = argparse.ArgumentParser(description="Run Aider polyglot benchmark")
    parser.add_argument("--model", required=True, help="Model name (e.g. claude-sonnet-4-6-20250514)")
    parser.add_argument("--provider", default="anthropic", choices=["anthropic", "openai", "ollama"])
    parser.add_argument("--edit-format", default="diff", choices=["diff", "whole", "udiff"])
    parser.add_argument("--threads", type=int, default=5, help="Parallel threads")
    parser.add_argument("--languages", default=None, help="Comma-separated languages (e.g. python,go)")
    parser.add_argument("--max-problems", type=int, default=None, help="Limit problems (for quick test)")
    parser.add_argument("--output-dir", default="./aider-results", help="Output directory")

    args = parser.parse_args()
    result = run_benchmark(args)

    print()
    print(f"Benchmark complete in {result['duration_seconds']}s")
    print(f"Exit code: {result['exit_code']}")
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
