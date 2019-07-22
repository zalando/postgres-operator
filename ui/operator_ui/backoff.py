import random


def expo(n: int, base=2, factor=1, max_value=None):
    """Exponential decay.

    Adapted from https://github.com/litl/backoff/blob/master/backoff.py (MIT License)

    Args:
        base: The mathematical base of the exponentiation operation
        factor: Factor to multiply the exponentation by.
        max_value: The maximum value to yield. Once the value in the
             true exponential sequence exceeds this, the value
             of max_value will forever after be yielded.
    """
    a = factor * base ** n
    if max_value is None or a < max_value:
        return a
    else:
        return max_value


def random_jitter(value, jitter=1):
    """Jitter the value a random number of milliseconds.

    Copied from https://github.com/litl/backoff/blob/master/backoff.py (MIT License)

    This adds up to 1 second of additional time to the original value.
    Prior to backoff version 1.2 this was the default jitter behavior.
    Args:
        value: The unadulterated backoff value.
    """
    return value + random.uniform(0, jitter)


def full_jitter(value):
    """Jitter the value across the full range (0 to value).

    Copied from https://github.com/litl/backoff/blob/master/backoff.py (MIT License)

    This corresponds to the "Full Jitter" algorithm specified in the
    AWS blog's post on the performance of various jitter algorithms.
    (http://www.awsarchitectureblog.com/2015/03/backoff.html)

    Args:
        value: The unadulterated backoff value.
    """
    return random.uniform(0, value)
