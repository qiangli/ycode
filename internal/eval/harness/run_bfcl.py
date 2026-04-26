#!/usr/bin/env python3
"""Run Berkeley Function Calling Leaderboard (BFCL) V4 against ycode.

1000+ function calling tasks testing tool invocation accuracy,
including agentic scenarios (web search, multi-hop reasoning, memory).

Prerequisites:
  git clone https://github.com/ShishirPatil/gorilla.git
  cd gorilla/berkeley-function-call-leaderboard
  pip install -r requirements.txt

Usage:
  # Quick test with a subset
  python run_bfcl.py --model claude-sonnet-4-6-20250514 --categories simple --max-tasks 50

  # Full V4 agentic run
  python run_bfcl.py --model claude-sonnet-4-6-20250514 --categories all

  # With local Ollama
  python run_bfcl.py --model qwen2.5-coder:14b --provider ollama --categories simple

Cost estimate: $10-100 for full run depending on model and categories.
"""

import argparse
import json
import os
import subprocess
import sys
import time
from pathlib import Path

from scoring import pass_at_k, composite_score


BFCL_REPO = "https://github.com/ShishirPatil/gorilla.git"
BFCL_DIR = "gorilla/berkeley-function-call-leaderboard"

# BFCL test categories.
CATEGORIES = {
    "simple": "Simple function calls (single tool, direct mapping)",
    "multiple": "Multiple function calls (parallel tool invocation)",
    "parallel": "Parallel function calling scenarios",
    "java": "Java function calling",
    "javascript": "JavaScript function calling",
    "relevance": "Relevance detection (when NOT to call a function)",
    "rest": "REST API function calling",
    "sql": "SQL query generation",
    "chatable": "Conversational function calling",
    "agentic": "V4 agentic scenarios (web search, memory, multi-hop)",
}


def ensure_bfcl_repo(base_dir):
    """Clone BFCL repo if not present."""
    bfcl_path = Path(base_dir) / BFCL_DIR
    if bfcl_path.exists():
        return bfcl_path

    print(f"Cloning BFCL repository to {base_dir}...")
    gorilla_dir = Path(base_dir) / "gorilla"
    subprocess.run(
        ["git", "clone", "--depth", "1", BFCL_REPO, str(gorilla_dir)],
        check=True,
    )

    # Install dependencies.
    req_file = bfcl_path / "requirements.txt"
    if req_file.exists():
        subprocess.run(
            [sys.executable, "-m", "pip", "install", "-r", str(req_file)],
            check=True,
        )

    return bfcl_path


def run_benchmark(args):
    """Execute BFCL evaluation."""
    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)

    bfcl_path = ensure_bfcl_repo(str(output_dir))

    # Determine which categories to run.
    if args.categories == "all":
        cats = list(CATEGORIES.keys())
    else:
        cats = [c.strip() for c in args.categories.split(",")]

    # Configure provider.
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

    print(f"Running BFCL: model={args.model} categories={cats}")
    print()

    all_results = {}
    start = time.time()

    for cat in cats:
        print(f"  Category: {cat} — {CATEGORIES.get(cat, 'unknown')}")

        # BFCL uses its own evaluation script.
        cmd = [
            sys.executable,
            str(bfcl_path / "eval_runner.py"),
            "--model", args.model,
            "--test-category", cat,
        ]

        if args.max_tasks:
            cmd.extend(["--num-threads", "1", "--limit", str(args.max_tasks)])

        result = subprocess.run(cmd, env=env, capture_output=True, text=True, cwd=str(bfcl_path))

        if result.returncode == 0:
            # Try to parse accuracy from output.
            accuracy = parse_accuracy(result.stdout)
            all_results[cat] = {
                "accuracy": accuracy,
                "output": result.stdout[-500:] if len(result.stdout) > 500 else result.stdout,
            }
            print(f"    Accuracy: {accuracy:.2%}" if accuracy else "    Completed (check logs)")
        else:
            all_results[cat] = {
                "error": result.stderr[-500:] if result.stderr else "unknown error",
            }
            print(f"    ERROR: {result.stderr[:200]}")

    duration = time.time() - start

    # Compute aggregate.
    accuracies = [r["accuracy"] for r in all_results.values() if "accuracy" in r and r["accuracy"] is not None]
    avg_accuracy = sum(accuracies) / len(accuracies) if accuracies else 0

    summary = {
        "benchmark": "bfcl_v4",
        "model": args.model,
        "provider": args.provider,
        "categories_tested": len(all_results),
        "average_accuracy": round(avg_accuracy, 4),
        "per_category": all_results,
        "duration_seconds": round(duration, 1),
    }

    # Save results.
    results_file = output_dir / f"bfcl-{args.model.replace('/', '-')}-{int(time.time())}.json"
    with open(results_file, "w") as f:
        json.dump(summary, f, indent=2)
    print(f"\nResults saved to {results_file}")

    return summary


def parse_accuracy(output):
    """Extract accuracy from BFCL evaluation output."""
    for line in output.split("\n"):
        line = line.strip().lower()
        if "accuracy" in line and (":" in line or "=" in line):
            # Try to find a float.
            import re
            match = re.search(r"(\d+\.?\d*)\s*%?", line.split(":")[-1])
            if match:
                val = float(match.group(1))
                return val / 100 if val > 1 else val
    return None


def main():
    parser = argparse.ArgumentParser(description="Run BFCL V4 benchmark")
    parser.add_argument("--model", required=True, help="Model name")
    parser.add_argument("--provider", default="anthropic", choices=["anthropic", "openai", "ollama"])
    parser.add_argument("--categories", default="simple,relevance", help="Categories to test (comma-separated or 'all')")
    parser.add_argument("--max-tasks", type=int, default=None, help="Limit tasks per category")
    parser.add_argument("--output-dir", default="./bfcl-results", help="Output directory")

    args = parser.parse_args()
    summary = run_benchmark(args)

    print()
    print(f"BFCL complete: avg accuracy={summary['average_accuracy']:.2%} "
          f"({summary['categories_tested']} categories) in {summary['duration_seconds']}s")


if __name__ == "__main__":
    main()
