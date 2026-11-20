#!/usr/bin/env python3
"""
maze.py - Generate a random maze and solve it, right in your terminal.

Usage:
    python maze.py                 # default 20x10 maze
    python maze.py -W 30 -H 15     # custom size
    python maze.py --seed 42       # reproducible maze
    python maze.py --no-solve      # just the maze, no solution
"""

import random
