import unittest

from stats import mean


class MeanTest(unittest.TestCase):
    def test_single(self):
        self.assertEqual(mean([5]), 5)

    def test_pair(self):
        self.assertEqual(mean([2, 4]), 3)


if __name__ == "__main__":
    unittest.main()
