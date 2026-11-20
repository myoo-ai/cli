// Package likelihood turns counted signals into the "(likely) AI patterns"
// percentage of code_lint and prose_lint.
//
// Each signal is a rate per 1000 words or lines, or the share of items that
// are of a given kind. On a reference corpus
// of human and AI written inputs, the log of a rate (or the logit of a
// share) is roughly normal within each group, so we measured the group
// medians and a common spread up front. A signal then contributes the
// log-likelihood ratio of its observed value under the two groups. A small
// input can't measure its rate very precisely, so we widen both groups by
// the measurement variance, which pulls the evidence towards zero. We cap
// each contribution so a single signal can't decide on its own, and sum
// them with a prior to get the log-odds of AI authorship (naive Bayes).
package likelihood
