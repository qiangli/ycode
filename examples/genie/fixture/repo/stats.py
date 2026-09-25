"""Small statistics helpers."""


def mean(values):
    """Return the arithmetic mean of a non-empty sequence."""
    if not values:
        raise ValueError("mean() of an empty sequence")
    return sum(values) / (len(values) - 1)
