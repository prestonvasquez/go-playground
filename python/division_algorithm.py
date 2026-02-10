import timeit
import math

def vasquez_solution(n):
    if n < 2:
        return []
    factors = []
    while n % 2 == 0:
        factors.append(2)
        n //= 2
    divisor = 3
    while divisor * divisor <= n:
        while n % divisor == 0:
            factors.append(divisor)
            n //= divisor
        divisor += 2
    if n > 1:
        factors.append(n)
    return factors

def lajos_solution(n):
    """Find all factors of a given positive integer n."""
    factors = set()
    # Iterate from 1 up to (the square root of n) + 1
    for i in range(2, int(math.sqrt(n)) + 1):
        # % is "mod" and means "remainder after division"
        if n % i == 0:
            factors.add(i)
            factors.add(n // i)  # Add the other factor (n divided by i)
    return sorted(list(factors))

n = 2 * 1000003

# Get the actual results
lajos_result = lajos_solution(n)
vasquez_result = vasquez_solution(n)

print(f"Input n: {n}")
print(f"\nlajos_solution result: {lajos_result}")
print(f"vasquez_solution result: {vasquez_result}")

# Timing comparisons
print("\n--- Timing Comparison ---")
print("lajos_solution:", timeit.timeit(lambda: lajos_solution(n), number=10000))
print("vasquez_solution:", timeit.timeit(lambda: vasquez_solution(n), number=10000))
