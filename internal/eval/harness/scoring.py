"""Shared scoring utilities for external eval harnesses.

These implement the same formulas as internal/eval/scoring.go
for consistency when scoring from Python-based eval tools.
"""

import math
from typing import Optional


def pass_at_k(n: int, c: int, k: int) -> float:
    """Compute pass@k: probability that at least 1 of k samples passes.

    Args:
        n: total number of samples
        c: number of correct samples
        k: number of samples to evaluate

    Returns:
        Float in [0.0, 1.0]. Higher is better.
    """
    if n <= 0 or k <= 0 or k > n:
        return 0.0
    if c >= n:
        return 1.0
    if c <= 0:
        return 0.0

    # Use log-space to avoid overflow.
    log_ratio = 0.0
    for i in range(k):
        num = n - c - i
        den = n - i
        if num <= 0:
            return 1.0
        log_ratio += math.log(num) - math.log(den)

    return 1.0 - math.exp(log_ratio)


def pass_pow_k(n: int, c: int, k: int) -> float:
    """Compute pass^k: probability that ALL k samples pass.

    Args:
        n: total number of samples
        c: number of correct samples
        k: number of samples to evaluate

    Returns:
        Float in [0.0, 1.0]. Higher is more consistent.
    """
    if n <= 0 or k <= 0 or k > n:
        return 0.0
    if c < k:
        return 0.0
    if c >= n:
        return 1.0

    log_ratio = 0.0
    for i in range(k):
        log_ratio += math.log(c - i) - math.log(n - i)
    return math.exp(log_ratio)


def flakiness(pass_rate: float) -> float:
    """Compute binary entropy of pass rate.

    Args:
        pass_rate: fraction of passing trials [0.0, 1.0]

    Returns:
        Float in [0.0, 1.0]. 0 = deterministic, 1 = max entropy.
    """
    if pass_rate <= 0 or pass_rate >= 1:
        return 0.0
    return -pass_rate * math.log2(pass_rate) - (1 - pass_rate) * math.log2(1 - pass_rate)


def composite_score(
    pass_at_k_val: float,
    pass_pow_k_val: float,
    flakiness_val: float,
    tool_accuracy: float = 1.0,
    cost_efficiency: float = 1.0,
) -> float:
    """Compute weighted composite score.

    Returns:
        Float in [0.0, 1.0]. Displayed as 0-100 points.
    """
    return (
        0.35 * pass_at_k_val
        + 0.25 * pass_pow_k_val
        + 0.15 * (1.0 - flakiness_val)
        + 0.15 * tool_accuracy
        + 0.10 * cost_efficiency
    )


def wilson_lower(successes: int, total: int, z: float = 1.96) -> float:
    """Compute lower bound of Wilson score confidence interval.

    Args:
        successes: number of successful trials
        total: total number of trials
        z: z-score for confidence level (1.96 = 95%)

    Returns:
        Lower bound of the confidence interval.
    """
    if total <= 0:
        return 0.0
    n = float(total)
    p = float(successes) / n
    denom = 1 + z * z / n
    center = p + z * z / (2 * n)
    margin = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n))
    return (center - margin) / denom


def percent_change(baseline: float, current: float) -> float:
    """Compute percentage change from baseline to current."""
    if baseline == 0:
        return 100.0 if current != 0 else 0.0
    return ((current - baseline) / baseline) * 100


def classify_regression(baseline: float, current: float) -> str:
    """Classify regression severity.

    Returns: 'none', 'warning', or 'regression'
    """
    pct = percent_change(baseline, current)
    if pct >= -5:
        return "none"
    if pct >= -15:
        return "warning"
    return "regression"


if __name__ == "__main__":
    # Self-test: verify formulas match Go implementation.
    assert abs(pass_at_k(3, 2, 1) - 0.6667) < 0.01
    assert abs(pass_at_k(3, 3, 3) - 1.0) < 0.01
    assert abs(pass_pow_k(3, 2, 2) - 0.3333) < 0.01
    assert abs(flakiness(0.5) - 1.0) < 0.01
    assert abs(flakiness(1.0)) < 0.001
    assert abs(composite_score(1.0, 1.0, 0.0, 1.0, 1.0) - 1.0) < 0.001
    assert classify_regression(0.8, 0.6) == "regression"
    assert classify_regression(0.8, 0.77) == "none"
    print("All scoring self-tests passed.")
