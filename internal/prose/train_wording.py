#!/usr/bin/env python3
# myoo-ignore-file: comments-in-file-header
"""Generate wording_en.go, the English word-choice model of prose_lint.

The model is a logistic regression of AI authorship on how often a text uses
each common word, question marks and exclamation marks (count / number of
words). prose_lint computes the same sum over the text, so the result is the
log-odds of AI authorship at even priors.

Input is a JSON Lines corpus, one document per line:

    {"text": "...", "label": "human" | "ai", "source": "...", "group": "..."}

Every source gets the same total weight, and so does each label within a
source, so a big source can't dominate. A word joins the vocabulary when 1.5
% or more of the human documents and of the AI documents use it, which keeps
out names and most topic words.

The regularization that keeps topic words out also shrinks the log-odds, so
we calibrate them. We score every document with a model trained on the
other folds, keeping each group (a human text and the AI text on its topic)
in a single fold, fit the logistic curve of the labels on those scores, and
scale the weights by its slope.

The corpus behind the committed wording_en.go pairs 389 human texts with a
text that an AI agent (Claude) wrote in 2026 on the same topic and in the
same genre: news, opinion posts, stories, reviews, abstracts, encyclopedia
paragraphs and forum posts from the MAGE dataset (yaful/MAGE), answers to
questions from the HC3 dataset (Hello-SimpleAI/HC3), and sections of the
human-written project documentation from the README's likelihood section.
Pairing by topic keeps the model from learning what people and agents write
about. Another 99 pairs were held out. We tried adding the documentation of
whole projects as well, but held-out projects showed the model had learned
the projects rather than their authors.

Usage: train_wording.py corpus.jsonl > wording_en.go   (needs scikit-learn)
"""
import collections
import json
import re
import sys

import numpy as np
from sklearn.linear_model import LogisticRegression
from sklearn.model_selection import GroupKFold

# A word, or a question or exclamation mark, which people write more often
# than agents. People also write more parentheses and semicolons than
# Claude does in the pairs, but agent-written documentation is full of them,
# so they made the model worse on it. prose_lint counts the same tokens (see
# wording.go).
TOKEN = re.compile(r"[^\W_]+(?:'[^\W_]+)*|[?!]")
MARKS = {"?", "!"}


def tokens(text):
    """The words and marks of a text as prose_lint counts them, without code
    and URLs, and the number of words among them."""
    t = text.replace("\u2019", "'")
    t = re.sub(r"```.*?```", " ", t, flags=re.S)
    t = re.sub(r"<pre>.*?</pre>", " ", t, flags=re.S)
    t = re.sub(r"`[^`\n]*`", " ", t)
    t = re.sub(r"\]\([^)]*\)", "]", t)
    t = re.sub(r"https?://\S+", " ", t)
    # Some datasets split contractions into separate tokens ("do n't").
    t = re.sub(r" (n't|'s|'re|'ve|'ll|'d|'m)\b", r"\1", t)
    out = [m.lower() for m in TOKEN.findall(t)]
    return out, sum(1 for w in out if w not in MARKS)


def main():
    docs = [json.loads(line) for line in open(sys.argv[1], encoding="utf-8")]
    toks = [tokens(d["text"]) for d in docs]
    y = np.array([1 if d["label"] == "ai" else 0 for d in docs])
    df = [collections.Counter(), collections.Counter()]
    for (ts, _), label in zip(toks, y):
        df[label].update(set(ts))
    n = [int((y == 0).sum()), int((y == 1).sum())]
    vocab = sorted(w for w in set(df[0]) | set(df[1])
                   if w in MARKS or not any(ch.isdigit() for ch in w) and (len(w) > 1 or w in ("a", "i"))
                   if df[0][w] >= 0.015 * n[0] and df[1][w] >= 0.015 * n[1])
    index = {w: i for i, w in enumerate(vocab)}
    X = np.zeros((len(docs), len(vocab)), dtype=np.float32)
    for r, (ts, words) in enumerate(toks):
        for w, k in collections.Counter(ts).items():
            if w in index and words:
                X[r, index[w]] = 100 * k / words
    sources = collections.Counter((d["source"], d["label"]) for d in docs)
    weight = np.array([1.0 / sources[(d["source"], d["label"])] for d in docs])
    weight *= len(docs) / weight.sum()
    groups = [d["source"] + ":" + d["group"] for d in docs]
    oof = np.zeros(len(docs))
    for train, test in GroupKFold(n_splits=5).split(X, y, groups):
        fold = LogisticRegression(C=0.05, max_iter=5000).fit(X[train], y[train], sample_weight=weight[train])
        oof[test] = fold.decision_function(X[test])
    curve = LogisticRegression(C=1e6).fit(oof.reshape(-1, 1), y, sample_weight=weight)
    scale, shift = curve.coef_[0][0], curve.intercept_[0]
    model = LogisticRegression(C=0.05, max_iter=5000).fit(X, y, sample_weight=weight)
    coef = model.coef_[0] * 100 * scale  # per use of the word per word of text
    bias = model.intercept_[0] * scale + shift
    print(f"calibration: scale {scale:.2f}, shift {shift:.2f}", file=sys.stderr)
    # How often human text uses each word, averaged over the sources, so
    # prose_lint can name the words a text uses more than people do.
    human = collections.defaultdict(list)
    for r, d in enumerate(docs):
        if d["label"] == "human":
            human[d["source"]].append(X[r] / 100)
    rate = np.mean([np.mean(rows, axis=0) for rows in human.values()], axis=0)
    out = sys.stdout
    out.write("// Code generated by train_wording.py; DO NOT EDIT.\n\npackage prose\n\n")
    out.write("// wordingBias and wordingModel are the English word-choice model, see\n// wording.go and train_wording.py.\n")
    out.write(f"const wordingBias = {bias:.3f}\n\nvar wordingModel = map[string]wordStat{{\n")
    for w, c, h in sorted(zip(vocab, coef, rate)):
        if abs(c) >= 0.005:
            out.write(f"\t{json.dumps(w)}: {{{c:.2f}, {h:.6f}}},\n")
    out.write("}\n")


main()
